package backend

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/user/flow/backend/internal/config"
	"github.com/user/flow/backend/internal/llamacpp"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// LlamaStartResult is returned after Flow starts its managed llama-server.
type LlamaStartResult struct {
	Status LlamaServerStatus `json:"status"`
	Models []ModelInfo       `json:"models"`
}

// LlamaServerStatus is the frontend-facing runtime status for managed llama.cpp.
type LlamaServerStatus = llamacpp.Status

// PickLlamaModel opens a native file picker for GGUF model files.
func (a *App) PickLlamaModel() (string, error) {
	if a.ctx == nil {
		return "", fmt.Errorf("app context unavailable")
	}
	return wailsRuntime.OpenFileDialog(a.ctx, wailsRuntime.OpenDialogOptions{
		Title: "Choose a GGUF model",
		Filters: []wailsRuntime.FileFilter{
			{DisplayName: "GGUF models (*.gguf)", Pattern: "*.gguf"},
		},
		ResolvesAliases: true,
	})
}

// GetLlamaServerStatus returns the current managed llama-server state.
func (a *App) GetLlamaServerStatus() LlamaServerStatus {
	if a.llama == nil {
		return llamacpp.Status{State: "stopped", Port: 8080, BaseURL: llamacpp.BaseURL(8080), ContextSize: 4096}
	}
	return a.llama.Status()
}

// StartLlamaServer starts Flow's managed llama-server and waits until /v1/models is reachable.
func (a *App) StartLlamaServer(modelPath string, port, contextSize int) (*LlamaStartResult, error) {
	if a.llama == nil {
		if a.baseDir == "" {
			return nil, fmt.Errorf("baseDir not initialised")
		}
		a.llama = llamacpp.NewManager(a.baseDir)
	}
	if err := a.llama.Start(modelPath, port, contextSize); err != nil {
		return nil, err
	}

	status := a.llama.Status()
	models, err := a.waitForLlamaModels(status.BaseURL)
	if err != nil {
		if stopErr := a.llama.Stop(); stopErr != nil {
			return nil, fmt.Errorf("%w; stop failed: %v", err, stopErr)
		}
		return nil, err
	}
	if len(models) == 0 {
		models = []ModelInfo{{ID: modelIDFromPath(modelPath), OwnedBy: "llama.cpp"}}
	}
	return &LlamaStartResult{Status: a.llama.Status(), Models: models}, nil
}

// StopLlamaServer stops only the llama-server process owned by Flow.
func (a *App) StopLlamaServer() (LlamaServerStatus, error) {
	if a.llama == nil {
		return a.GetLlamaServerStatus(), nil
	}
	err := a.llama.Stop()
	return a.llama.Status(), err
}

func (a *App) waitForLlamaModels(baseURL string) ([]ModelInfo, error) {
	var lastErr error
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		models, err := a.ListLocalModels(baseURL, "")
		if err == nil {
			return models, nil
		}
		lastErr = err
		status := a.GetLlamaServerStatus()
		if status.State == "error" {
			if status.LastError != "" {
				return nil, fmt.Errorf("llama-server exited: %s", status.LastError)
			}
			return nil, fmt.Errorf("llama-server exited while starting")
		}
		time.Sleep(500 * time.Millisecond)
	}
	if lastErr != nil {
		return nil, fmt.Errorf("llama-server did not become ready: %w", lastErr)
	}
	return nil, fmt.Errorf("llama-server did not become ready")
}

func modelIDFromPath(path string) string {
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if name == "" {
		return "local-gguf"
	}
	return name
}

// DownloadLlamaModel streams a GGUF model from a Hugging Face (or any direct)
// URL into ~/.flow/llamacpp/models/, deletes any previously-downloaded GGUF in
// that directory on success, and returns the absolute path to the new file.
// Progress is emitted to the frontend via "flow:llama:download:progress".
func (a *App) DownloadLlamaModel(rawURL string) (string, error) {
	if a.baseDir == "" {
		return "", fmt.Errorf("baseDir not initialised")
	}
	if a.ctx == nil {
		return "", fmt.Errorf("app context unavailable")
	}

	downloadURL, filename, err := normalizeGGUFURL(rawURL)
	if err != nil {
		return "", err
	}

	modelsDir := filepath.Join(a.baseDir, "llamacpp", "models")
	if err := os.MkdirAll(modelsDir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", modelsDir, err)
	}

	destPath := filepath.Join(modelsDir, filename)
	tmpPath := destPath + ".part"
	_ = os.Remove(tmpPath)

	a.emitDownloadProgress("starting", 0, 0, filename, "")

	req, err := http.NewRequestWithContext(a.ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "flow-llama-downloader")
	resp, err := (&http.Client{Timeout: 30 * time.Minute}).Do(req) // large file downloads can take a while
	if err != nil {
		a.emitDownloadProgress("error", 0, 0, filename, err.Error())
		return "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg := fmt.Sprintf("HTTP %s", resp.Status)
		a.emitDownloadProgress("error", 0, 0, filename, msg)
		return "", fmt.Errorf("download: %s", msg)
	}

	out, err := os.Create(tmpPath)
	if err != nil {
		return "", fmt.Errorf("create %s: %w", tmpPath, err)
	}

	total := resp.ContentLength
	var downloaded int64
	buf := make([]byte, 256*1024)
	lastEmit := time.Now()

	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				_ = out.Close()
				_ = os.Remove(tmpPath)
				a.emitDownloadProgress("error", downloaded, total, filename, werr.Error())
				return "", fmt.Errorf("write: %w", werr)
			}
			downloaded += int64(n)
			if time.Since(lastEmit) > 200*time.Millisecond {
				a.emitDownloadProgress("downloading", downloaded, total, filename, "")
				lastEmit = time.Now()
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			_ = out.Close()
			_ = os.Remove(tmpPath)
			a.emitDownloadProgress("error", downloaded, total, filename, rerr.Error())
			return "", fmt.Errorf("download: %w", rerr)
		}
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}

	// Delete previously-downloaded GGUFs in the managed dir so only the latest
	// one remains. Files outside this dir (user-picked) are never touched.
	if entries, err := os.ReadDir(modelsDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if name == filename || name == filepath.Base(tmpPath) {
				continue
			}
			if !strings.EqualFold(filepath.Ext(name), ".gguf") {
				continue
			}
			_ = os.Remove(filepath.Join(modelsDir, name))
		}
	}

	if err := os.Rename(tmpPath, destPath); err != nil {
		_ = os.Remove(tmpPath)
		a.emitDownloadProgress("error", downloaded, total, filename, err.Error())
		return "", fmt.Errorf("rename: %w", err)
	}

	// Persist so reopening the settings modal shows the last-used URL and path
	// without the user having to click Save.
	if a.cfg != nil {
		a.cfg.LlamaModelPath = destPath
		a.cfg.LlamaDownloadURL = downloadURL
		_ = config.Save(a.baseDir, a.cfg)
	}

	a.emitDownloadProgress("done", downloaded, total, filename, "")
	return destPath, nil
}

func (a *App) emitDownloadProgress(stage string, downloaded, total int64, filename, errMsg string) {
	if a.ctx == nil {
		return
	}
	wailsRuntime.EventsEmit(a.ctx, "flow:llama:download:progress", map[string]interface{}{
		"stage":      stage,
		"downloaded": downloaded,
		"total":      total,
		"filename":   filename,
		"error":      errMsg,
	})
}

// normalizeGGUFURL accepts a Hugging Face GGUF link (resolve or blob form) or
// any direct GGUF URL, and returns the URL to actually fetch plus the filename
// to save it as.
func normalizeGGUFURL(raw string) (string, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", fmt.Errorf("paste a Hugging Face GGUF URL")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return "", "", fmt.Errorf("URL must start with https://")
	}

	// Validate the download host against trusted sources.
	trustedHosts := []string{
		"huggingface.co",
		"github.com",
		"objects.githubusercontent.com",
		"ollama.com",
	}
	hostLower := strings.ToLower(u.Hostname())
	isTrusted := false
	for _, trusted := range trustedHosts {
		if hostLower == trusted || strings.HasSuffix(hostLower, "."+trusted) {
			isTrusted = true
			break
		}
	}
	if !isTrusted {
		log.Printf("[llama] WARNING: downloading GGUF from untrusted host %q — verify this is safe", u.Host)
	}

	if strings.Contains(u.Host, "huggingface.co") {
		u.Path = strings.Replace(u.Path, "/blob/", "/resolve/", 1)
	}
	base := path.Base(u.Path)
	if base == "" || base == "/" || base == "." {
		return "", "", fmt.Errorf("URL must point to a .gguf file")
	}
	if !strings.EqualFold(filepath.Ext(base), ".gguf") {
		return "", "", fmt.Errorf("URL must point to a .gguf file")
	}
	return u.String(), base, nil
}

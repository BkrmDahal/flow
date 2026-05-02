//go:build darwin

package speech

import (
	"bytes"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// bundledFS holds the whisper-cli arm64 binary plus its rewritten dylibs.
// `scripts/fetch-whisper-cli.sh` populates the directory; the repo ships a
// 0-byte placeholder so go:embed compiles without it. At runtime we treat a
// binary under minBundledBinarySize as "no embed, fall back to PATH" so
// contributors can build the repo before running the fetch script.
//
//go:embed bin
var bundledFS embed.FS

const (
	minBundledBinarySize = 100 * 1024 // real whisper-cli is ~650 KB
	bundledBinaryName    = "bin/whisper-cli-darwin-arm64"
	bundledLibPrefix     = "bin/lib/"
)

// LocalProgressFn is invoked while the bundled whisper model is being
// downloaded on first use. stage is "download"; downloaded/total are bytes.
// Implementations should be cheap (the callback fires every ~256 KB).
type LocalProgressFn func(stage string, downloaded, total int64)

var (
	localProgressMu sync.RWMutex
	localProgressCb LocalProgressFn

	binaryOnce   sync.Once
	binaryPath   string
	binaryErr    error
)

// SetLocalProgressCallback registers a callback that receives model-download
// progress events. Pass nil to clear.
func SetLocalProgressCallback(cb LocalProgressFn) {
	localProgressMu.Lock()
	localProgressCb = cb
	localProgressMu.Unlock()
}

// emitProgress invokes the registered callback if any.
func emitProgress(stage string, downloaded, total int64) {
	localProgressMu.RLock()
	cb := localProgressCb
	localProgressMu.RUnlock()
	if cb != nil {
		cb(stage, downloaded, total)
	}
}

// transcribeLocal runs the bundled whisper.cpp on the supplied audio bytes and
// returns the transcribed text.
func transcribeLocal(cfg TranscribeConfig, audio []byte, mimeType string) (*TranscribeResult, error) {
	bin, err := ensureWhisperBinary()
	if err != nil {
		return nil, fmt.Errorf("whisper-cli unavailable: %w", err)
	}

	model := cfg.Model
	if model == "" {
		model = "base.en"
	}
	modelPath, err := ensureWhisperModel(model)
	if err != nil {
		return nil, fmt.Errorf("whisper model: %w", err)
	}

	tmpDir, err := os.MkdirTemp("", "flow-whisper-*")
	if err != nil {
		return nil, fmt.Errorf("temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Always normalise to 16-kHz mono WAV — whisper.cpp's default decoder is
	// strict about format. afconvert ships with macOS, no extra dependency.
	srcExt := fileExtForMime(mimeType)
	if srcExt == "" {
		srcExt = ".m4a"
	}
	srcPath := filepath.Join(tmpDir, "input"+srcExt)
	if err := os.WriteFile(srcPath, audio, 0o600); err != nil {
		return nil, fmt.Errorf("write audio: %w", err)
	}

	wavPath := filepath.Join(tmpDir, "input.wav")
	convCmd := exec.Command("/usr/bin/afconvert",
		"-f", "WAVE", "-d", "LEI16@16000", "-c", "1",
		srcPath, wavPath)
	var convErr bytes.Buffer
	convCmd.Stderr = &convErr
	if err := convCmd.Run(); err != nil {
		return nil, fmt.Errorf("afconvert: %w (%s)", err, strings.TrimSpace(convErr.String()))
	}

	outPrefix := filepath.Join(tmpDir, "out")
	args := []string{"-m", modelPath, "-f", wavPath, "-nt", "-otxt", "-of", outPrefix}
	if cfg.Language != "" {
		args = append(args, "-l", cfg.Language)
	}
	if cfg.Prompt != "" {
		args = append(args, "--prompt", cfg.Prompt)
	}

	cmd := exec.Command(bin, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("whisper-cli: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}

	textBytes, err := os.ReadFile(outPrefix + ".txt")
	if err != nil {
		return nil, fmt.Errorf("read whisper output: %w", err)
	}
	text := strings.TrimSpace(string(textBytes))
	return &TranscribeResult{Text: text}, nil
}

// ensureWhisperBinary returns a path to an executable whisper-cli, extracting
// the embedded copy on first call. If the embedded blob is a placeholder, we
// fall back to whatever `whisper-cli` is on PATH (developer convenience).
func ensureWhisperBinary() (string, error) {
	binaryOnce.Do(func() {
		binaryPath, binaryErr = resolveWhisperBinary()
	})
	return binaryPath, binaryErr
}

func resolveWhisperBinary() (string, error) {
	binBytes, err := bundledFS.ReadFile(bundledBinaryName)
	if err == nil && len(binBytes) >= minBundledBinarySize {
		binDir, err := flowSubdir("bin")
		if err != nil {
			return "", err
		}
		// Extract dylibs first — the binary's rpath (@loader_path/../lib)
		// resolves them at the moment dyld loads whisper-cli.
		if err := extractBundledLibs(); err != nil {
			return "", err
		}
		binPath := filepath.Join(binDir, "whisper-cli")
		if needsExtract(binPath, int64(len(binBytes))) {
			if err := writeExecutable(binPath, binBytes); err != nil {
				return "", fmt.Errorf("extract whisper-cli: %w", err)
			}
		}
		return binPath, nil
	}

	// Placeholder embed — try PATH (dev fallback).
	if path, err := exec.LookPath("whisper-cli"); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("bundled whisper-cli is missing — run scripts/fetch-whisper-cli.sh, or `brew install whisper-cpp`")
}

// extractBundledLibs copies every dylib under embed:bin/lib/ into ~/.flow/lib/
// so the binary's rpath @loader_path/../lib resolves them.
func extractBundledLibs() error {
	libDir, err := flowSubdir("lib")
	if err != nil {
		return err
	}
	return fs.WalkDir(bundledFS, "bin/lib", func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		data, err := bundledFS.ReadFile(p)
		if err != nil {
			return fmt.Errorf("read %s: %w", p, err)
		}
		dest := filepath.Join(libDir, filepath.Base(p))
		if !needsExtract(dest, int64(len(data))) {
			return nil
		}
		return writeExecutable(dest, data)
	})
}

func needsExtract(path string, size int64) bool {
	info, err := os.Stat(path)
	if err != nil {
		return true
	}
	return info.Size() != size
}

func writeExecutable(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ensureWhisperModel returns the on-disk path to a ggml model, downloading it
// from Hugging Face on first use.
func ensureWhisperModel(name string) (string, error) {
	dir, err := flowSubdir("models")
	if err != nil {
		return "", err
	}
	filename := "ggml-" + name + ".bin"
	dest := filepath.Join(dir, filename)

	if info, err := os.Stat(dest); err == nil && info.Size() > 0 {
		return dest, nil
	}

	url := "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/" + filename
	if err := downloadWithProgress(url, dest); err != nil {
		return "", err
	}
	return dest, nil
}

// downloadWithProgress streams a URL to dest atomically (writes to .tmp, then
// renames). Emits progress events along the way.
func downloadWithProgress(url, dest string) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("download %s: HTTP %d", url, resp.StatusCode)
	}

	tmp := dest + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create %s: %w", tmp, err)
	}

	total := resp.ContentLength
	emitProgress("download", 0, total)

	pw := &progressWriter{total: total, last: time.Now()}
	if _, err := io.Copy(out, io.TeeReader(resp.Body, pw)); err != nil {
		out.Close()
		os.Remove(tmp)
		return fmt.Errorf("copy: %w", err)
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("close: %w", err)
	}
	if err := os.Rename(tmp, dest); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	emitProgress("download", pw.done.Load(), total)
	return nil
}

type progressWriter struct {
	done  atomic.Int64
	total int64
	last  time.Time
}

func (p *progressWriter) Write(b []byte) (int, error) {
	n := len(b)
	done := p.done.Add(int64(n))
	// Throttle to ~10 events/sec so the front-end isn't flooded.
	if time.Since(p.last) >= 100*time.Millisecond {
		p.last = time.Now()
		emitProgress("download", done, p.total)
	}
	return n, nil
}

// flowSubdir returns ~/.flow/<sub>/, creating it if necessary.
func flowSubdir(sub string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	dir := filepath.Join(home, ".flow", sub)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", dir, err)
	}
	return dir, nil
}

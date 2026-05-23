//go:build darwin

package llamacpp

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

//go:embed all:bin
var bundledFS embed.FS

const (
	defaultHost          = "127.0.0.1"
	minBundledBinarySize = 1 * 1024 * 1024
	bundledBinaryName    = "bin/llama-server-darwin-arm64"
	bundledLibPrefix     = "bin/lib"
)

// Status describes the Flow-owned llama-server process.
type Status struct {
	State       string `json:"state"`
	Running     bool   `json:"running"`
	Owned       bool   `json:"owned"`
	Port        int    `json:"port"`
	BaseURL     string `json:"baseUrl"`
	ModelPath   string `json:"modelPath"`
	LastError   string `json:"lastError,omitempty"`
	LogExcerpt  string `json:"logExcerpt,omitempty"`
	ProcessID   int    `json:"processId,omitempty"`
	ContextSize int    `json:"contextSize"`
}

// Manager owns the lifecycle of a llama-server process started by Flow.
type Manager struct {
	mu      sync.Mutex
	baseDir string

	cmd         *exec.Cmd
	done        chan struct{}
	state       string
	modelPath   string
	port        int
	contextSize int
	lastError   string
	logs        ringBuffer
}

func NewManager(baseDir string) *Manager {
	return &Manager{
		baseDir:     baseDir,
		state:       "stopped",
		port:        8080,
		contextSize: 4096,
	}
}

func BaseURL(port int) string {
	if port == 0 {
		port = 8080
	}
	return fmt.Sprintf("http://%s:%d/v1", defaultHost, port)
}

func (m *Manager) Start(modelPath string, port, contextSize int) error {
	modelPath = strings.TrimSpace(modelPath)
	if modelPath == "" {
		return fmt.Errorf("choose a GGUF model before starting llama.cpp")
	}
	info, err := os.Stat(modelPath)
	if err != nil {
		return fmt.Errorf("model file: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("model path is a directory, choose a .gguf file")
	}
	if !strings.EqualFold(filepath.Ext(modelPath), ".gguf") {
		return fmt.Errorf("model must be a .gguf file")
	}
	if port == 0 {
		port = 8080
	}
	if contextSize == 0 {
		contextSize = 4096
	}

	m.mu.Lock()
	running := m.cmd != nil && m.cmd.Process != nil && (m.state == "starting" || m.state == "running")
	sameServer := running && m.modelPath == modelPath && m.port == port && m.contextSize == contextSize
	m.mu.Unlock()
	if sameServer {
		return nil
	}
	if running {
		if err := m.Stop(); err != nil {
			return fmt.Errorf("stop existing llama-server: %w", err)
		}
	}

	if portOpen(port) {
		return fmt.Errorf("port %d is already in use by another process", port)
	}

	bin, err := m.ensureBinary()
	if err != nil {
		return err
	}

	cmd := exec.Command(bin,
		"-m", modelPath,
		"--host", defaultHost,
		"--port", fmt.Sprintf("%d", port),
		"-c", fmt.Sprintf("%d", contextSize),
	)
	cmd.Stdout = m.logWriter()
	cmd.Stderr = m.logWriter()

	m.mu.Lock()
	m.cmd = cmd
	m.done = make(chan struct{})
	m.state = "starting"
	m.modelPath = modelPath
	m.port = port
	m.contextSize = contextSize
	m.lastError = ""
	m.logs.Reset()
	m.mu.Unlock()

	if err := cmd.Start(); err != nil {
		m.mu.Lock()
		m.cmd = nil
		m.state = "error"
		m.lastError = err.Error()
		m.mu.Unlock()
		return fmt.Errorf("start llama-server: %w", err)
	}

	m.mu.Lock()
	m.state = "running"
	m.mu.Unlock()

	go m.wait(cmd)
	return nil
}

// KillStrayServers kills any running llama-server processes on the system to ensure a clean state.
func KillStrayServers() {
	_ = exec.Command("pkill", "llama-server").Run()
	_ = exec.Command("killall", "llama-server").Run()
}

func (m *Manager) Stop() error {
	m.mu.Lock()
	cmd := m.cmd
	done := m.done
	if cmd != nil {
		m.state = "stopping"
	}
	m.mu.Unlock()

	var stopErr error
	if cmd != nil && cmd.Process != nil {
		if err := cmd.Process.Signal(os.Interrupt); err != nil && !errors.Is(err, os.ErrProcessDone) {
			_ = cmd.Process.Kill()
			stopErr = err
		} else {
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				_ = cmd.Process.Kill()
				if done != nil {
					<-done
				}
			}
		}
	}

	// Always ensure any running llama-server processes on the system are terminated
	KillStrayServers()

	return stopErr
}

func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	status := Status{
		State:       m.state,
		Running:     m.cmd != nil && m.cmd.Process != nil && (m.state == "starting" || m.state == "running" || m.state == "stopping"),
		Owned:       m.cmd != nil,
		Port:        m.port,
		BaseURL:     BaseURL(m.port),
		ModelPath:   m.modelPath,
		LastError:   m.lastError,
		LogExcerpt:  m.logs.String(),
		ContextSize: m.contextSize,
	}
	if m.cmd != nil && m.cmd.Process != nil {
		status.ProcessID = m.cmd.Process.Pid
	}
	return status
}

func (m *Manager) wait(cmd *exec.Cmd) {
	err := cmd.Wait()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cmd != cmd {
		return
	}
	m.cmd = nil
	if m.done != nil {
		close(m.done)
		m.done = nil
	}
	if m.state == "stopping" {
		m.state = "stopped"
		return
	}
	if err != nil {
		m.state = "error"
		m.lastError = err.Error()
		return
	}
	m.state = "stopped"
}

func (m *Manager) ensureBinary() (string, error) {
	binBytes, err := bundledFS.ReadFile(bundledBinaryName)
	if err == nil && len(binBytes) >= minBundledBinarySize {
		binDir := filepath.Join(m.baseDir, "llamacpp", "bin")
		if err := os.MkdirAll(binDir, 0o755); err != nil {
			return "", fmt.Errorf("mkdir %s: %w", binDir, err)
		}
		if err := m.extractBundledLibs(); err != nil {
			return "", err
		}
		binPath := filepath.Join(binDir, "llama-server")
		if needsExtract(binPath, int64(len(binBytes))) {
			if err := writeExecutable(binPath, binBytes); err != nil {
				return "", fmt.Errorf("extract llama-server: %w", err)
			}
		}
		return binPath, nil
	}
	if path, err := exec.LookPath("llama-server"); err == nil {
		return path, nil
	}
	// PATH lookup misses Homebrew when the app is launched from Finder, so
	// fall back to well-known install locations.
	for _, candidate := range []string{"/opt/homebrew/bin/llama-server", "/usr/local/bin/llama-server"} {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("llama-server unavailable: run scripts/fetch-llama-server.sh for packaged builds, or install llama.cpp (e.g. `brew install llama.cpp`) so llama-server is on PATH")
}

func (m *Manager) extractBundledLibs() error {
	libDir := filepath.Join(m.baseDir, "llamacpp", "lib")
	if err := os.MkdirAll(libDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", libDir, err)
	}
	err := fs.WalkDir(bundledFS, bundledLibPrefix, func(p string, d fs.DirEntry, walkErr error) error {
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
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

func (m *Manager) logWriter() io.Writer {
	return writerFunc(func(p []byte) (int, error) {
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.logs.Write(p)
	})
}

func portOpen(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", defaultHost, port), 250*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
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

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) {
	return f(p)
}

type ringBuffer struct {
	buf bytes.Buffer
}

func (r *ringBuffer) Write(p []byte) (int, error) {
	const max = 8 * 1024
	if len(p) >= max {
		r.buf.Reset()
		_, _ = r.buf.Write(p[len(p)-max:])
		return len(p), nil
	}
	if r.buf.Len()+len(p) > max {
		existing := r.buf.Bytes()
		keep := max - len(p)
		if keep < 0 {
			keep = 0
		}
		if len(existing) > keep {
			existing = existing[len(existing)-keep:]
		}
		r.buf.Reset()
		_, _ = r.buf.Write(existing)
	}
	_, _ = r.buf.Write(p)
	return len(p), nil
}

func (r *ringBuffer) String() string {
	return strings.TrimSpace(r.buf.String())
}

func (r *ringBuffer) Reset() {
	r.buf.Reset()
}

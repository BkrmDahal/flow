//go:build !darwin

package llamacpp

import "fmt"

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

type Manager struct{}

func KillStrayServers() {}

func NewManager(baseDir string) *Manager { return &Manager{} }

func BaseURL(port int) string {
	if port == 0 {
		port = 8080
	}
	return fmt.Sprintf("http://127.0.0.1:%d/v1", port)
}

func (m *Manager) Start(modelPath string, port, contextSize int) error {
	return fmt.Errorf("managed llama.cpp is currently supported on macOS only")
}

func (m *Manager) Stop() error { return nil }

func (m *Manager) Status() Status {
	return Status{State: "unsupported", Port: 8080, BaseURL: BaseURL(8080), ContextSize: 4096}
}

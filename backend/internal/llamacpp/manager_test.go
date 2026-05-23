//go:build darwin

package llamacpp

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStartRejectsInvalidModelPath(t *testing.T) {
	m := NewManager(t.TempDir())
	err := m.Start("", 8080, 4096)
	if err == nil || !strings.Contains(err.Error(), "choose a GGUF model") {
		t.Fatalf("Start error = %v, want choose model error", err)
	}
	if status := m.Status(); status.State != "stopped" {
		t.Fatalf("state = %q, want stopped", status.State)
	}
}

func TestStartDoesNotOwnExternalServerOnPortConflict(t *testing.T) {
	dir := t.TempDir()
	modelPath := filepath.Join(dir, "model.gguf")
	if err := os.WriteFile(modelPath, []byte("placeholder"), 0o644); err != nil {
		t.Fatal(err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("sandbox does not permit listening sockets: %v", err)
		}
		t.Fatal(err)
	}
	defer ln.Close()

	port := ln.Addr().(*net.TCPAddr).Port
	m := NewManager(dir)
	err = m.Start(modelPath, port, 4096)
	if err == nil || !strings.Contains(err.Error(), "already in use") {
		t.Fatalf("Start error = %v, want port conflict", err)
	}
	status := m.Status()
	if status.Owned || status.Running {
		t.Fatalf("status = %+v, want not owned/running", status)
	}
	if err := m.Stop(); err != nil {
		t.Fatalf("Stop should be a no-op for unowned server: %v", err)
	}
}

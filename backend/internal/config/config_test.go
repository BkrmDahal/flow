package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAppliesLlamaDefaults(t *testing.T) {
	base := t.TempDir()
	data := []byte(`{
  "provider_type": "local-openai",
  "provider_label": "LM Studio",
  "base_url": "http://localhost:1234/v1",
  "api_key": "lm-studio"
}`)
	if err := os.WriteFile(filepath.Join(base, "config.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(base)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LlamaPort != 8080 {
		t.Fatalf("LlamaPort = %d, want 8080", cfg.LlamaPort)
	}
	if cfg.LlamaContextSize != 4096 {
		t.Fatalf("LlamaContextSize = %d, want 4096", cfg.LlamaContextSize)
	}
}

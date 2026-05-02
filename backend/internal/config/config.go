package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// AgentConfig defines the configuration for a single named agent persona.
type AgentConfig struct {
	Name           string `json:"name"`
	Model          string `json:"model"`
	PromptPath     string `json:"prompt_path"`
	SessionPrefix  string `json:"session_prefix"`
	EnableThinking bool   `json:"enable_thinking"`
}

// Config is the application configuration persisted to ~/.flow/config.json.
type Config struct {
	// ── Local / OpenAI-compatible provider (Cowork) ──────────────────────────
	ProviderType  string `json:"provider_type"`  // "local-openai" | "anthropic" | "openai" | "gemini"
	ProviderLabel string `json:"provider_label"` // display label
	BaseURL       string `json:"base_url"`       // e.g. http://localhost:1234/v1
	APIKey        string `json:"api_key"`        // API key for OpenAI-compat providers
	Model         string `json:"model"`          // model id for Cowork

	// ── Cloud provider keys (agent/chat modes) ───────────────────────────────
	AnthropicKey string `json:"anthropic_key"` // Anthropic Claude API key
	OpenAIKey    string `json:"openai_key"`    // OpenAI API key
	GeminiKey    string `json:"gemini_key"`    // Google Gemini API key
	DeepgramKey  string `json:"deepgram_key"` // Deepgram STT API key

	// ── Named agent configs (keyed by name, e.g. "main") ────────────────────
	Agents map[string]AgentConfig `json:"agents,omitempty"`

	// ── Push-to-talk hotkey ───────────────────────────────────────────────────
	HotkeyEnabled  bool   `json:"hotkey_enabled"`
	HotkeyModifier string `json:"hotkey_modifier"` // "right_option" (default), "left_option", etc.

	// ── Speech-to-text provider for dictation hotkey ─────────────────────────
	SpeechProvider string `json:"speech_provider"` // "local" | "whisper" | "deepgram"
	SpeechAPIKey   string `json:"speech_api_key"`  // OpenAI or Deepgram API key
	SpeechModel    string `json:"speech_model"`    // e.g. "gpt-4o-mini-transcribe" or "base.en"
	SpeechLanguage string `json:"speech_language"` // e.g. "en"
	SpeechPrompt   string `json:"speech_prompt"`   // optional transcription guidance

	// ── Auto-refine transcript ────────────────────────────────────────────────
	AutoRefineAction       string `json:"auto_refine_action"`        // "off" | "clean" | "summarize" | "bullets" | "custom"
	AutoRefineCustomPrompt string `json:"auto_refine_custom_prompt"` // custom prompt if action is "custom"
}

// FlowDir returns the resolved path to ~/.flow/.
func FlowDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".flow"), nil
}

// Bootstrap creates ~/.flow/ and the default config.json if missing.
func Bootstrap() (string, error) {
	base, err := FlowDir()
	if err != nil {
		return "", err
	}

	dirs := []string{
		filepath.Join(base, "flow"),
		filepath.Join(base, "cowork"),
		filepath.Join(base, "agents"),
		filepath.Join(base, "sessions"),
		filepath.Join(base, "memory"),
		filepath.Join(base, "workspace"),
		filepath.Join(base, "plugins", "commands"),
		filepath.Join(base, "plugins", "skills"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return "", fmt.Errorf("mkdir %s: %w", d, err)
		}
	}

	cfgPath := filepath.Join(base, "config.json")
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		def := Config{
			ProviderType:     "local-openai",
			ProviderLabel:    "LM Studio",
			BaseURL:          "http://localhost:1234/v1",
			APIKey:           "lm-studio",
			Model:            "",
			HotkeyEnabled:    false,
			HotkeyModifier:   "right_option",
			SpeechProvider:   "local",
			SpeechModel:      "base.en",
			SpeechLanguage:   "en",
			AutoRefineAction: "off",
			Agents: map[string]AgentConfig{
				"main": {
					Name:           "Flow",
					Model:          "claude-sonnet-4-5-20250929",
					PromptPath:     "workspace/Master_prompt.md",
					SessionPrefix:  "agent_main",
					EnableThinking: false,
				},
			},
		}
		data, _ := json.MarshalIndent(def, "", "  ")
		if err := os.WriteFile(cfgPath, data, 0o644); err != nil {
			return "", fmt.Errorf("write config.json: %w", err)
		}
	}

	// Default Master_prompt.md.
	promptPath := filepath.Join(base, "workspace", "Master_prompt.md")
	if _, err := os.Stat(promptPath); os.IsNotExist(err) {
		prompt := `# Flow — System Prompt

You are Flow, a helpful and capable AI assistant.
You have access to tools for running shell commands, reading and writing files,
and managing persistent memory.

## Task Planning

For multi-step tasks, use the todo_write tool to create a visible plan, then
update it as you complete each step.

## Memory

You have persistent memory. Use save_memory to remember important information,
memory_search to recall it, and delete_memory to remove outdated entries.
`
		_ = os.WriteFile(promptPath, []byte(prompt), 0o644)
	}

	// Default plugins.json.
	pluginsPath := filepath.Join(base, "plugins.json")
	if _, err := os.Stat(pluginsPath); os.IsNotExist(err) {
		defaultPlugins := map[string]interface{}{
			"commands": []interface{}{},
			"skills":   []interface{}{},
		}
		data, _ := json.MarshalIndent(defaultPlugins, "", "  ")
		_ = os.WriteFile(pluginsPath, data, 0o644)
	}

	// Default exec-approvals.json.
	approvalsPath := filepath.Join(base, "exec-approvals.json")
	if _, err := os.Stat(approvalsPath); os.IsNotExist(err) {
		defaultApprovals := map[string][]string{
			"allowed": {"ls", "cat", "echo", "pwd", "date", "whoami", "uname", "head", "tail", "wc", "grep", "find", "which", "env", "go"},
			"blocked": {"rm -rf /", "mkfs", "dd if="},
		}
		data, _ := json.MarshalIndent(defaultApprovals, "", "  ")
		_ = os.WriteFile(approvalsPath, data, 0o644)
	}

	return base, nil
}

// Load reads ~/.flow/config.json from `base`.
func Load(base string) (*Config, error) {
	f, err := os.Open(filepath.Join(base, "config.json"))
	if err != nil {
		return nil, fmt.Errorf("open config.json: %w", err)
	}
	defer f.Close()

	var cfg Config
	if err := json.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decode config.json: %w", err)
	}

	if cfg.ProviderType == "" {
		cfg.ProviderType = "local-openai"
	}
	if cfg.HotkeyModifier == "" {
		cfg.HotkeyModifier = "right_option"
	}
	if cfg.SpeechProvider == "" {
		cfg.SpeechProvider = "local"
	}
	// Migrate stranded remote configs (provider set but no key) onto the local
	// engine — otherwise they'd fail every Record click with "setup API key".
	if (cfg.SpeechProvider == "whisper" || cfg.SpeechProvider == "deepgram") && cfg.SpeechAPIKey == "" {
		cfg.SpeechProvider = "local"
		cfg.SpeechModel = ""
	}
	if cfg.SpeechModel == "" {
		if cfg.SpeechProvider == "local" {
			cfg.SpeechModel = "base.en"
		} else {
			cfg.SpeechModel = "gpt-4o-mini-transcribe"
		}
	}
	if cfg.SpeechLanguage == "" {
		cfg.SpeechLanguage = "en"
	}
	if cfg.AutoRefineAction == "" {
		cfg.AutoRefineAction = "off"
	}
	return &cfg, nil
}

// Save writes the configuration back to ~/.flow/config.json.
func Save(base string, cfg *Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(filepath.Join(base, "config.json"), data, 0o644); err != nil {
		return fmt.Errorf("write config.json: %w", err)
	}
	return nil
}

// ExecApprovals holds allowlisted and blocklisted command prefixes.
type ExecApprovals struct {
	Allowed []string `json:"allowed"`
	Blocked []string `json:"blocked"`
}

// LoadExecApprovals reads exec-approvals.json from the base directory.
func LoadExecApprovals(base string) (*ExecApprovals, error) {
	path := filepath.Join(base, "exec-approvals.json")
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open exec-approvals.json: %w", err)
	}
	defer f.Close()
	var approvals ExecApprovals
	if err := json.NewDecoder(f).Decode(&approvals); err != nil {
		return nil, fmt.Errorf("decode exec-approvals.json: %w", err)
	}
	return &approvals, nil
}

// SaveExecApprovals writes the allowed/blocked command lists to exec-approvals.json.
func SaveExecApprovals(base string, approvals *ExecApprovals) error {
	path := filepath.Join(base, "exec-approvals.json")
	data, err := json.MarshalIndent(approvals, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal exec-approvals: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write exec-approvals.json: %w", err)
	}
	return nil
}

package backend

import (
	"fmt"
	"log"

	"github.com/user/flow/backend/internal/config"
	"github.com/user/flow/backend/internal/llm"
)

// SettingsPayload is the data shape exchanged with the frontend.
type SettingsPayload struct {
	// Local / OpenAI-compatible provider (Cowork)
	ProviderType  string `json:"providerType"`
	ProviderLabel string `json:"providerLabel"`
	BaseURL       string `json:"baseUrl"`
	APIKey        string `json:"apiKey"`
	Model         string `json:"model"`

	LlamaManagedEnabled bool   `json:"llamaManagedEnabled"`
	LlamaModelPath      string `json:"llamaModelPath"`
	LlamaDownloadURL    string `json:"llamaDownloadURL"`
	LlamaPort           int    `json:"llamaPort"`
	LlamaContextSize    int    `json:"llamaContextSize"`

	// Cloud provider keys (agent/chat)
	AnthropicKey  string `json:"anthropicKey"`
	OpenAIKey     string `json:"openaiKey"`
	GeminiKey     string `json:"geminiKey"`
	OpenRouterKey string `json:"openRouterKey"`
	DeepgramKey   string `json:"deepgramKey"`

	// Provider mode and cloud-tab selection
	ProviderMode   string `json:"providerMode"` // "local" | "cloud"
	CloudProvider  string `json:"cloudProvider"`
	CloudModel     string `json:"cloudModel"`
	CustomCloudURL string `json:"customCloudURL"`
	CustomCloudKey string `json:"customCloudKey"`

	// Hotkey
	HotkeyEnabled  bool   `json:"hotkeyEnabled"`
	HotkeyModifier string `json:"hotkeyModifier"`

	// STT
	SpeechProvider string `json:"speechProvider"`
	SpeechAPIKey   string `json:"speechApiKey"`
	SpeechModel    string `json:"speechModel"`
	SpeechLanguage string `json:"speechLanguage"`
	SpeechPrompt   string `json:"speechPrompt"`

	// Auto-refine
	AutoRefineAction       string `json:"autoRefineAction"`
	AutoRefineCustomPrompt string `json:"autoRefineCustomPrompt"`

	// Python Executable Path
	PythonPath string `json:"pythonPath"`
}

// GetSettings returns the current persisted configuration.
func (a *App) GetSettings() (*SettingsPayload, error) {
	if a.cfg == nil {
		return nil, fmt.Errorf("config not loaded")
	}
	return toPayload(a.cfg), nil
}

// SaveSettings persists the supplied payload and rebuilds the LLM client.
func (a *App) SaveSettings(p SettingsPayload) error {
	if a.baseDir == "" {
		return fmt.Errorf("baseDir not initialised")
	}

	oldHotkeyEnabled := a.cfg != nil && a.cfg.HotkeyEnabled

	cfg := fromPayload(p)
	if cfg.LlamaManagedEnabled {
		if cfg.LlamaPort == 0 {
			cfg.LlamaPort = 8080
		}
		cfg.BaseURL = fmt.Sprintf("http://127.0.0.1:%d/v1", cfg.LlamaPort)
		cfg.APIKey = ""
	}
	// Preserve existing agents config — settings panel doesn't touch it.
	if a.cfg != nil && a.cfg.Agents != nil {
		cfg.Agents = a.cfg.Agents
	}

	if err := config.Save(a.baseDir, cfg); err != nil {
		return err
	}
	a.cfg = cfg

	if err := a.rebuildLLMClient(); err != nil {
		log.Printf("flow: rebuild LLM client failed: %v", err)
	}

	// Toggle dictation if hotkey state changed.
	if cfg.HotkeyEnabled && !oldHotkeyEnabled {
		a.setupDictationIfEnabled()
	} else if !cfg.HotkeyEnabled && oldHotkeyEnabled {
		a.ToggleDictation(false)
	} else if cfg.HotkeyEnabled {
		// Re-setup with new settings (modifier/API key may have changed).
		a.ToggleDictation(false)
		a.setupDictationIfEnabled()
	}

	return nil
}

// GetStatus returns a summary of the app's runtime state for the frontend.
func (a *App) GetStatus() map[string]interface{} {
	status := map[string]interface{}{
		"ready":        a.llm != nil,
		"model":        "",
		"providerType": "",
		"hasAnthropic": false,
		"hasOpenAI":    false,
		"hasGemini":    false,
		"hasDeepgram":  false,
	}
	if a.cfg != nil {
		status["model"] = a.cfg.Model
		status["providerType"] = a.cfg.ProviderType
		status["hasAnthropic"] = a.cfg.AnthropicKey != ""
		status["hasOpenAI"] = a.cfg.OpenAIKey != "" || a.cfg.APIKey != ""
		status["hasGemini"] = a.cfg.GeminiKey != ""
		status["hasDeepgram"] = a.cfg.DeepgramKey != "" || a.cfg.SpeechAPIKey != ""
	}
	if a.llm != nil {
		status["model"] = a.llm.GetModel()
	}
	return status
}

// GetExecApprovals returns the current exec-approvals.json contents.
func (a *App) GetExecApprovals() (*config.ExecApprovals, error) {
	return config.LoadExecApprovals(a.baseDir)
}

// SaveExecApprovals writes updated allowed/blocked command lists.
func (a *App) SaveExecApprovals(allowed []string, blocked []string) error {
	return config.SaveExecApprovals(a.baseDir, &config.ExecApprovals{
		Allowed: allowed,
		Blocked: blocked,
	})
}

// rebuildLLMClient swaps a.llm based on the current config. The user-chosen
// ProviderMode ("local" or "cloud") picks the primary path; the other side
// is the fallback. Legacy agent-map config is the last resort.
func (a *App) rebuildLLMClient() error {
	if a.cfg == nil {
		a.llm = nil
		return nil
	}

	tryLocal := func() (llm.LLMClient, error) {
		if a.cfg.Model == "" || a.cfg.BaseURL == "" {
			return nil, fmt.Errorf("local provider not configured")
		}
		return llm.NewClient(a.cfg)
	}
	tryCloud := func() (llm.LLMClient, error) {
		if a.cfg.CloudProvider == "" || a.cfg.CloudModel == "" {
			return nil, fmt.Errorf("cloud provider not configured")
		}
		return llm.NewCloudClient(
			a.cfg.CloudProvider, a.cfg.CloudModel,
			a.cfg.AnthropicKey, a.cfg.OpenAIKey, a.cfg.OpenRouterKey,
			a.cfg.CustomCloudURL, a.cfg.CustomCloudKey,
		)
	}

	primary, fallback := tryLocal, tryCloud
	primaryName, fallbackName := "local", "cloud"
	if a.cfg.ProviderMode == "cloud" {
		primary, fallback = tryCloud, tryLocal
		primaryName, fallbackName = "cloud", "local"
	}

	if client, err := primary(); err == nil {
		a.llm = client
		log.Printf("flow: LLM client ready (%s provider)", primaryName)
		return nil
	} else {
		log.Printf("flow: %s LLM not ready: %v", primaryName, err)
	}
	if client, err := fallback(); err == nil {
		a.llm = client
		log.Printf("flow: LLM client ready (%s provider, fallback)", fallbackName)
		return nil
	} else {
		log.Printf("flow: %s LLM not ready: %v", fallbackName, err)
	}

	// Last resort: legacy agent-map model.
	agentModel := ""
	if a.cfg.Agents != nil {
		if ac, ok := a.cfg.Agents["main"]; ok {
			agentModel = ac.Model
		}
	}
	if agentModel != "" {
		if client, err := llm.NewClientForModel(agentModel, a.cfg.AnthropicKey, a.cfg.OpenAIKey, a.cfg.GeminiKey); err == nil {
			a.llm = client
			log.Printf("flow: LLM client ready (agent model=%q)", agentModel)
			return nil
		}
	}

	a.llm = nil
	return nil
}

func toPayload(c *config.Config) *SettingsPayload {
	return &SettingsPayload{
		ProviderType:           c.ProviderType,
		ProviderLabel:          c.ProviderLabel,
		BaseURL:                c.BaseURL,
		APIKey:                 c.APIKey,
		Model:                  c.Model,
		LlamaManagedEnabled:    c.LlamaManagedEnabled,
		LlamaModelPath:         c.LlamaModelPath,
		LlamaDownloadURL:       c.LlamaDownloadURL,
		LlamaPort:              c.LlamaPort,
		LlamaContextSize:       c.LlamaContextSize,
		AnthropicKey:           c.AnthropicKey,
		OpenAIKey:              c.OpenAIKey,
		GeminiKey:              c.GeminiKey,
		OpenRouterKey:          c.OpenRouterKey,
		DeepgramKey:            c.DeepgramKey,
		ProviderMode:           c.ProviderMode,
		CloudProvider:          c.CloudProvider,
		CloudModel:             c.CloudModel,
		CustomCloudURL:         c.CustomCloudURL,
		CustomCloudKey:         c.CustomCloudKey,
		HotkeyEnabled:          c.HotkeyEnabled,
		HotkeyModifier:         c.HotkeyModifier,
		SpeechProvider:         c.SpeechProvider,
		SpeechAPIKey:           c.SpeechAPIKey,
		SpeechModel:            c.SpeechModel,
		SpeechLanguage:         c.SpeechLanguage,
		SpeechPrompt:           c.SpeechPrompt,
		AutoRefineAction:       c.AutoRefineAction,
		AutoRefineCustomPrompt: c.AutoRefineCustomPrompt,
		PythonPath:             c.PythonPath,
	}
}

func fromPayload(p SettingsPayload) *config.Config {
	return &config.Config{
		ProviderType:           p.ProviderType,
		ProviderLabel:          p.ProviderLabel,
		BaseURL:                p.BaseURL,
		APIKey:                 p.APIKey,
		Model:                  p.Model,
		LlamaManagedEnabled:    p.LlamaManagedEnabled,
		LlamaModelPath:         p.LlamaModelPath,
		LlamaDownloadURL:       p.LlamaDownloadURL,
		LlamaPort:              p.LlamaPort,
		LlamaContextSize:       p.LlamaContextSize,
		AnthropicKey:           p.AnthropicKey,
		OpenAIKey:              p.OpenAIKey,
		GeminiKey:              p.GeminiKey,
		OpenRouterKey:          p.OpenRouterKey,
		DeepgramKey:            p.DeepgramKey,
		ProviderMode:           p.ProviderMode,
		CloudProvider:          p.CloudProvider,
		CloudModel:             p.CloudModel,
		CustomCloudURL:         p.CustomCloudURL,
		CustomCloudKey:         p.CustomCloudKey,
		HotkeyEnabled:          p.HotkeyEnabled,
		HotkeyModifier:         p.HotkeyModifier,
		SpeechProvider:         p.SpeechProvider,
		SpeechAPIKey:           p.SpeechAPIKey,
		SpeechModel:            p.SpeechModel,
		SpeechLanguage:         p.SpeechLanguage,
		SpeechPrompt:           p.SpeechPrompt,
		AutoRefineAction:       p.AutoRefineAction,
		AutoRefineCustomPrompt: p.AutoRefineCustomPrompt,
		PythonPath:             p.PythonPath,
	}
}

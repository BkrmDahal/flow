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

	// Cloud provider keys (agent/chat)
	AnthropicKey string `json:"anthropicKey"`
	OpenAIKey    string `json:"openaiKey"`
	GeminiKey    string `json:"geminiKey"`
	DeepgramKey  string `json:"deepgramKey"`

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
	AutoRefineAction string `json:"autoRefineAction"`
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

// rebuildLLMClient swaps a.llm based on the current config.
// For Cowork / local use-case, uses NewClient (OpenAI-compat).
// For agent tab, uses NewClientForModel (multi-provider).
func (a *App) rebuildLLMClient() error {
	if a.cfg == nil {
		a.llm = nil
		return nil
	}

	// Try building a client for the agent model first.
	agentModel := ""
	if a.cfg.Agents != nil {
		if ac, ok := a.cfg.Agents["main"]; ok {
			agentModel = ac.Model
		}
	}

	if agentModel != "" {
		client, err := llm.NewClientForModel(agentModel, a.cfg.AnthropicKey, a.cfg.OpenAIKey, a.cfg.GeminiKey)
		if err == nil {
			a.llm = client
			log.Printf("flow: LLM client ready for agent (model=%q)", agentModel)
			return nil
		}
		log.Printf("flow: agent LLM not ready: %v", err)
	}

	// Fall back to the Cowork local/OpenAI-compat client.
	if a.cfg.Model == "" || a.cfg.BaseURL == "" {
		a.llm = nil
		return nil
	}
	client, err := llm.NewClient(a.cfg)
	if err != nil {
		a.llm = nil
		return err
	}
	a.llm = client
	log.Printf("flow: LLM client ready (provider=%q model=%q)", a.cfg.ProviderLabel, a.cfg.Model)
	return nil
}

func toPayload(c *config.Config) *SettingsPayload {
	return &SettingsPayload{
		ProviderType:     c.ProviderType,
		ProviderLabel:    c.ProviderLabel,
		BaseURL:          c.BaseURL,
		APIKey:           c.APIKey,
		Model:            c.Model,
		AnthropicKey:     c.AnthropicKey,
		OpenAIKey:        c.OpenAIKey,
		GeminiKey:        c.GeminiKey,
		DeepgramKey:      c.DeepgramKey,
		HotkeyEnabled:    c.HotkeyEnabled,
		HotkeyModifier:   c.HotkeyModifier,
		SpeechProvider:   c.SpeechProvider,
		SpeechAPIKey:     c.SpeechAPIKey,
		SpeechModel:      c.SpeechModel,
		SpeechLanguage:   c.SpeechLanguage,
		SpeechPrompt:     c.SpeechPrompt,
		AutoRefineAction: c.AutoRefineAction,
	}
}

func fromPayload(p SettingsPayload) *config.Config {
	return &config.Config{
		ProviderType:     p.ProviderType,
		ProviderLabel:    p.ProviderLabel,
		BaseURL:          p.BaseURL,
		APIKey:           p.APIKey,
		Model:            p.Model,
		AnthropicKey:     p.AnthropicKey,
		OpenAIKey:        p.OpenAIKey,
		GeminiKey:        p.GeminiKey,
		DeepgramKey:      p.DeepgramKey,
		HotkeyEnabled:    p.HotkeyEnabled,
		HotkeyModifier:   p.HotkeyModifier,
		SpeechProvider:   p.SpeechProvider,
		SpeechAPIKey:     p.SpeechAPIKey,
		SpeechModel:      p.SpeechModel,
		SpeechLanguage:   p.SpeechLanguage,
		SpeechPrompt:     p.SpeechPrompt,
		AutoRefineAction: p.AutoRefineAction,
	}
}

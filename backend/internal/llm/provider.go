package llm

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/user/flow/backend/internal/config"
)

// Provider identifies which LLM API backend to use.
type Provider string

const (
	ProviderAnthropic  Provider = "anthropic"
	ProviderOpenAI     Provider = "openai"
	ProviderGemini     Provider = "gemini"
	ProviderOpenRouter Provider = "openrouter"
	ProviderCustom     Provider = "custom"
	ProviderLocal      Provider = "local-openai"
)

const openRouterAPIURL = "https://openrouter.ai/api/v1/chat/completions"

// NewOpenRouterClient builds an OpenAI-compatible client pointed at OpenRouter.
func NewOpenRouterClient(apiKey, model string) *OpenAIClient {
	return &OpenAIClient{
		APIKey:        apiKey,
		Model:         model,
		BaseURL:       openRouterAPIURL,
		ProviderLabel: "OpenRouter",
		HTTPClient:    &http.Client{},
	}
}

// NewCloudClient builds the LLM client for the explicit Cloud-tab selection.
// kind is one of "openai" | "anthropic" | "openrouter" | "custom". For
// "custom", customURL is the chat-completions endpoint and customKey the API
// key.
func NewCloudClient(kind, model, anthropicKey, openaiKey, openRouterKey, customURL, customKey string) (LLMClient, error) {
	if model == "" {
		return nil, fmt.Errorf("no cloud model selected")
	}
	switch Provider(kind) {
	case ProviderAnthropic:
		if anthropicKey == "" {
			return nil, fmt.Errorf("Anthropic API key not configured")
		}
		return NewAnthropicClient(anthropicKey, model), nil
	case ProviderOpenAI:
		if openaiKey == "" {
			return nil, fmt.Errorf("OpenAI API key not configured")
		}
		return NewOpenAIClient(openaiKey, model), nil
	case ProviderOpenRouter:
		if openRouterKey == "" {
			return nil, fmt.Errorf("OpenRouter API key not configured")
		}
		return NewOpenRouterClient(openRouterKey, model), nil
	case ProviderCustom:
		base := strings.TrimRight(customURL, "/")
		if base == "" {
			return nil, fmt.Errorf("custom cloud URL is empty")
		}
		c := NewOpenAIClient(customKey, model)
		// Accept either a /v1 base or a full /chat/completions URL.
		if strings.HasSuffix(base, "/chat/completions") {
			c.BaseURL = base
		} else {
			c.BaseURL = base + "/chat/completions"
		}
		c.ProviderLabel = "Custom Cloud"
		return c, nil
	default:
		return nil, fmt.Errorf("cloud provider %q not supported", kind)
	}
}

// DetectProvider returns the API provider for a given model ID.
func DetectProvider(model string) Provider {
	lower := strings.ToLower(model)
	if strings.HasPrefix(lower, "gemini-") {
		return ProviderGemini
	}
	if strings.HasPrefix(lower, "gpt-") ||
		strings.HasPrefix(lower, "o1-") ||
		strings.HasPrefix(lower, "o3-") ||
		strings.HasPrefix(lower, "o4-") {
		return ProviderOpenAI
	}
	// Default to Anthropic (claude-* models and anything else).
	return ProviderAnthropic
}

// geminiAPIURL is Google's OpenAI-compatible Chat Completions endpoint.
const geminiAPIURL = "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions"

// NewGeminiClient creates an LLM client that talks to the Gemini API via
// Google's OpenAI-compatible endpoint. It reuses the OpenAIClient
// implementation with a different base URL.
func NewGeminiClient(apiKey, model string) *OpenAIClient {
	return &OpenAIClient{
		APIKey:        apiKey,
		Model:         model,
		BaseURL:       geminiAPIURL,
		ProviderLabel: "Gemini",
		HTTPClient:    &http.Client{},
	}
}

// NewClientForModel creates the appropriate LLM client based on the model name.
// anthropicKey, openaiKey, and geminiKey are the respective API keys; the
// function selects which one to use based on the detected provider.
func NewClientForModel(model, anthropicKey, openaiKey, geminiKey string) (LLMClient, error) {
	provider := DetectProvider(model)
	switch provider {
	case ProviderGemini:
		if geminiKey == "" {
			return nil, fmt.Errorf("Gemini API key not configured — add it in Settings to use %s", model)
		}
		return NewGeminiClient(geminiKey, model), nil
	case ProviderOpenAI:
		if openaiKey == "" {
			return nil, fmt.Errorf("OpenAI API key not configured — add it in Settings to use %s", model)
		}
		return NewOpenAIClient(openaiKey, model), nil
	default:
		if anthropicKey == "" {
			return nil, fmt.Errorf("Anthropic API key not configured — add it in Settings to use %s", model)
		}
		return NewAnthropicClient(anthropicKey, model), nil
	}
}

// NewClient builds an LLMClient from the persisted config (used by Cowork for local/OpenAI-compat providers).
func NewClient(cfg *config.Config) (LLMClient, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is nil")
	}
	if cfg.Model == "" {
		return nil, fmt.Errorf("no model selected")
	}

	switch cfg.ProviderType {
	case "", string(ProviderLocal):
		base := strings.TrimRight(cfg.BaseURL, "/")
		if base == "" {
			return nil, fmt.Errorf("base URL is empty")
		}
		c := NewOpenAIClient(cfg.APIKey, cfg.Model)
		c.BaseURL = base + "/chat/completions"
		if cfg.ProviderLabel != "" {
			c.ProviderLabel = cfg.ProviderLabel
		} else {
			c.ProviderLabel = "Local"
		}
		return c, nil
	case string(ProviderAnthropic):
		key := cfg.AnthropicKey
		if key == "" {
			return nil, fmt.Errorf("anthropic_key not configured")
		}
		return NewAnthropicClient(key, cfg.Model), nil
	case string(ProviderOpenAI):
		key := cfg.OpenAIKey
		if key == "" {
			return nil, fmt.Errorf("openai_key not configured")
		}
		return NewOpenAIClient(key, cfg.Model), nil
	case string(ProviderGemini):
		key := cfg.GeminiKey
		if key == "" {
			return nil, fmt.Errorf("gemini_key not configured")
		}
		return NewGeminiClient(key, cfg.Model), nil
	default:
		return nil, fmt.Errorf("provider %q not supported", cfg.ProviderType)
	}
}

package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ModelInfo is one entry returned by an OpenAI-compatible /v1/models endpoint.
type ModelInfo struct {
	ID      string `json:"id"`
	OwnedBy string `json:"owned_by,omitempty"`
}

// ListLocalModels calls GET {baseURL}/models on an OpenAI-compatible local
// server (LM Studio, llama-server) and returns the list of model ids.
// `apiKey` may be empty; LM Studio accepts any non-empty bearer.
func (a *App) ListLocalModels(baseURL, apiKey string) ([]ModelInfo, error) {
	url := strings.TrimRight(baseURL, "/") + "/models"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if apiKey != "" {
		if strings.Contains(apiKey, "****") && a.cfg != nil {
			if strings.Contains(url, "openrouter.ai") {
				apiKey = a.cfg.OpenRouterKey
			} else if strings.Contains(url, "api.openai.com") {
				apiKey = a.cfg.OpenAIKey
			} else if strings.Contains(url, "api.anthropic.com") {
				apiKey = a.cfg.AnthropicKey
			} else {
				if a.cfg.CustomCloudKey != "" {
					apiKey = a.cfg.CustomCloudKey
				} else if a.cfg.APIKey != "" {
					apiKey = a.cfg.APIKey
				}
			}
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("GET %s returned %s", url, resp.Status)
	}

	var body struct {
		Data []ModelInfo `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return body.Data, nil
}

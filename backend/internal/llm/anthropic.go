package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/user/flow/backend/internal/config"
	"github.com/user/flow/backend/internal/session"
)

const anthropicAPIURL = "https://api.anthropic.com/v1/messages"
const anthropicVersion = "2023-06-01"
const defaultMaxTokens = 8192
const thinkingMaxTokens = 16000
const thinkingBudget = 10000

// ThinkingConfig controls extended thinking in the API request.
type ThinkingConfig struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens"`
}

type cacheControl struct {
	Type string `json:"type"` // "ephemeral"
}

type systemBlock struct {
	Type         string        `json:"type"` // "text"
	Text         string        `json:"text"`
	CacheControl *cacheControl `json:"cache_control,omitempty"`
}

type anthropicTool struct {
	Name         string                 `json:"name"`
	Description  string                 `json:"description"`
	InputSchema  map[string]interface{} `json:"input_schema"`
	CacheControl *cacheControl          `json:"cache_control,omitempty"`
}

// AnthropicClient is the Anthropic Messages API client.
type AnthropicClient struct {
	APIKey     string
	Model      string
	HTTPClient *http.Client
}

// NewAnthropicClient creates a new Anthropic API client.
func NewAnthropicClient(apiKey, model string) *AnthropicClient {
	return &AnthropicClient{
		APIKey:     apiKey,
		Model:      model,
		HTTPClient: &http.Client{},
	}
}

// GetModel returns the model identifier.
func (c *AnthropicClient) GetModel() string { return c.Model }

// anthropicRequest is the request body for the Messages API.
type anthropicRequest struct {
	Model     string            `json:"model"`
	MaxTokens int               `json:"max_tokens"`
	System    []systemBlock     `json:"system,omitempty"`
	Messages  []session.Message `json:"messages"`
	Tools     []anthropicTool   `json:"tools,omitempty"`
	Thinking  *ThinkingConfig   `json:"thinking,omitempty"`
	Stream    bool              `json:"stream,omitempty"`
}

func (c *AnthropicClient) getThinkingBudget(enableThinking bool) int {
	if !enableThinking {
		return 0
	}
	if baseDir, err := config.FlowDir(); err == nil {
		if cfg, err := config.Load(baseDir); err == nil && cfg != nil {
			if aCfg, ok := cfg.Agents["main"]; ok && aCfg.ThinkingBudget > 0 {
				return aCfg.ThinkingBudget
			}
		}
	}
	return thinkingBudget
}

func (c *AnthropicClient) buildRequest(system string, messages []session.Message, tools []ToolDef, enableThinking bool, isStream bool) (anthropicRequest, int) {
	budget := c.getThinkingBudget(enableThinking)
	maxTok := defaultMaxTokens
	var thinking *ThinkingConfig
	if enableThinking {
		thinking = &ThinkingConfig{Type: "enabled", BudgetTokens: budget}
		maxTok = thinkingMaxTokens
	}

	var systemBlocks []systemBlock
	if system != "" {
		systemBlocks = append(systemBlocks, systemBlock{
			Type:         "text",
			Text:         system,
			CacheControl: &cacheControl{Type: "ephemeral"},
		})
	}

	var aTools []anthropicTool
	for i, t := range tools {
		at := anthropicTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		}
		if i == len(tools)-1 {
			at.CacheControl = &cacheControl{Type: "ephemeral"}
		}
		aTools = append(aTools, at)
	}

	sanitizedMsgs := c.sanitizeMessages(messages)

	reqBody := anthropicRequest{
		Model:     c.Model,
		MaxTokens: maxTok,
		System:    systemBlocks,
		Messages:  sanitizedMsgs,
		Tools:     aTools,
		Thinking:  thinking,
		Stream:    isStream,
	}
	return reqBody, budget
}

func (c *AnthropicClient) sanitizeMessages(messages []session.Message) []session.Message {
	sanitized := make([]session.Message, len(messages))
	for i, msg := range messages {
		if msg.Role != "user" {
			sanitized[i] = msg
			continue
		}

		// Try parsing as array of content blocks
		var blocks []map[string]interface{}
		if json.Unmarshal(msg.Content, &blocks) != nil || len(blocks) == 0 {
			sanitized[i] = msg
			continue
		}

		var newBlocks []map[string]interface{}
		changed := false
		for _, block := range blocks {
			typ, _ := block["type"].(string)
			if typ == "document" {
				source, _ := block["source"].(map[string]interface{})
				if source != nil {
					mediaType, _ := source["media_type"].(string)
					// Anthropic only natively supports PDF document attachments.
					if mediaType != "application/pdf" {
						changed = true
						newBlocks = append(newBlocks, map[string]interface{}{
							"type": "text",
							"text": fmt.Sprintf("[%s document attached — content not directly available in this format]", friendlyDocName(mediaType)),
						})
						continue
					}
				}
			}
			newBlocks = append(newBlocks, block)
		}

		if changed {
			raw, _ := json.Marshal(newBlocks)
			sanitized[i] = session.Message{
				Role:    msg.Role,
				Content: raw,
			}
		} else {
			sanitized[i] = msg
		}
	}
	return sanitized
}

// SendMessages calls the Anthropic Messages API (non-streaming).
func (c *AnthropicClient) SendMessages(ctx context.Context, system string, messages []session.Message, tools []ToolDef, enableThinking bool) (*Response, error) {
	reqBody, _ := c.buildRequest(system, messages, tools, enableThinking, false)

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicAPIURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.APIKey)
	req.Header.Set("anthropic-version", anthropicVersion)

	betas := []string{"prompt-caching-2024-07-31"}
	if enableThinking {
		betas = append(betas, "interleaved-thinking-2025-05-14")
	}
	req.Header.Set("anthropic-beta", strings.Join(betas, ","))

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic API error (status %d): %s", resp.StatusCode, string(body))
	}

	// The Anthropic API returns content blocks directly — reuse the shared Response type.
	var rawResp struct {
		ID      string         `json:"id"`
		Role    string         `json:"role"`
		Content []ContentBlock `json:"content"`
		Usage   struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &rawResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &Response{
		ID:      rawResp.ID,
		Role:    rawResp.Role,
		Content: rawResp.Content,
		Usage:   rawResp.Usage,
	}, nil
}

// SendMessagesStream calls the Anthropic Messages API with streaming.
func (c *AnthropicClient) SendMessagesStream(ctx context.Context, system string, messages []session.Message, tools []ToolDef, enableThinking bool, onDelta func(StreamDelta)) (*Response, error) {
	reqBody, _ := c.buildRequest(system, messages, tools, enableThinking, true)

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicAPIURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.APIKey)
	req.Header.Set("anthropic-version", anthropicVersion)

	betas := []string{"prompt-caching-2024-07-31"}
	if enableThinking {
		betas = append(betas, "interleaved-thinking-2025-05-14")
	}
	req.Header.Set("anthropic-beta", strings.Join(betas, ","))

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("anthropic API error (status %d): %s", resp.StatusCode, string(body))
	}

	return parseSSEStream(resp.Body, onDelta)
}

// Summarize asks the model to summarize a conversation.
func (c *AnthropicClient) Summarize(ctx context.Context, system string, messages []session.Message) (string, error) {
	summaryRequest, _ := json.Marshal("Please provide a concise summary of the conversation above, preserving all key facts, decisions, and context.")
	allMsgs := append(messages, session.Message{
		Role:    "user",
		Content: summaryRequest,
	})
	resp, err := c.SendMessages(ctx, system, allMsgs, nil, false)
	if err != nil {
		return "", err
	}
	return resp.TextContent(), nil
}

// --- SSE stream types ---

type sseContentBlockStart struct {
	Index        int          `json:"index"`
	ContentBlock ContentBlock `json:"content_block"`
}

type sseContentBlockDelta struct {
	Index int `json:"index"`
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text,omitempty"`
		Thinking    string `json:"thinking,omitempty"`
		PartialJSON string `json:"partial_json,omitempty"`
		Signature   string `json:"signature,omitempty"`
	} `json:"delta"`
}

type sseContentBlockStop struct {
	Index int `json:"index"`
}

type sseMessageStart struct {
	Message struct {
		ID    string `json:"id"`
		Role  string `json:"role"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

type sseMessageDelta struct {
	Delta struct {
		StopReason string `json:"stop_reason"`
	} `json:"delta"`
	Usage struct {
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func parseSSEStream(body io.Reader, onDelta func(StreamDelta)) (*Response, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	var result Response
	var contentBlocks []ContentBlock
	var toolInputBuilders []string
	var currentEvent string

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "event: ") {
			currentEvent = strings.TrimPrefix(line, "event: ")
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		switch currentEvent {
		case "message_start":
			var evt sseMessageStart
			if err := json.Unmarshal([]byte(data), &evt); err == nil {
				result.ID = evt.Message.ID
				result.Role = evt.Message.Role
				result.Usage.InputTokens = evt.Message.Usage.InputTokens
			}

		case "content_block_start":
			var evt sseContentBlockStart
			if err := json.Unmarshal([]byte(data), &evt); err == nil {
				for len(contentBlocks) <= evt.Index {
					contentBlocks = append(contentBlocks, ContentBlock{})
					toolInputBuilders = append(toolInputBuilders, "")
				}
				contentBlocks[evt.Index] = evt.ContentBlock
				if evt.ContentBlock.Type == "thinking" {
					onDelta(StreamDelta{Type: "thinking_start"})
				}
			}

		case "content_block_delta":
			var evt sseContentBlockDelta
			if err := json.Unmarshal([]byte(data), &evt); err == nil {
				idx := evt.Index
				if idx < len(contentBlocks) {
					switch evt.Delta.Type {
					case "text_delta":
						contentBlocks[idx].Text += evt.Delta.Text
						onDelta(StreamDelta{Type: "text", Content: evt.Delta.Text})
					case "thinking_delta":
						contentBlocks[idx].Thinking += evt.Delta.Thinking
						onDelta(StreamDelta{Type: "thinking", Content: evt.Delta.Thinking})
					case "input_json_delta":
						toolInputBuilders[idx] += evt.Delta.PartialJSON
					case "signature_delta":
						contentBlocks[idx].Signature += evt.Delta.Signature
					}
				}
			}

		case "content_block_stop":
			var evt sseContentBlockStop
			if err := json.Unmarshal([]byte(data), &evt); err == nil {
				idx := evt.Index
				if idx < len(contentBlocks) && contentBlocks[idx].Type == "tool_use" {
					contentBlocks[idx].Input = json.RawMessage(toolInputBuilders[idx])
				}
			}

		case "message_delta":
			var evt sseMessageDelta
			if err := json.Unmarshal([]byte(data), &evt); err == nil {
				result.StopReason = evt.Delta.StopReason
				result.Usage.OutputTokens = evt.Usage.OutputTokens
			}

		case "error":
			var apiErr struct {
				Error struct {
					Type    string `json:"type"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(data), &apiErr); err == nil {
				return nil, fmt.Errorf("anthropic stream error (%s): %s", apiErr.Error.Type, apiErr.Error.Message)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read SSE stream: %w", err)
	}

	result.Content = contentBlocks
	return &result, nil
}

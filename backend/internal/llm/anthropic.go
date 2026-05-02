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
	System    string            `json:"system,omitempty"`
	Messages  []session.Message `json:"messages"`
	Tools     []ToolDef         `json:"tools,omitempty"`
	Thinking  *ThinkingConfig   `json:"thinking,omitempty"`
	Stream    bool              `json:"stream,omitempty"`
}

// SendMessages calls the Anthropic Messages API (non-streaming).
func (c *AnthropicClient) SendMessages(ctx context.Context, system string, messages []session.Message, tools []ToolDef, enableThinking bool) (*Response, error) {
	maxTok := defaultMaxTokens
	var thinking *ThinkingConfig
	if enableThinking {
		thinking = &ThinkingConfig{Type: "enabled", BudgetTokens: thinkingBudget}
		maxTok = thinkingMaxTokens
	}

	reqBody := anthropicRequest{
		Model:     c.Model,
		MaxTokens: maxTok,
		System:    system,
		Messages:  messages,
		Tools:     tools,
		Thinking:  thinking,
	}

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
	if enableThinking {
		req.Header.Set("anthropic-beta", "interleaved-thinking-2025-05-14")
	}

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
	maxTok := defaultMaxTokens
	var thinking *ThinkingConfig
	if enableThinking {
		thinking = &ThinkingConfig{Type: "enabled", BudgetTokens: thinkingBudget}
		maxTok = thinkingMaxTokens
	}

	reqBody := anthropicRequest{
		Model:     c.Model,
		MaxTokens: maxTok,
		System:    system,
		Messages:  messages,
		Tools:     tools,
		Thinking:  thinking,
		Stream:    true,
	}

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
	if enableThinking {
		req.Header.Set("anthropic-beta", "interleaved-thinking-2025-05-14")
	}

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

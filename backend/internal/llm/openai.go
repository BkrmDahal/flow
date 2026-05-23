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

const openaiAPIURL = "https://api.openai.com/v1/chat/completions"
const openaiDefaultMaxTokens = 8192

// OpenAIClient is the OpenAI Chat Completions API client.
// It implements the LLMClient interface and translates between the
// internal Anthropic-style message format and the OpenAI API format.
type OpenAIClient struct {
	APIKey        string
	Model         string
	BaseURL       string // API endpoint; defaults to openaiAPIURL.
	ProviderLabel string // Human-readable provider name for error messages.
	HTTPClient    *http.Client
}

// NewOpenAIClient creates a new OpenAI API client.
func NewOpenAIClient(apiKey, model string) *OpenAIClient {
	return &OpenAIClient{
		APIKey:        apiKey,
		Model:         model,
		BaseURL:       openaiAPIURL,
		ProviderLabel: "OpenAI",
		HTTPClient:    &http.Client{},
	}
}

// GetModel returns the model identifier.
func (c *OpenAIClient) GetModel() string {
	return c.Model
}

// --- OpenAI-specific types ---

type oaiMessage struct {
	Role       string        `json:"role"`
	Content    interface{}   `json:"content,omitempty"` // string or []oaiContentPart
	ToolCalls  []oaiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
}

type oaiContentPart struct {
	Type     string       `json:"type"`
	Text     string       `json:"text,omitempty"`
	ImageURL *oaiImageURL `json:"image_url,omitempty"`
}

type oaiImageURL struct {
	URL string `json:"url"`
}

type oaiToolCall struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Function oaiToolFunction `json:"function"`
	Google   *oaiGoogleExt   `json:"google,omitempty"` // Gemini thought signatures for function calls
}

// oaiGoogleExt holds Gemini-specific extensions returned by Google's
// OpenAI-compatible endpoint (nested under a "google" key on each tool call).
type oaiGoogleExt struct {
	ThoughtSignature string `json:"thought_signature,omitempty"`
}

// getThoughtSignature extracts the thought signature from the Google
// extension wrapper, returning an empty string if absent.
func (tc *oaiToolCall) getThoughtSignature() string {
	if tc.Google != nil && tc.Google.ThoughtSignature != "" {
		return tc.Google.ThoughtSignature
	}
	return ""
}

type oaiToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type oaiTool struct {
	Type     string             `json:"type"`
	Function oaiToolFunctionDef `json:"function"`
}

type oaiToolFunctionDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

type oaiRequest struct {
	Model               string         `json:"model"`
	Messages            []oaiMessage   `json:"messages"`
	Tools               []oaiTool      `json:"tools,omitempty"`
	MaxCompletionTokens int            `json:"max_completion_tokens,omitempty"`
	Stream              bool           `json:"stream,omitempty"`
	StreamOptions       *oaiStreamOpts `json:"stream_options,omitempty"`
}

type oaiStreamOpts struct {
	IncludeUsage bool `json:"include_usage"`
}

// --- Message Conversion (Anthropic-style → OpenAI) ---

// convertTools converts Anthropic-style ToolDefs to OpenAI function tools.
func oaiConvertTools(tools []ToolDef) []oaiTool {
	if len(tools) == 0 {
		return nil
	}
	result := make([]oaiTool, len(tools))
	for i, t := range tools {
		result[i] = oaiTool{
			Type: "function",
			Function: oaiToolFunctionDef{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		}
	}
	return result
}

// convertMessages converts session.Message history to OpenAI API messages.
// System prompt is prepended as a system message.
func (c *OpenAIClient) convertMessages(system string, messages []session.Message) []oaiMessage {
	var result []oaiMessage

	if system != "" {
		result = append(result, oaiMessage{Role: "system", Content: system})
	}

	for _, msg := range messages {
		result = append(result, c.convertMessage(msg)...)
	}

	return result
}

func (c *OpenAIClient) convertMessage(msg session.Message) []oaiMessage {
	switch msg.Role {
	case "user":
		return c.convertUserMessage(msg)
	case "assistant":
		return c.convertAssistantMessage(msg)
	default:
		return nil
	}
}

func (c *OpenAIClient) convertUserMessage(msg session.Message) []oaiMessage {
	// Try plain text string first.
	var text string
	if json.Unmarshal(msg.Content, &text) == nil {
		return []oaiMessage{{Role: "user", Content: text}}
	}

	// Try array of Anthropic content blocks.
	var blocks []map[string]interface{}
	if json.Unmarshal(msg.Content, &blocks) != nil || len(blocks) == 0 {
		return []oaiMessage{{Role: "user", Content: string(msg.Content)}}
	}

	// Check if this is a tool_result message.
	if typ, _ := blocks[0]["type"].(string); typ == "tool_result" {
		var result []oaiMessage
		for _, block := range blocks {
			toolUseID, _ := block["tool_use_id"].(string)
			content, _ := block["content"].(string)
			result = append(result, oaiMessage{
				Role:       "tool",
				Content:    content,
				ToolCallID: toolUseID,
			})
		}
		return result
	}

	// Multimodal content — convert to OpenAI content parts.
	var parts []oaiContentPart
	for _, block := range blocks {
		typ, _ := block["type"].(string)
		switch typ {
		case "text":
			t, _ := block["text"].(string)
			parts = append(parts, oaiContentPart{Type: "text", Text: t})
		case "image":
			source, _ := block["source"].(map[string]interface{})
			if source != nil {
				mediaType, _ := source["media_type"].(string)
				data, _ := source["data"].(string)
				parts = append(parts, oaiContentPart{
					Type: "image_url",
					ImageURL: &oaiImageURL{
						URL: fmt.Sprintf("data:%s;base64,%s", mediaType, data),
					},
				})
			}
		case "document":
			parts = append(parts, oaiContentPart{
				Type: "text",
				Text: "[PDF document attached — content not directly available in this format]",
			})
		}
	}

	if len(parts) > 0 {
		return []oaiMessage{{Role: "user", Content: parts}}
	}

	return []oaiMessage{{Role: "user", Content: string(msg.Content)}}
}

func (c *OpenAIClient) convertAssistantMessage(msg session.Message) []oaiMessage {
	var blocks []map[string]interface{}
	if json.Unmarshal(msg.Content, &blocks) != nil || len(blocks) == 0 {
		var text string
		json.Unmarshal(msg.Content, &text)
		return []oaiMessage{{Role: "assistant", Content: text}}
	}

	var textContent string
	var toolCalls []oaiToolCall

	for _, block := range blocks {
		typ, _ := block["type"].(string)
		switch typ {
		case "text":
			t, _ := block["text"].(string)
			textContent += t
		case "tool_use":
			id, _ := block["id"].(string)
			name, _ := block["name"].(string)
			input := block["input"]
			inputBytes, _ := json.Marshal(input)
			thoughtSig, _ := block["thought_signature"].(string)
			tc := oaiToolCall{
				ID:   id,
				Type: "function",
				Function: oaiToolFunction{
					Name:      name,
					Arguments: string(inputBytes),
				},
			}
			if thoughtSig != "" {
				tc.Google = &oaiGoogleExt{ThoughtSignature: thoughtSig}
			}
			toolCalls = append(toolCalls, tc)
			// "thinking" blocks are skipped — OpenAI doesn't have an equivalent.
		}
	}

	m := oaiMessage{Role: "assistant"}
	if textContent != "" {
		m.Content = textContent
	}
	if len(toolCalls) > 0 {
		m.ToolCalls = toolCalls
	}

	return []oaiMessage{m}
}

// --- API Calls ---

// SendMessages calls the OpenAI Chat Completions API (non-streaming).
func (c *OpenAIClient) SendMessages(ctx context.Context, system string, messages []session.Message, tools []ToolDef, enableThinking bool) (*Response, error) {
	oaiMsgs := c.convertMessages(system, messages)
	oaiTools := oaiConvertTools(tools)

	reqBody := oaiRequest{
		Model:               c.Model,
		Messages:            oaiMsgs,
		Tools:               oaiTools,
		MaxCompletionTokens: openaiDefaultMaxTokens,
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	apiURL := c.BaseURL
	if apiURL == "" {
		apiURL = openaiAPIURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	// Set OpenRouter-specific identification headers.
	if c.ProviderLabel == "OpenRouter" {
		req.Header.Set("HTTP-Referer", "https://github.com/user/flow")
		req.Header.Set("X-Title", "Flow Agent")
	}

	label := c.ProviderLabel
	if label == "" {
		label = "OpenAI"
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
		return nil, fmt.Errorf("%s API error (status %d): %s", label, resp.StatusCode, string(body))
	}

	return c.parseResponse(body)
}

func (c *OpenAIClient) parseResponse(body []byte) (*Response, error) {
	var oaiResp struct {
		ID      string `json:"id"`
		Choices []struct {
			Message struct {
				Role      string        `json:"role"`
				Content   *string       `json:"content"`
				ToolCalls []oaiToolCall `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}

	if err := json.Unmarshal(body, &oaiResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	result := &Response{
		ID:   oaiResp.ID,
		Role: "assistant",
	}
	result.Usage.InputTokens = oaiResp.Usage.PromptTokens
	result.Usage.OutputTokens = oaiResp.Usage.CompletionTokens

	if len(oaiResp.Choices) > 0 {
		choice := oaiResp.Choices[0]
		result.StopReason = oaiConvertFinishReason(choice.FinishReason)

		if choice.Message.Content != nil && *choice.Message.Content != "" {
			result.Content = append(result.Content, ContentBlock{
				Type: "text",
				Text: *choice.Message.Content,
			})
		}

		for _, tc := range choice.Message.ToolCalls {
			result.Content = append(result.Content, ContentBlock{
				Type:             "tool_use",
				ID:               tc.ID,
				Name:             tc.Function.Name,
				Input:            json.RawMessage(tc.Function.Arguments),
				ThoughtSignature: tc.getThoughtSignature(),
			})
		}
	}

	return result, nil
}

// SendMessagesStream calls the OpenAI Chat Completions API with streaming.
func (c *OpenAIClient) SendMessagesStream(ctx context.Context, system string, messages []session.Message, tools []ToolDef, enableThinking bool, onDelta func(StreamDelta)) (*Response, error) {
	oaiMsgs := c.convertMessages(system, messages)
	oaiTools := oaiConvertTools(tools)

	reqBody := oaiRequest{
		Model:               c.Model,
		Messages:            oaiMsgs,
		Tools:               oaiTools,
		MaxCompletionTokens: openaiDefaultMaxTokens,
		Stream:              true,
		StreamOptions:       &oaiStreamOpts{IncludeUsage: true},
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	apiURL := c.BaseURL
	if apiURL == "" {
		apiURL = openaiAPIURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	// Set OpenRouter-specific identification headers.
	if c.ProviderLabel == "OpenRouter" {
		req.Header.Set("HTTP-Referer", "https://github.com/user/flow")
		req.Header.Set("X-Title", "Flow Agent")
	}

	label := c.ProviderLabel
	if label == "" {
		label = "OpenAI"
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%s API error (status %d): %s", label, resp.StatusCode, string(body))
	}

	return c.parseStreamResponse(resp.Body, onDelta)
}

func (c *OpenAIClient) parseStreamResponse(body io.Reader, onDelta func(StreamDelta)) (*Response, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	var result Response
	result.Role = "assistant"

	var textContent string
	var toolCalls []oaiToolCall
	toolArgBuilders := map[int]string{}
	thoughtSigBuilders := map[int]string{}

	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk struct {
			ID      string `json:"id"`
			Choices []struct {
				Delta struct {
					Role      string  `json:"role"`
					Content   *string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id,omitempty"`
						Type     string `json:"type,omitempty"`
						Function struct {
							Name      string `json:"name,omitempty"`
							Arguments string `json:"arguments,omitempty"`
						} `json:"function"`
						Google *struct {
							ThoughtSignature string `json:"thought_signature,omitempty"`
						} `json:"google,omitempty"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
		}

		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if chunk.ID != "" {
			result.ID = chunk.ID
		}

		if chunk.Usage != nil {
			result.Usage.InputTokens = chunk.Usage.PromptTokens
			result.Usage.OutputTokens = chunk.Usage.CompletionTokens
		}

		if len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]

		// Text content delta.
		if choice.Delta.Content != nil && *choice.Delta.Content != "" {
			textContent += *choice.Delta.Content
			onDelta(StreamDelta{Type: "text", Content: *choice.Delta.Content})
		}

		// Tool call deltas.
		for _, tc := range choice.Delta.ToolCalls {
			idx := tc.Index

			for len(toolCalls) <= idx {
				toolCalls = append(toolCalls, oaiToolCall{Type: "function"})
			}

			if tc.ID != "" {
				toolCalls[idx].ID = tc.ID
			}

			if tc.Type != "" {
				toolCalls[idx].Type = tc.Type
			}

			if tc.Function.Name != "" {
				toolCalls[idx].Function.Name = tc.Function.Name
			}

			if tc.Function.Arguments != "" {
				toolArgBuilders[idx] += tc.Function.Arguments
			}

			if tc.Google != nil && tc.Google.ThoughtSignature != "" {
				thoughtSigBuilders[idx] += tc.Google.ThoughtSignature
			}
		}

		// Finish reason.
		if choice.FinishReason != nil {
			result.StopReason = oaiConvertFinishReason(*choice.FinishReason)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read SSE stream: %w", err)
	}

	// Assemble final content blocks.
	if textContent != "" {
		result.Content = append(result.Content, ContentBlock{
			Type: "text",
			Text: textContent,
		})
	}

	for idx, tc := range toolCalls {
		args := toolArgBuilders[idx]
		if tc.ID == "" && tc.Function.Name == "" && args == "" {
			continue
		}
		result.Content = append(result.Content, ContentBlock{
			Type:             "tool_use",
			ID:               tc.ID,
			Name:             tc.Function.Name,
			Input:            json.RawMessage(args),
			ThoughtSignature: thoughtSigBuilders[idx],
		})
	}

	return &result, nil
}

// Summarize asks the model to summarize a conversation.
func (c *OpenAIClient) Summarize(ctx context.Context, system string, messages []session.Message) (string, error) {
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

// oaiConvertFinishReason maps OpenAI finish reasons to Anthropic stop reasons.
func oaiConvertFinishReason(reason string) string {
	switch reason {
	case "stop":
		return "end_turn"
	case "tool_calls":
		return "tool_use"
	case "length":
		return "max_tokens"
	default:
		return reason
	}
}

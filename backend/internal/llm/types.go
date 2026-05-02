package llm

import "encoding/json"

// ToolDef is the JSON-schema definition of a tool the model may call.
type ToolDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

// ContentBlock represents one block in a Response.Content array.
type ContentBlock struct {
	Type             string          `json:"type"` // "text" | "tool_use" | "thinking"
	Text             string          `json:"text,omitempty"`
	Thinking         string          `json:"thinking,omitempty"`
	Signature        string          `json:"signature,omitempty"`
	ID               string          `json:"id,omitempty"`
	Name             string          `json:"name,omitempty"`
	Input            json.RawMessage `json:"input,omitempty"`
	ThoughtSignature string          `json:"thought_signature,omitempty"`
}

// Response is the normalised response shape every provider produces.
type Response struct {
	ID         string         `json:"id"`
	Role       string         `json:"role"`
	Content    []ContentBlock `json:"content"`
	StopReason string         `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// HasToolUse reports whether the response contains any tool_use blocks.
func (r *Response) HasToolUse() bool {
	for _, b := range r.Content {
		if b.Type == "tool_use" {
			return true
		}
	}
	return false
}

// TextContent concatenates every text block in the response.
func (r *Response) TextContent() string {
	var s string
	for _, b := range r.Content {
		if b.Type == "text" {
			s += b.Text
		}
	}
	return s
}

// ThinkingContent concatenates every thinking block in the response.
func (r *Response) ThinkingContent() string {
	var s string
	for _, b := range r.Content {
		if b.Type == "thinking" {
			s += b.Thinking
		}
	}
	return s
}

// ToolUseBlocks returns only the tool_use content blocks.
func (r *Response) ToolUseBlocks() []ContentBlock {
	var blocks []ContentBlock
	for _, b := range r.Content {
		if b.Type == "tool_use" {
			blocks = append(blocks, b)
		}
	}
	return blocks
}

// ContentToRaw serialises the content array for storage in a session Message.
func (r *Response) ContentToRaw() json.RawMessage {
	data, _ := json.Marshal(r.Content)
	return data
}

// StreamDelta is one incremental update emitted during streaming.
type StreamDelta struct {
	Type    string // "thinking_start" | "thinking" | "text"
	Content string
}

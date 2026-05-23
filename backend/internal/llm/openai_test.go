package llm

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/user/flow/backend/internal/session"
)

func TestParseStreamResponseAssemblesToolCallWithoutInitialID(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"id":"chatcmpl-test","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"todo_write"}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-test","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"arguments":"{\"todos\""}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-test","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":":[{\"id\":\"1\",\"content\":\"Parse PDF\",\"status\":\"in_progress\"}]}"}}]},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
		"",
	}, "\n\n")

	c := &OpenAIClient{}
	resp, err := c.parseStreamResponse(strings.NewReader(stream), func(StreamDelta) {})
	if err != nil {
		t.Fatalf("parseStreamResponse returned error: %v", err)
	}

	if !resp.HasToolUse() {
		t.Fatalf("expected tool_use block, got %#v", resp.Content)
	}

	toolUse := resp.ToolUseBlocks()[0]
	if toolUse.ID != "call_1" {
		t.Fatalf("tool ID = %q, want call_1", toolUse.ID)
	}
	if toolUse.Name != "todo_write" {
		t.Fatalf("tool name = %q, want todo_write", toolUse.Name)
	}
	if string(toolUse.Input) != `{"todos":[{"id":"1","content":"Parse PDF","status":"in_progress"}]}` {
		t.Fatalf("tool input = %s", toolUse.Input)
	}
}

func TestConvertUserMessage(t *testing.T) {
	msgContent := []map[string]interface{}{
		{
			"type": "image",
			"source": map[string]interface{}{
				"media_type": "image/png",
				"data":       "abcdef",
			},
		},
	}
	contentBytes, err := json.Marshal(msgContent)
	if err != nil {
		t.Fatalf("failed to marshal content: %v", err)
	}

	msg := session.Message{
		Role:    "user",
		Content: contentBytes,
	}

	client := &OpenAIClient{
		Model:         "llama-3-8b",
		ProviderLabel: "Local",
	}
	oaiMsgs := client.convertMessages("", []session.Message{msg})
	if len(oaiMsgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(oaiMsgs))
	}
	parts, ok := oaiMsgs[0].Content.([]oaiContentPart)
	if !ok {
		t.Fatalf("expected oaiContentPart slice content")
	}
	if len(parts) != 1 || parts[0].Type != "image_url" {
		t.Fatalf("expected image to convert directly to image_url, got: %#v", parts)
	}
}

func TestConvertAssistantMessageWithThinking(t *testing.T) {
	msgContent := []map[string]interface{}{
		{
			"type":     "thinking",
			"thinking": "Let me think about coding.",
		},
		{
			"type": "text",
			"text": "Hello world!",
		},
	}
	contentBytes, err := json.Marshal(msgContent)
	if err != nil {
		t.Fatalf("failed to marshal content: %v", err)
	}

	msg := session.Message{
		Role:    "assistant",
		Content: contentBytes,
	}

	// 1. Test standard model (like GPT-4o) - should format reasoning inside <think> tags in Content
	standardClient := &OpenAIClient{
		Model:         "gpt-4o",
		ProviderLabel: "OpenAI",
	}
	oaiMsgsStandard := standardClient.convertMessages("", []session.Message{msg})
	if len(oaiMsgsStandard) != 1 {
		t.Fatalf("expected 1 message, got %d", len(oaiMsgsStandard))
	}
	standardMsg := oaiMsgsStandard[0]
	if standardMsg.Role != "assistant" {
		t.Fatalf("expected assistant role")
	}
	textBlock, ok := standardMsg.Content.(string)
	if !ok {
		t.Fatalf("expected content string for standard client")
	}
	if !strings.Contains(textBlock, "<think>") || !strings.Contains(textBlock, "Let me think about coding.") || !strings.Contains(textBlock, "Hello world!") {
		t.Fatalf("expected reasoning inside <think> tags, got: %s", textBlock)
	}
	if standardMsg.ReasoningContent != "" {
		t.Fatalf("expected empty ReasoningContent for standard client, got: %q", standardMsg.ReasoningContent)
	}

	// 2. Test reasoning-enabled model (like deepseek-r1) - should populate ReasoningContent
	reasoningClient := &OpenAIClient{
		Model:         "deepseek-r1",
		ProviderLabel: "OpenRouter",
	}
	oaiMsgsReasoning := reasoningClient.convertMessages("", []session.Message{msg})
	if len(oaiMsgsReasoning) != 1 {
		t.Fatalf("expected 1 message, got %d", len(oaiMsgsReasoning))
	}
	reasoningMsg := oaiMsgsReasoning[0]
	if reasoningMsg.Role != "assistant" {
		t.Fatalf("expected assistant role")
	}
	if reasoningMsg.ReasoningContent != "Let me think about coding." {
		t.Fatalf("expected ReasoningContent to be populated, got: %q", reasoningMsg.ReasoningContent)
	}
	textBlockReasoning, ok := reasoningMsg.Content.(string)
	if !ok {
		t.Fatalf("expected content string")
	}
	if textBlockReasoning != "Hello world!" {
		t.Fatalf("expected Content to be only the final response text, got: %q", textBlockReasoning)
	}
}

func TestParseStreamResponseWithReasoningContent(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"id":"chatcmpl-test","choices":[{"delta":{"role":"assistant","reasoning_content":"Let me "},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-test","choices":[{"delta":{"reasoning_content":"think."},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-test","choices":[{"delta":{"content":"Hi!"},"finish_reason":"stop"}]}`,
		`data: [DONE]`,
		"",
	}, "\n\n")

	var deltaTypes []string
	var deltaContents []string
	onDelta := func(d StreamDelta) {
		deltaTypes = append(deltaTypes, d.Type)
		if d.Content != "" {
			deltaContents = append(deltaContents, d.Content)
		}
	}

	c := &OpenAIClient{}
	resp, err := c.parseStreamResponse(strings.NewReader(stream), onDelta)
	if err != nil {
		t.Fatalf("parseStreamResponse returned error: %v", err)
	}

	// Verify generated Content blocks
	if len(resp.Content) != 2 {
		t.Fatalf("expected 2 content blocks, got %d: %#v", len(resp.Content), resp.Content)
	}
	if resp.Content[0].Type != "thinking" || resp.Content[0].Thinking != "Let me think." {
		t.Fatalf("expected first block to be thinking with reasoning content, got: %#v", resp.Content[0])
	}
	if resp.Content[1].Type != "text" || resp.Content[1].Text != "Hi!" {
		t.Fatalf("expected second block to be text, got: %#v", resp.Content[1])
	}

	// Verify StreamDelta events triggered
	expectedTypes := []string{"thinking_start", "thinking", "thinking", "text"}
	if len(deltaTypes) != len(expectedTypes) {
		t.Fatalf("expected delta types %v, got %v", expectedTypes, deltaTypes)
	}
	for i, typ := range expectedTypes {
		if deltaTypes[i] != typ {
			t.Fatalf("at index %d: expected delta type %s, got %s", i, typ, deltaTypes[i])
		}
	}

	expectedContents := []string{"Let me ", "think.", "Hi!"}
	if len(deltaContents) != len(expectedContents) {
		t.Fatalf("expected delta contents %v, got %v", expectedContents, deltaContents)
	}
	for i, content := range expectedContents {
		if deltaContents[i] != content {
			t.Fatalf("at index %d: expected delta content %q, got %q", i, content, deltaContents[i])
		}
	}
}



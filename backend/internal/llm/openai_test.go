package llm

import (
	"strings"
	"testing"
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

package llm

import (
	"context"

	"github.com/user/flow/backend/internal/session"
)

// LLMClient is the interface for language model API clients.
// Both the Anthropic and OpenAI clients implement this interface,
// allowing the agent to work with either provider.
type LLMClient interface {
	// SendMessages calls the LLM API with the given conversation.
	SendMessages(ctx context.Context, system string, messages []session.Message, tools []ToolDef, enableThinking bool, reasoningEffort string) (*Response, error)

	// SendMessagesStream calls the LLM API with streaming enabled.
	SendMessagesStream(ctx context.Context, system string, messages []session.Message, tools []ToolDef, enableThinking bool, reasoningEffort string, onDelta func(StreamDelta)) (*Response, error)

	// Summarize asks the model to summarize a conversation (for context compaction).
	Summarize(ctx context.Context, system string, messages []session.Message) (string, error)

	// GetModel returns the model identifier string.
	GetModel() string
}

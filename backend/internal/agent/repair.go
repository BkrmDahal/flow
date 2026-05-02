package agent

import (
	"encoding/json"

	"github.com/user/flow/backend/internal/session"
)

// repairDanglingToolUse scans conversation history for assistant messages
// containing tool_use blocks that are not immediately followed by a user
// message with matching tool_result blocks. This can happen when a previous
// agent turn was interrupted (crash, timeout, cancellation) between
// persisting the assistant response and persisting the tool results.
//
// For each dangling tool_use, a synthetic tool_result is injected so the
// Anthropic API won't reject the conversation. Returns the repaired
// history and any synthetic messages that should be persisted to disk.
func repairDanglingToolUse(history []session.Message) ([]session.Message, []session.Message) {
	if len(history) == 0 {
		return history, nil
	}

	var repaired []session.Message
	var synthetic []session.Message

	for i := 0; i < len(history); i++ {
		repaired = append(repaired, history[i])

		if history[i].Role != "assistant" {
			continue
		}

		ids := extractToolUseIDs(history[i].Content)
		if len(ids) == 0 {
			continue
		}

		hasResults := false
		if i+1 < len(history) && history[i+1].Role == "user" {
			hasResults = hasAllToolResults(history[i+1].Content, ids)
		}

		if !hasResults {
			msg := syntheticToolResults(ids)
			repaired = append(repaired, msg)
			synthetic = append(synthetic, msg)
		}
	}

	return repaired, synthetic
}

func extractToolUseIDs(content json.RawMessage) []string {
	var blocks []map[string]interface{}
	if json.Unmarshal(content, &blocks) != nil {
		return nil
	}
	var ids []string
	for _, b := range blocks {
		if typ, _ := b["type"].(string); typ == "tool_use" {
			if id, _ := b["id"].(string); id != "" {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

func hasAllToolResults(content json.RawMessage, needed []string) bool {
	var blocks []map[string]interface{}
	if json.Unmarshal(content, &blocks) != nil {
		return false
	}
	found := map[string]bool{}
	for _, b := range blocks {
		if typ, _ := b["type"].(string); typ == "tool_result" {
			if id, _ := b["tool_use_id"].(string); id != "" {
				found[id] = true
			}
		}
	}
	for _, id := range needed {
		if !found[id] {
			return false
		}
	}
	return true
}

func syntheticToolResults(ids []string) session.Message {
	var blocks []map[string]interface{}
	for _, id := range ids {
		blocks = append(blocks, map[string]interface{}{
			"type":        "tool_result",
			"tool_use_id": id,
			"content":     "Tool execution was interrupted. The previous operation did not complete.",
		})
	}
	raw, _ := json.Marshal(blocks)
	return session.Message{
		Role:    "user",
		Content: raw,
	}
}

package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/user/flow/backend/internal/llm"
	"github.com/user/flow/backend/internal/session"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	RefineActionClean     = "clean"
	RefineActionSummarize = "summarize"
	RefineActionBullets   = "bullets"
	RefineActionCustom    = "custom"
)

var refineSystemPrompts = map[string]string{
	RefineActionClean:     "You are a transcription cleanup assistant. Fix grammar, punctuation, and disfluencies in the user's voice transcript. Preserve their voice and meaning. Output only the cleaned text — no commentary, no preamble, no quotation marks.",
	RefineActionSummarize: "Summarize the following voice note in 2 to 3 sentences. Output only the summary.",
	RefineActionBullets:   "Convert the following voice note into a clean bullet list of the key points. Output only the bullet list using - markers.",
}

// RefineFlowText runs a saved transcript through the configured local LLM.
//
// It streams text deltas to the frontend via the "flow:refine:event" Wails
// event (one event per delta plus a final {kind: "done"} or {kind: "error"}),
// persists the final text into the transcript JSON's `refinements` array,
// and returns the refined text.
func (a *App) RefineFlowText(transcriptID, action, customPrompt string) (string, error) {
	if a.llm == nil {
		return "", fmt.Errorf("no model configured — set up Settings first")
	}

	t, err := a.LoadFlowTranscript(transcriptID)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(t.Text) == "" {
		return "", fmt.Errorf("transcript is empty")
	}

	systemPrompt := promptFor(action, customPrompt)
	if systemPrompt == "" {
		return "", fmt.Errorf("unknown refine action %q", action)
	}

	contentJSON, err := json.Marshal(t.Text)
	if err != nil {
		return "", fmt.Errorf("encode transcript: %w", err)
	}
	messages := []session.Message{
		{Role: "user", Content: contentJSON},
	}

	ctx, cancel := context.WithTimeout(a.ctx, 90*time.Second)
	defer cancel()

	emit := func(kind string, text string) {
		wailsRuntime.EventsEmit(a.ctx, "flow:refine:event", map[string]interface{}{
			"transcriptId": transcriptID,
			"action":       action,
			"kind":         kind,
			"text":         text,
		})
	}

	emit("start", "")

	resp, err := a.llm.SendMessagesStream(ctx, systemPrompt, messages, nil, false, "none", func(d llm.StreamDelta) {
		if d.Type == "text" && d.Content != "" {
			emit("delta", d.Content)
		}
	})
	if err != nil {
		emit("error", err.Error())
		return "", fmt.Errorf("LLM error: %w", err)
	}

	finalText := strings.TrimSpace(resp.TextContent())
	if finalText == "" {
		emit("error", "model returned empty response")
		return "", fmt.Errorf("model returned empty response")
	}

	ref := FlowRefinement{
		Action:    action,
		Model:     a.llm.GetModel(),
		Text:      finalText,
		Timestamp: time.Now().UnixMilli(),
	}
	if strings.TrimSpace(customPrompt) != "" {
		ref.CustomPrompt = customPrompt
	}
	if err := a.appendRefinement(transcriptID, ref); err != nil {
		log.Printf("[flow-refine] failed to persist refinement: %v", err)
	}

	emit("done", finalText)
	return finalText, nil
}

func promptFor(action, customPrompt string) string {
	s := strings.TrimSpace(customPrompt)
	if s != "" {
		return s + "\nOutput only the result, no preamble."
	}
	if action == RefineActionCustom {
		return ""
	}
	return refineSystemPrompts[action]
}

// RefineTextDirect runs any raw text through the configured local/cloud LLM directly.
func (a *App) RefineTextDirect(text, action, customPrompt string) (string, error) {
	if a.llm == nil {
		return "", fmt.Errorf("no model configured — set up Settings first")
	}
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("text is empty")
	}

	systemPrompt := promptFor(action, customPrompt)
	if systemPrompt == "" {
		return "", fmt.Errorf("unknown refine action %q", action)
	}

	contentJSON, err := json.Marshal(text)
	if err != nil {
		return "", fmt.Errorf("encode text: %w", err)
	}
	messages := []session.Message{
		{Role: "user", Content: contentJSON},
	}

	ctx, cancel := context.WithTimeout(a.ctx, 45*time.Second)
	defer cancel()

	resp, err := a.llm.SendMessages(ctx, systemPrompt, messages, nil, false, "none")
	if err != nil {
		return "", fmt.Errorf("LLM error: %w", err)
	}

	finalText := strings.TrimSpace(resp.TextContent())
	if finalText == "" {
		return "", fmt.Errorf("model returned empty response")
	}

	return finalText, nil
}

package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/user/flow/backend/internal/session"
	"github.com/user/flow/backend/internal/speech"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// setupDictationIfEnabled configures the dictation hotkey if the config has it enabled.
func (a *App) setupDictationIfEnabled() {
	if a.cfg == nil || !a.cfg.HotkeyEnabled {
		return
	}
	// Only cloud providers (whisper, deepgram) need an API key.
	// The local whisper.cpp provider works without one.
	isLocal := a.cfg.SpeechProvider == "local" || a.cfg.SpeechProvider == ""
	if !isLocal && a.cfg.SpeechAPIKey == "" {
		log.Println("[dictation] hotkey_enabled but no speech_api_key for cloud provider — skipping setup")
		return
	}

	modifier := a.cfg.HotkeyModifier
	if modifier == "" {
		modifier = speech.DefaultModifier
	}

	// Config loader — called each time a dictation transcription is needed.
	cfgLoader := func() (speech.TranscribeConfig, error) {
		return speech.TranscribeConfig{
			Provider: speech.Provider(a.cfg.SpeechProvider),
			APIKey:   a.cfg.SpeechAPIKey,
			Language: a.cfg.SpeechLanguage,
			Model:    a.cfg.SpeechModel,
			Prompt:   a.cfg.SpeechPrompt,
		}, nil
	}

	// Status handler — emits events to the frontend.
	onStatus := func(state speech.DictationState, text string) {
		wailsRuntime.EventsEmit(a.ctx, "flow:dictation:status", map[string]interface{}{
			"state": string(state),
			"text":  text,
		})
	}

	onError := func(errMsg string) {
		log.Printf("[dictation] error: %s", errMsg)
		wailsRuntime.EventsEmit(a.ctx, "flow:dictation:error", map[string]interface{}{
			"error": errMsg,
		})
	}

	// Grammar fixer — uses the configured LLM to fix grammar on double-tap.
	var grammarFixer speech.GrammarFixer
	if a.llm != nil {
		grammarFixer = func(selectedText string) (string, error) {
			if a.llm == nil {
				return "", fmt.Errorf("no LLM configured")
			}
			system := "You are a grammar and spelling correction assistant. Fix the grammar, spelling, and punctuation of the given text. Return ONLY the corrected text, nothing else. Do not add explanations."
			userContent, _ := json.Marshal(selectedText)
			msgs := []session.Message{
				{Role: "user", Content: userContent},
			}
			resp, err := a.llm.SendMessages(context.Background(), system, msgs, nil, false)
			if err != nil {
				return "", err
			}
			return resp.TextContent(), nil
		}
	}

	speech.SetupDictation(modifier, cfgLoader, onStatus, onError, nil, grammarFixer)
}

// ToggleDictation enables or disables the dictation hotkey at runtime.
// Called from the settings UI when the user toggles the hotkey switch.
func (a *App) ToggleDictation(enabled bool) {
	if enabled {
		a.setupDictationIfEnabled()
	} else {
		if speech.IsDictationEnabled() {
			speech.TeardownDictation()
		}
	}
}

// GetDictationState returns the current dictation state for the frontend.
func (a *App) GetDictationState() string {
	return string(speech.GetDictationState())
}

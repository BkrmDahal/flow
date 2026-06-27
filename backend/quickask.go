package backend

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/user/flow/backend/internal/agent"
	"github.com/user/flow/backend/internal/config"
	"github.com/user/flow/backend/internal/session"
	"github.com/user/flow/backend/internal/speech"
	"github.com/user/flow/backend/internal/streaming"
	"github.com/user/flow/backend/internal/tools"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// setupQuickAskIfEnabled wires the quick-ask hotkey when enabled in config.
func (a *App) setupQuickAskIfEnabled() {
	if a.cfg == nil || !a.cfg.QuickAskHotkeyEnabled {
		return
	}
	// Cloud STT providers need an API key; the local engine does not.
	isLocal := a.cfg.SpeechProvider == "local" || a.cfg.SpeechProvider == ""
	if !isLocal && a.cfg.SpeechAPIKey == "" {
		log.Println("[quickask] enabled but no speech_api_key for cloud provider — skipping setup")
		return
	}

	modifier := a.cfg.QuickAskHotkeyModifier
	if modifier == "" {
		modifier = "left_option"
	}

	cfgLoader := func() (speech.TranscribeConfig, error) {
		return speech.TranscribeConfig{
			Provider: speech.Provider(a.cfg.SpeechProvider),
			APIKey:   a.cfg.SpeechAPIKey,
			Language: a.cfg.SpeechLanguage,
			Model:    a.cfg.SpeechModel,
			Prompt:   a.cfg.SpeechPrompt,
		}, nil
	}

	onError := func(msg string) {
		log.Printf("[quickask] error: %s", msg)
		a.hudBroadcast(map[string]interface{}{"type": "error", "content": msg})
	}

	speech.SetupQuickAsk(modifier, cfgLoader, a.StartQuickAsk, a.OpenQuickAskSuggestions, onError)
	speech.UpdateMenuBarQuickAskLabel(modifier, true)
}

// StartQuickAsk is the voice entry point: it always begins a fresh session and
// injects the current on-screen context.
func (a *App) StartQuickAsk(transcript string) {
	a.quickAskTurn(transcript, true)
}

// AskInQuickAsk handles a typed/clicked request from the HUD. It continues the
// active session if there is one, otherwise starts a fresh, context-aware turn.
func (a *App) AskInQuickAsk(text string) {
	a.quickAskMu.Lock()
	hasSession := a.quickAskSession != ""
	a.quickAskMu.Unlock()
	a.quickAskTurn(text, !hasSession)
}

// quickAskTurn runs one agent turn behind the HUD. When fresh is true it opens
// a new session and captures the on-screen context (screenshot + selection);
// otherwise it continues the current session as a follow-up.
func (a *App) quickAskTurn(request string, fresh bool) {
	if a.llm == nil {
		a.hudBroadcast(map[string]interface{}{"type": "error", "content": "No model configured — open Settings first."})
		speech.ShowHUD(a.hudURL())
		return
	}

	baseDir, err := config.FlowDir()
	if err != nil {
		a.hudBroadcast(map[string]interface{}{"type": "error", "content": err.Error()})
		return
	}

	a.quickAskMu.Lock()
	if fresh || a.quickAskSession == "" {
		// Normal cowork session ID so the task shows in history and can be
		// re-opened in the main window via "open in app".
		a.quickAskSession = a.NewCoworkSession()
	}
	sessionID := a.quickAskSession
	a.quickAskMu.Unlock()

	workDir := filepath.Join(baseDir, "cowork", sessionID)

	// Let HUD-driven tasks drive other apps with less friction: pre-allow the
	// macOS automation commands for THIS session only (not the global approval
	// list). Other commands still prompt for approval in the HUD.
	if fresh {
		tools.SessionAllowedMu.Lock()
		tools.SessionAllowedCommands[workDir] = []string{"osascript", "open"}
		tools.SessionAllowedMu.Unlock()

		// Title the chat with the actual request, not the injected on-screen
		// context preamble (which otherwise becomes the sidebar title).
		_ = a.RenameCoworkSession(sessionID, titleFromText(request))
	}

	speech.ShowHUD(a.hudURL())
	a.hudBroadcast(map[string]interface{}{"type": "session", "session_id": sessionID})
	a.hudBroadcast(map[string]interface{}{"type": "user", "content": request})

	// Build the agent content. The user message stays exactly what the user
	// said/typed. Lightweight context (frontmost app, selection) always goes in
	// the system prompt; the (expensive) screenshot is attached ONLY when the
	// request actually seems to need the screen.
	var content json.RawMessage
	var ctxNote string
	if fresh {
		ctxNote = a.quickAskContextNote()
		var files []streaming.FileAttachment
		if requestNeedsScreen(request) {
			if shot := a.captureScreenshotAttachment(workDir); shot != nil {
				files = append(files, *shot)
				ctxNote += "- A screenshot of the current screen is attached to the user's message.\n"
			}
		}
		content, err = streaming.BuildContent(request, files, streaming.ContentOptions{WorkDir: workDir})
		if err != nil {
			content, _ = json.Marshal(request)
		}
	} else {
		content, _ = json.Marshal(request)
	}

	go a.runQuickAskStream(sessionID, content, workDir, ctxNote)
}

// requestNeedsScreen is a cheap, zero-latency heuristic for whether a request
// likely needs to see the screen. A false positive just attaches an unused
// screenshot; a false negative means the agent answers from text alone.
func requestNeedsScreen(request string) bool {
	r := strings.ToLower(request)
	cues := []string{
		"screen", "this ", " this?", " this.", "these", "below", "above",
		"here", "on my", "current page", "current tab", "what's on", "what is on",
		"look at", "selected", "highlighted", "visible", "showing", "on the page",
		"summarize this", "summarise this", "explain this", "fix this", "translate this",
		"what does this", "what am i", "help me with this", "rewrite this", "reply to this",
	}
	for _, c := range cues {
		if strings.Contains(r, c) {
			return true
		}
	}
	return false
}

// quickAskContextNote builds the lightweight, screenshot-free context note that
// goes into the system prompt: the frontmost app and any selected text. Both
// are cheap (no screen capture) and best-effort.
func (a *App) quickAskContextNote() string {
	var b strings.Builder
	b.WriteString("## On-screen context\n")
	b.WriteString("The user is asking from their Mac. Use the context below to answer without asking them to describe their screen. Do not repeat this context back to them.\n")
	if app := speech.FrontmostAppName(); app != "" {
		fmt.Fprintf(&b, "- Frontmost app: %s\n", app)
	}
	if sel := speech.CopySelectedText(); strings.TrimSpace(sel) != "" {
		fmt.Fprintf(&b, "- Selected text: %s\n", strings.TrimSpace(sel))
	}
	return b.String()
}

// captureScreenshotAttachment captures the current screen as a PNG attachment,
// or nil on failure (e.g. missing Screen Recording permission).
func (a *App) captureScreenshotAttachment(workDir string) *streaming.FileAttachment {
	_ = os.MkdirAll(workDir, 0o755)
	shot := filepath.Join(workDir, fmt.Sprintf("screen_%d.png", time.Now().Unix()))
	if err := tools.CaptureScreenshot(a.ctx, shot); err != nil {
		log.Printf("[quickask] screenshot failed (Screen Recording permission?): %v", err)
		return nil
	}
	data, err := os.ReadFile(shot)
	if err != nil || len(data) == 0 {
		return nil
	}
	return &streaming.FileAttachment{
		Name:     filepath.Base(shot),
		MimeType: "image/png",
		Data:     base64.StdEncoding.EncodeToString(data),
	}
}

// OpenQuickAskWindow opens the HUD as a plain chat window: no screenshot is
// captured and no suggestions are generated. Used by the "Open Quick Ask" menu
// bar item.
func (a *App) OpenQuickAskWindow() {
	a.quickAskMu.Lock()
	a.quickAskSession = "" // clean slate
	a.quickAskMu.Unlock()

	speech.ShowHUD(a.hudURL())
	// suggest:false tells the HUD to show the chat/help view immediately rather
	// than waiting on (or capturing for) suggestions.
	a.hudBroadcast(map[string]interface{}{"type": "session", "session_id": "", "suggest": false})
}

// OpenQuickAskSuggestions is the hotkey "tap" handler: it opens the HUD and
// offers a few context-aware action chips derived from the current screen.
func (a *App) OpenQuickAskSuggestions() {
	a.quickAskMu.Lock()
	a.quickAskSession = "" // a tap starts a clean slate
	a.quickAskMu.Unlock()

	speech.ShowHUD(a.hudURL())
	a.hudBroadcast(map[string]interface{}{"type": "session", "session_id": "", "suggest": true})

	if a.llm == nil {
		return
	}

	baseDir, err := config.FlowDir()
	if err != nil {
		return
	}
	tmpDir := filepath.Join(baseDir, "cowork", ".quickask-suggest")
	shot := a.captureScreenshotAttachment(tmpDir)
	if shot == nil {
		// No screenshot (e.g. permission missing) → resolve the HUD's loading
		// state so it falls back to the static help text instead of spinning.
		a.hudBroadcast(map[string]interface{}{"type": "suggestions", "items": []map[string]string{}})
		return
	}
	files := []streaming.FileAttachment{*shot}

	// Tell the HUD suggestions are on the way so it shows a loader instead of
	// the full help text (which would otherwise flash and get replaced).
	a.hudBroadcast(map[string]interface{}{"type": "suggestions_loading"})
	go a.generateSuggestions(files)
}

// generateSuggestions makes one lightweight LLM call to propose ~3 short actions
// for the captured screen, then broadcasts them to the HUD as chips.
func (a *App) generateSuggestions(files []streaming.FileAttachment) {
	prompt := "Look at the attached screenshot of the user's screen. Suggest exactly 3 short, useful actions they might want to take right now. " +
		"Respond with ONLY a JSON array of objects with \"label\" (2-4 words) and \"prompt\" (the full request to run). No prose, no markdown fences."

	content, err := streaming.BuildContent(prompt, files, streaming.ContentOptions{})
	if err != nil {
		return
	}
	msgs := []session.Message{{Role: "user", Content: content}}

	resp, err := a.llm.SendMessages(a.ctx, "", msgs, nil, false)
	if err != nil {
		log.Printf("[quickask] suggestions failed: %v", err)
		// Clear the loader so the HUD falls back to the help text.
		a.hudBroadcast(map[string]interface{}{"type": "suggestions", "items": []map[string]string{}})
		return
	}

	// Always broadcast (even when empty) so the HUD clears its loading state.
	a.hudBroadcast(map[string]interface{}{"type": "suggestions", "items": parseSuggestions(resp.TextContent())})
}

// parseSuggestions extracts the JSON array of {label, prompt} from a model reply,
// tolerating surrounding prose or code fences.
func parseSuggestions(text string) []map[string]string {
	start := strings.Index(text, "[")
	end := strings.LastIndex(text, "]")
	if start < 0 || end <= start {
		return nil
	}
	var raw []map[string]string
	if err := json.Unmarshal([]byte(text[start:end+1]), &raw); err != nil {
		return nil
	}
	var out []map[string]string
	for _, it := range raw {
		label := strings.TrimSpace(it["label"])
		p := strings.TrimSpace(it["prompt"])
		if label == "" && p == "" {
			continue
		}
		if p == "" {
			p = label
		}
		out = append(out, map[string]string{"label": label, "prompt": p})
		if len(out) == 3 {
			break
		}
	}
	return out
}

// forwardApprovalToHUD relays a tool approval request to the HUD, but only
// while a HUD-driven turn is running (so main-window tasks aren't affected).
func (a *App) forwardApprovalToHUD(eventType string, payload map[string]interface{}) {
	a.quickAskMu.Lock()
	active := a.quickAskRunning > 0
	a.quickAskMu.Unlock()
	if !active {
		return
	}
	evt := map[string]interface{}{"type": eventType}
	for k, v := range payload {
		evt[k] = v
	}
	a.hudBroadcast(evt)
}

// runQuickAskStream runs the agent and forwards every stream event to the HUD
// (via SSE) instead of the main Wails window.
func (a *App) runQuickAskStream(sessionID string, content json.RawMessage, workDir string, ctxNote string) {
	ctx, cleanup := a.streams.Start(a.ctx, sessionID)
	defer cleanup()

	a.quickAskMu.Lock()
	a.quickAskRunning++
	a.quickAskMu.Unlock()
	defer func() {
		a.quickAskMu.Lock()
		a.quickAskRunning--
		a.quickAskMu.Unlock()
	}()

	emit := func(evt agent.StreamEvent) {
		data := map[string]interface{}{
			"session_id": sessionID,
			"type":       evt.Type,
			"content":    evt.Content,
			"tool_name":  evt.ToolName,
			"tool_input": evt.ToolInput,
		}
		if evt.TodoItems != nil {
			data["todo_items"] = evt.TodoItems
		}
		a.hudBroadcast(data)
	}

	baseDir, err := config.FlowDir()
	if err != nil {
		emit(agent.StreamEvent{Type: "error", Content: err.Error()})
		return
	}
	coworkDir := filepath.Join(baseDir, "cowork")

	sessMgr := session.NewManager(coworkDir)
	toolReg := tools.NewRegistry()
	tools.RegisterStandardTools(toolReg, baseDir)

	systemPrompt := a.loadCoworkSystemPrompt()
	if ctxNote != "" {
		systemPrompt += "\n\n" + ctxNote
	}

	deps := agent.Deps{
		SessionMgr:          sessMgr,
		LLMClient:           a.llm,
		ToolRegistry:        toolReg,
		WorkDir:             workDir,
		BaseDir:             baseDir,
		DisableSystemPrompt: a.cfg != nil && a.cfg.DisableSystemPrompt,
	}

	result, err := agent.RunTurnStreamWithContent(ctx, sessionID, systemPrompt, content, deps, emit)
	if err != nil {
		a.hudBroadcast(map[string]interface{}{
			"session_id": sessionID,
			"type":       "error",
			"content":    err.Error(),
		})
		return
	}

	a.hudBroadcast(map[string]interface{}{
		"session_id": sessionID,
		"type":       "done",
		"final_text": result.FinalText,
	})
}

// OpenSessionInWindow raises the main Flow window and asks the frontend to load
// the given cowork session, so a HUD task can be continued in the full UI.
func (a *App) OpenSessionInWindow(sessionID string) {
	if a.ctx == nil {
		return
	}
	wailsRuntime.WindowShow(a.ctx)
	wailsRuntime.EventsEmit(a.ctx, "cowork:open_session", map[string]interface{}{
		"session_id": sessionID,
	})
	speech.HideHUD()
}

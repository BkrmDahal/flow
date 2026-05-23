package backend

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/user/flow/backend/internal/agent"
	"github.com/user/flow/backend/internal/config"
	"github.com/user/flow/backend/internal/session"
	"github.com/user/flow/backend/internal/streaming"
	"github.com/user/flow/backend/internal/tools"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// TaskFileInfo holds metadata about a file in an agent task's working directory.
type TaskFileInfo struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// HistoryMessage is a simplified message format for loading past sessions
// into the frontend.
type HistoryMessage struct {
	Role    string     `json:"role"`
	Content string     `json:"content"`
	Steps   []ChatStep `json:"steps"`
	Files   []ChatFile `json:"files,omitempty"`
}

// ChatStep represents a single intermediate step in an agent turn.
type ChatStep struct {
	Type      string `json:"type"`                 // "tool_call" | "tool_result"
	Content   string `json:"content"`              // tool result content
	ToolName  string `json:"tool_name,omitempty"`  // for tool_call / tool_result
	ToolInput string `json:"tool_input,omitempty"` // for tool_call (JSON string)
}

// ChatFile represents an attachment for frontend history display.
type ChatFile struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// coworkDefaultSystemPrompt is bootstrapped to ~/.flow/cowork_prompt.md on first run.
const coworkDefaultSystemPrompt = `You are Cowork, a helpful coding assistant.
You have tools for planning work, reading and writing files, running shell commands, managing memory, and loading skills.
Keep responses concise. When creating files, use relative paths.`

// coworkSystemPromptSuffix is appended after the user-editable prompt.
const coworkSystemPromptSuffix = `

## Cowork Tooling

You have access to the full standard tool set, including todo_write.

### Planning Rules
- Only create a plan (via todo_write) when the task requires multiple distinct steps. For simple, single-step tasks, skip planning and act directly.
- Each plan item must be a concise description of WHAT to do, not HOW. Write steps as plain-language goals (e.g. "Add error handling to the upload function"), never as code snippets or shell commands to run.
- Update the plan as you progress, marking items in_progress or completed.`

// --- Wails-bound Agent Methods ---

// NewAgentSession starts a fresh agent task session and returns the new session ID.
func (a *App) NewAgentSession() string {
	prefix := "agent_main"
	if a.cfg != nil {
		if ac, ok := a.cfg.Agents["main"]; ok && ac.SessionPrefix != "" {
			prefix = ac.SessionPrefix
		}
	}
	return fmt.Sprintf("%s_%d", prefix, time.Now().UnixMilli())
}

// SendAgentTaskStream starts a streaming agent turn for an agent task.
// Returns immediately; events are emitted on "agent:stream:event".
func (a *App) SendAgentTaskStream(input string, sessionID string) error {
	if a.llm == nil {
		return fmt.Errorf("LLM not configured — please add an API key in Settings")
	}
	content, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("marshal input: %w", err)
	}

	sid := sessionID
	if sid == "" {
		sid = fmt.Sprintf("agent_%d", time.Now().UnixMilli())
	}

	workDir := filepath.Join(a.baseDir, "agents", sid)
	go a.runAgentStream(sid, content, workDir)
	return nil
}

// SendAgentTaskStreamWithFiles starts a streaming agent turn with file attachments.
// Supports image, PDF (native), and text-extractable documents.
func (a *App) SendAgentTaskStreamWithFiles(input string, files []streaming.FileAttachment, sessionID string) error {
	if a.llm == nil {
		return fmt.Errorf("LLM not configured — please add an API key in Settings")
	}

	sid := sessionID
	if sid == "" {
		sid = fmt.Sprintf("agent_%d", time.Now().UnixMilli())
	}

	workDir := filepath.Join(a.baseDir, "agents", sid)
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return fmt.Errorf("create workdir: %w", err)
	}

	raw, err := streaming.BuildContent(input, files, streaming.ContentOptions{
		WorkDir: workDir,
	})
	if err != nil {
		return fmt.Errorf("marshal content: %w", err)
	}

	go a.runAgentStream(sid, raw, workDir)
	return nil
}

// SendCoworkTaskStream starts a streaming agent turn for a Cowork-style task.
// The Cowork system prompt is injected. Events are emitted on "cowork:stream:event".
func (a *App) SendCoworkTaskStream(input string, sessionID string) error {
	if a.llm == nil {
		return fmt.Errorf("no model configured — open Settings first")
	}
	if sessionID == "" {
		return fmt.Errorf("session ID is required")
	}

	content, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("marshal input: %w", err)
	}

	baseDir, err := config.FlowDir()
	if err != nil {
		return fmt.Errorf("resolve flow dir: %w", err)
	}
	workDir := filepath.Join(baseDir, "cowork", sessionID)

	go a.runCoworkStream(sessionID, content, workDir)
	return nil
}

// SendCoworkTaskStreamWithFiles starts a streaming cowork turn with file attachments.
func (a *App) SendCoworkTaskStreamWithFiles(input string, files []streaming.FileAttachment, extractText bool, sessionID string) error {
	if a.llm == nil {
		return fmt.Errorf("no model configured — open Settings first")
	}
	if sessionID == "" {
		return fmt.Errorf("session ID is required")
	}

	baseDir, err := config.FlowDir()
	if err != nil {
		return fmt.Errorf("resolve flow dir: %w", err)
	}
	workDir := filepath.Join(baseDir, "cowork", sessionID)
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return fmt.Errorf("create workdir: %w", err)
	}

	raw, err := streaming.BuildContent(input, files, streaming.ContentOptions{
		ExtractText: extractText,
		WorkDir:     workDir,
	})
	if err != nil {
		return fmt.Errorf("marshal content: %w", err)
	}

	go a.runCoworkStream(sessionID, raw, workDir)
	return nil
}

// NewCoworkSession creates a fresh cowork session ID and returns it.
func (a *App) NewCoworkSession() string {
	return fmt.Sprintf("cowork_%d", time.Now().UnixMilli())
}

// --- Stream runners ---

// runAgentStream runs the agent turn in a goroutine and emits events on "agent:stream:event".
func (a *App) runAgentStream(sessionID string, userContent json.RawMessage, workDir string) {
	ctx, cleanup := a.streams.Start(a.ctx, sessionID)
	defer cleanup()

	if a.sessionMgr == nil {
		log.Println("[agent] session manager not initialized")
		a.emitAgentEvent(sessionID, map[string]interface{}{
			"type":       "error",
			"session_id": sessionID,
			"error":      "session manager not initialized",
			"seq":        a.seq.Next(),
		})
		return
	}

	emit := func(evt agent.StreamEvent) {
		payload := map[string]interface{}{
			"type":       evt.Type,
			"session_id": sessionID,
			"seq":        a.seq.Next(),
		}
		if evt.Content != "" {
			payload["content"] = evt.Content
		}
		if evt.ToolName != "" {
			payload["tool_name"] = evt.ToolName
		}
		if evt.ToolInput != "" {
			payload["tool_input"] = evt.ToolInput
		}
		if evt.Path != "" {
			payload["path"] = evt.Path
		}
		if evt.Name != "" {
			payload["name"] = evt.Name
		}
		if evt.TodoItems != nil {
			payload["todo_items"] = evt.TodoItems
		}
		a.emitAgentEvent(sessionID, payload)
	}

	toolReg := tools.NewRegistry()
	tools.RegisterStandardTools(toolReg, a.baseDir)

	deps := agent.Deps{
		SessionMgr:   a.sessionMgr,
		LLMClient:    a.llm,
		ToolRegistry: toolReg,
		WorkDir:      workDir,
		BaseDir:      a.baseDir,
	}

	result, err := agent.RunTurnStreamWithContent(ctx, sessionID, "", userContent, deps, emit)
	if err != nil {
		if ctx.Err() == nil {
			a.emitAgentEvent(sessionID, map[string]interface{}{
				"type":       "error",
				"session_id": sessionID,
				"error":      err.Error(),
				"seq":        a.seq.Next(),
			})
		}
		return
	}

	stepsRaw, _ := json.Marshal(result.Steps)
	a.emitAgentEvent(sessionID, map[string]interface{}{
		"type":       "done",
		"session_id": sessionID,
		"final_text": result.FinalText,
		"steps":      json.RawMessage(stepsRaw),
		"seq":        a.seq.Next(),
	})
}

// runCoworkStream executes a streaming cowork turn and emits events on "cowork:stream:event".
func (a *App) runCoworkStream(sessionID string, content json.RawMessage, workDir string) {
	ctx, cleanup := a.streams.Start(a.ctx, sessionID)
	defer cleanup()

	emit := func(evt agent.StreamEvent) {
		seq := a.seq.Next()
		data := map[string]interface{}{
			"session_id": sessionID,
			"seq":        seq,
			"type":       evt.Type,
			"content":    evt.Content,
			"tool_name":  evt.ToolName,
			"tool_input": evt.ToolInput,
		}
		if evt.TodoItems != nil {
			data["todo_items"] = evt.TodoItems
		}
		if evt.Type == "error" {
			data["error"] = evt.Content
		}
		if evt.Type == "file_created" {
			data["path"] = evt.Content
			data["name"] = evt.ToolName
		}
		wailsRuntime.EventsEmit(a.ctx, "cowork:stream:event", data)
	}

	sessionDir, err := config.FlowDir()
	if err != nil {
		log.Printf("[cowork] flow dir: %v", err)
		return
	}
	coworkDir := filepath.Join(sessionDir, "cowork")

	sessMgr := session.NewManager(coworkDir)
	toolReg := tools.NewRegistry()
	tools.RegisterStandardTools(toolReg, sessionDir)

	systemPrompt := a.loadCoworkSystemPrompt() + coworkSystemPromptSuffix

	deps := agent.Deps{
		SessionMgr:   sessMgr,
		LLMClient:    a.llm,
		ToolRegistry: toolReg,
		WorkDir:      workDir,
		BaseDir:      sessionDir,
	}

	result, err := agent.RunTurnStreamWithContent(ctx, sessionID, systemPrompt, content, deps, emit)
	if err != nil {
		seq := a.seq.Next()
		wailsRuntime.EventsEmit(a.ctx, "cowork:stream:event", map[string]interface{}{
			"session_id": sessionID,
			"seq":        seq,
			"type":       "error",
			"error":      err.Error(),
		})
		return
	}

	seq := a.seq.Next()
	stepsData := make([]map[string]interface{}, 0, len(result.Steps))
	for _, s := range result.Steps {
		stepsData = append(stepsData, map[string]interface{}{
			"type":       s.Type,
			"content":    s.Content,
			"tool_name":  s.ToolName,
			"tool_input": s.ToolInput,
		})
	}
	wailsRuntime.EventsEmit(a.ctx, "cowork:stream:event", map[string]interface{}{
		"session_id": sessionID,
		"seq":        seq,
		"type":       "done",
		"final_text": result.FinalText,
		"steps":      stepsData,
	})
}

func (a *App) emitAgentEvent(sessionID string, payload map[string]interface{}) {
	wailsRuntime.EventsEmit(a.ctx, "agent:stream:event", payload)
}

// --- Cancel ---

// CancelStream cancels a streaming agent turn for the given session.
func (a *App) CancelStream(sessionID string) error {
	a.streams.Cancel(sessionID)
	return nil
}

// CancelCoworkStream cancels the stream for the given cowork session ID.
func (a *App) CancelCoworkStream(sessionID string) {
	a.streams.Cancel(sessionID)
}

// --- Session CRUD ---

// ListAgentSessions returns metadata for all agent task sessions, sorted newest first.
func (a *App) ListAgentSessions() ([]session.SessionInfo, error) {
	if a.sessionMgr == nil {
		return []session.SessionInfo{}, nil
	}
	return a.sessionMgr.ListAgentSessions()
}

// LoadAgentSession returns the raw session messages for a given session ID.
func (a *App) LoadAgentSession(sessionID string) ([]session.Message, error) {
	if a.sessionMgr == nil {
		return []session.Message{}, nil
	}
	return a.sessionMgr.Load(sessionID)
}

// DeleteAgentSession deletes an agent session. If the session is currently
// streaming, it is cancelled first.
func (a *App) DeleteAgentSession(sessionID string) error {
	a.streams.Cancel(sessionID)
	if a.sessionMgr == nil {
		return nil
	}
	return a.sessionMgr.DeleteSession(sessionID)
}

// ListCoworkSessions returns metadata for all cowork sessions, newest first.
func (a *App) ListCoworkSessions() ([]session.SessionInfo, error) {
	dir, err := config.FlowDir()
	if err != nil {
		return []session.SessionInfo{}, nil
	}
	mgr := session.NewManager(filepath.Join(dir, "cowork"))
	return mgr.ListSessionsByPrefix("cowork")
}

// LoadCoworkSession loads a previous cowork session's messages for frontend display.
func (a *App) LoadCoworkSession(sessionID string) ([]HistoryMessage, error) {
	dir, err := config.FlowDir()
	if err != nil {
		return nil, fmt.Errorf("resolve flow dir: %w", err)
	}
	mgr := session.NewManager(filepath.Join(dir, "cowork"))
	msgs, err := mgr.Load(sessionID)
	if err != nil {
		return nil, fmt.Errorf("load session: %w", err)
	}

	var result []HistoryMessage
	for _, msg := range msgs {
		parsed := parseCoworkMessageForFrontend(msg)
		if parsed == nil {
			continue
		}
		// Merge consecutive assistant messages.
		if parsed.Role == "assistant" && len(result) > 0 && result[len(result)-1].Role == "assistant" {
			prev := &result[len(result)-1]
			prev.Steps = append(prev.Steps, parsed.Steps...)
			if parsed.Content != "" {
				if prev.Content != "" {
					prev.Content += parsed.Content
				} else {
					prev.Content = parsed.Content
				}
			}
		} else {
			result = append(result, *parsed)
		}
	}
	return result, nil
}

// DeleteCoworkSession removes a saved cowork session.
func (a *App) DeleteCoworkSession(sessionID string) error {
	dir, err := config.FlowDir()
	if err != nil {
		return fmt.Errorf("resolve flow dir: %w", err)
	}
	mgr := session.NewManager(filepath.Join(dir, "cowork"))
	return mgr.DeleteSession(sessionID)
}

// --- Work directory & files ---

// GetTaskWorkDir returns the working directory path for an agent task.
func (a *App) GetTaskWorkDir(taskID string) (string, error) {
	dir := filepath.Join(a.baseDir, "agents", taskID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create task dir: %w", err)
	}
	return dir, nil
}

// GetCoworkWorkDir returns the working directory path for a cowork session.
func (a *App) GetCoworkWorkDir(sessionID string) (string, error) {
	dir, err := config.FlowDir()
	if err != nil {
		return "", fmt.Errorf("resolve flow dir: %w", err)
	}
	workDir := filepath.Join(dir, "cowork", sessionID)
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return "", fmt.Errorf("create workdir: %w", err)
	}
	return workDir, nil
}

// ListTaskFiles returns a list of files in an agent task's working directory.
func (a *App) ListTaskFiles(taskID string) ([]TaskFileInfo, error) {
	dir := filepath.Join(a.baseDir, "agents", taskID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []TaskFileInfo{}, nil
		}
		return nil, fmt.Errorf("read task dir: %w", err)
	}

	var files []TaskFileInfo
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if e.Name() == "session.json" || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, TaskFileInfo{
			Name: e.Name(),
			Path: filepath.Join(dir, e.Name()),
			Size: info.Size(),
		})
	}

	return files, nil
}

// ListCoworkFiles returns files in a cowork session's working directory.
func (a *App) ListCoworkFiles(sessionID string) ([]TaskFileInfo, error) {
	dir, err := config.FlowDir()
	if err != nil {
		return []TaskFileInfo{}, nil
	}
	workDir := filepath.Join(dir, "cowork", sessionID)
	entries, err := os.ReadDir(workDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []TaskFileInfo{}, nil
		}
		return nil, fmt.Errorf("read workdir: %w", err)
	}

	var files []TaskFileInfo
	for _, e := range entries {
		if e.IsDir() || strings.HasSuffix(e.Name(), ".jsonl") || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, TaskFileInfo{
			Name: e.Name(),
			Path: filepath.Join(workDir, e.Name()),
			Size: info.Size(),
		})
	}
	return files, nil
}

// --- File operations ---

// OpenFileInApp opens a file or directory in the default macOS application.
func (a *App) OpenFileInApp(filePath string) error {
	if strings.Contains(filePath, "://") {
		return fmt.Errorf("opening URLs is not allowed")
	}
	if strings.HasPrefix(filePath, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			filePath = filepath.Join(home, filePath[2:])
		}
	}
	// Verify the path exists and is not an .app bundle.
	info, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("path does not exist: %w", err)
	}
	if info.IsDir() && strings.HasSuffix(filePath, ".app") {
		return fmt.Errorf("opening .app bundles is not allowed")
	}
	return exec.Command("open", filePath).Run()
}

// RevealInFinder reveals a file in Finder, highlighting it in its parent folder.
func (a *App) RevealInFinder(filePath string) error {
	if strings.Contains(filePath, "://") {
		return fmt.Errorf("opening URLs is not allowed")
	}
	if strings.HasPrefix(filePath, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			filePath = filepath.Join(home, filePath[2:])
		}
	}
	return exec.Command("open", "-R", filePath).Run()
}

// --- Helpers ---

// loadCoworkSystemPrompt reads the user-editable system prompt from disk,
// or bootstraps it with a sensible default on first run.
func (a *App) loadCoworkSystemPrompt() string {
	dir, err := config.FlowDir()
	if err != nil {
		return coworkDefaultSystemPrompt
	}
	path := filepath.Join(dir, "cowork_prompt.md")
	data, err := os.ReadFile(path)
	if err != nil {
		// Bootstrap default.
		_ = os.WriteFile(path, []byte(coworkDefaultSystemPrompt), 0o644)
		return coworkDefaultSystemPrompt
	}
	if s := strings.TrimSpace(string(data)); s != "" {
		return s
	}
	return coworkDefaultSystemPrompt
}

// parseCoworkMessageForFrontend converts a stored session message to a
// HistoryMessage for the frontend. Returns nil for tool_result user messages.
func parseCoworkMessageForFrontend(msg session.Message) *HistoryMessage {
	if msg.Role == "user" {
		// Skip tool_result messages.
		var blocks []map[string]interface{}
		if json.Unmarshal(msg.Content, &blocks) == nil && len(blocks) > 0 {
			if typ, _ := blocks[0]["type"].(string); typ == "tool_result" {
				return nil
			}
		}
		text, files := parseUserContentForFrontend(msg.Content)
		return &HistoryMessage{
			Role:    "user",
			Content: text,
			Steps:   []ChatStep{},
			Files:   files,
		}
	}

	if msg.Role == "assistant" {
		text := session.ExtractTextFromContent(msg.Content)
		var steps []ChatStep

		var blocks []map[string]interface{}
		if json.Unmarshal(msg.Content, &blocks) == nil {
			for _, block := range blocks {
				typ, _ := block["type"].(string)
				switch typ {
				case "tool_use":
					name, _ := block["name"].(string)
					inputRaw, _ := json.Marshal(block["input"])
					steps = append(steps, ChatStep{
						Type:      "tool_call",
						ToolName:  name,
						ToolInput: string(inputRaw),
					})
				}
			}
		}
		if steps == nil {
			steps = []ChatStep{}
		}

		return &HistoryMessage{
			Role:    "assistant",
			Content: text,
			Steps:   steps,
		}
	}

	return nil
}

func parseUserContentForFrontend(content json.RawMessage) (string, []ChatFile) {
	var s string
	if json.Unmarshal(content, &s) == nil {
		return s, nil
	}

	var blocks []map[string]interface{}
	if json.Unmarshal(content, &blocks) != nil {
		return string(content), nil
	}

	var textParts []string
	var files []ChatFile
	for _, block := range blocks {
		typ, _ := block["type"].(string)
		if typ != "text" {
			continue
		}
		text, _ := block["text"].(string)
		if name, ok := parseAttachedFileName(text); ok {
			files = append(files, ChatFile{Name: name, Type: mimeTypeFromFilename(name)})
			continue
		}
		if strings.TrimSpace(text) != "" {
			textParts = append(textParts, text)
		}
	}

	return strings.TrimSpace(strings.Join(textParts, "\n")), files
}

func parseAttachedFileName(text string) (string, bool) {
	const prefix = "[Attached file "
	if !strings.HasPrefix(text, prefix) {
		return "", false
	}
	rest := strings.TrimPrefix(text, prefix)
	for _, marker := range []string{" content:]", " saved to workspace"} {
		if idx := strings.Index(rest, marker); idx >= 0 {
			name := strings.TrimSpace(rest[:idx])
			return name, name != ""
		}
	}
	return "", false
}

func mimeTypeFromFilename(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".pdf":
		return "application/pdf"
	case ".csv":
		return "text/csv"
	case ".md", ".markdown":
		return "text/markdown"
	case ".html", ".htm":
		return "text/html"
	case ".json":
		return "application/json"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	default:
		return "text/plain"
	}
}

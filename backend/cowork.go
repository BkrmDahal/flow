package backend

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/user/flow/backend/internal/agent"
	"github.com/user/flow/backend/internal/config"
	"github.com/user/flow/backend/internal/parser"
	"github.com/user/flow/backend/internal/session"
	"github.com/user/flow/backend/internal/tools"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// coworkSessionPrefix is baked into session IDs so we can filter them.
const coworkSessionPrefix = "cowork"

// coworkDefaultSystemPrompt is bootstrapped to ~/.flow/cowork_prompt.md on first run.
const coworkDefaultSystemPrompt = `You are Cowork, a helpful coding assistant.
You have tools for planning work, reading and writing files, running shell commands, managing memory, and loading skills.
Use todo_write to create a visible plan before multi-step work, then update that plan as you complete each step.
Keep responses concise. When creating files, use relative paths.`

// coworkSystemPromptSuffix is appended after the user-editable prompt. Older
// installs may still have a prompt that says Cowork only has three tools, so
// keep the current tool/planning contract close to the final system prompt.
const coworkSystemPromptSuffix = `

## Cowork Tooling

You have access to the full standard tool set, including todo_write. For any
task with more than one step, call todo_write before other tools so the side
panel can show the plan, then update it as the work progresses.`

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

// --- Stream management ---

// streamCancels tracks per-session cancellation functions so streams
// can be stopped from the frontend.
var (
	streamMu      sync.Mutex
	streamCancels = map[string]context.CancelFunc{}
)

// --- Cowork methods (Wails-bound) ---

// NewCoworkSession creates a fresh cowork session ID and returns it.
func (a *App) NewCoworkSession() string {
	return fmt.Sprintf("%s_%d", coworkSessionPrefix, time.Now().UnixMilli())
}

// SendCoworkTaskStream starts a streaming agent turn for a Cowork task.
// Returns immediately; events are emitted on "cowork:stream:event".
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

// CoworkFileAttachment is a file sent from the frontend for multimodal cowork tasks.
type CoworkFileAttachment struct {
	Name     string `json:"name"`
	MimeType string `json:"mime_type"`
	Data     string `json:"data"` // base64-encoded content
}

// SendCoworkTaskStreamWithFiles starts a streaming agent turn with file attachments.
func (a *App) SendCoworkTaskStreamWithFiles(input string, files []CoworkFileAttachment, extractText bool, sessionID string) error {
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

	// Build multimodal content blocks.
	type contentBlock struct {
		Type   string      `json:"type"`
		Text   string      `json:"text,omitempty"`
		Source interface{} `json:"source,omitempty"`
	}
	var blocks []contentBlock
	if input != "" {
		blocks = append(blocks, contentBlock{Type: "text", Text: input})
	}

	for _, f := range files {
		if strings.HasPrefix(f.MimeType, "image/") {
			blocks = append(blocks, contentBlock{
				Type: "image",
				Source: map[string]interface{}{
					"type":       "base64",
					"media_type": f.MimeType,
					"data":       f.Data,
				},
			})
		} else {
			// Decode base64 to save and parse
			rawBytes, err := base64.StdEncoding.DecodeString(f.Data)
			if err != nil {
				log.Printf("failed to decode base64 for file %s: %v", f.Name, err)
				continue
			}

			// Save file to workspace
			destPath := filepath.Join(workDir, f.Name)
			if err := os.WriteFile(destPath, rawBytes, 0o644); err != nil {
				log.Printf("failed to save file %s to workspace: %v", f.Name, err)
			}

			var textContent string
			if extractText {
				extracted, err := parser.ExtractText(f.Name, rawBytes)
				if err != nil {
					log.Printf("failed to extract text from %s: %v", f.Name, err)
					textContent = fmt.Sprintf("[Attached file %s saved to workspace at %s. Text extraction failed or unsupported.]", f.Name, destPath)
				} else {
					textContent = fmt.Sprintf("[Attached file %s content:]\n%s", f.Name, extracted)
				}
				blocks = append(blocks, contentBlock{
					Type: "text",
					Text: textContent,
				})
			} else {
				if strings.HasSuffix(strings.ToLower(f.Name), ".pdf") {
					blocks = append(blocks, contentBlock{
						Type: "document",
						Source: map[string]interface{}{
							"type":       "base64",
							"media_type": f.MimeType,
							"data":       f.Data,
						},
					})
				} else {
					textContent = fmt.Sprintf("[Attached file %s saved to workspace at %s. Context not extracted.]", f.Name, destPath)
					blocks = append(blocks, contentBlock{
						Type: "text",
						Text: textContent,
					})
				}
			}
		}
	}

	raw, err := json.Marshal(blocks)
	if err != nil {
		return fmt.Errorf("marshal content: %w", err)
	}

	go a.runCoworkStream(sessionID, raw, workDir)
	return nil
}

// runCoworkStream executes a streaming agent turn and emits events to
// the frontend on the "cowork:stream:event" channel.
func (a *App) runCoworkStream(sessionID string, content json.RawMessage, workDir string) {
	streamCtx, cancel := context.WithCancel(a.ctx)
	streamMu.Lock()
	streamCancels[sessionID] = cancel
	streamMu.Unlock()

	defer func() {
		streamMu.Lock()
		delete(streamCancels, sessionID)
		streamMu.Unlock()
		cancel()
	}()

	// Monotonic sequence counter for dedup on the macOS WebKit bridge.
	var seq int64
	emit := func(evt agent.StreamEvent) {
		seq++
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

	// Build deps for the agent.
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

	result, err := agent.RunTurnStreamWithContent(streamCtx, sessionID, systemPrompt, content, deps, emit)
	if err != nil {
		seq++
		wailsRuntime.EventsEmit(a.ctx, "cowork:stream:event", map[string]interface{}{
			"session_id": sessionID,
			"seq":        seq,
			"type":       "error",
			"error":      err.Error(),
		})
		return
	}

	// Emit the final done event.
	seq++
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

// CancelCoworkStream cancels the stream for the given session ID.
func (a *App) CancelCoworkStream(sessionID string) {
	streamMu.Lock()
	defer streamMu.Unlock()

	if sessionID != "" {
		if cancel, ok := streamCancels[sessionID]; ok {
			cancel()
			delete(streamCancels, sessionID)
		}
		return
	}
	for sid, cancel := range streamCancels {
		cancel()
		delete(streamCancels, sid)
	}
}

// ListCoworkSessions returns metadata for all cowork sessions, newest first.
func (a *App) ListCoworkSessions() ([]session.SessionInfo, error) {
	dir, err := config.FlowDir()
	if err != nil {
		return []session.SessionInfo{}, nil
	}
	mgr := session.NewManager(filepath.Join(dir, "cowork"))
	return mgr.ListSessionsByPrefix(coworkSessionPrefix)
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

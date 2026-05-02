package backend

import (
	"context"
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
	"github.com/user/flow/backend/internal/session"
	"github.com/user/flow/backend/internal/tools"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)


// coworkSessionPrefix is baked into session IDs so we can filter them.
const coworkSessionPrefix = "cowork"

// coworkDefaultSystemPrompt is bootstrapped to ~/.flow/cowork_prompt.md on first run.
const coworkDefaultSystemPrompt = `You are Cowork, a helpful coding assistant.
You have three tools: read_file, write_file, and run_bash.
Use them to accomplish the user's task step by step.
Keep responses concise. When creating files, use relative paths.`

// HistoryMessage is a simplified message format for loading past sessions
// into the frontend.
type HistoryMessage struct {
	Role    string     `json:"role"`
	Content string     `json:"content"`
	Steps   []ChatStep `json:"steps"`
}

// ChatStep represents a single intermediate step in an agent turn.
type ChatStep struct {
	Type      string `json:"type"`                // "tool_call" | "tool_result"
	Content   string `json:"content"`             // tool result content
	ToolName  string `json:"tool_name,omitempty"`  // for tool_call / tool_result
	ToolInput string `json:"tool_input,omitempty"` // for tool_call (JSON string)
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
	tools.RegisterStandard(toolReg)

	systemPrompt := a.loadCoworkSystemPrompt()

	deps := agent.Deps{
		SessionMgr:   sessMgr,
		LLMClient:    a.llm,
		ToolRegistry: toolReg,
		WorkDir:      workDir,
	}

	// Extract user text from the JSON content for use as a plain string.
	var userText string
	json.Unmarshal(content, &userText)

	result, err := agent.RunTurnStream(streamCtx, sessionID, systemPrompt, userText, deps, emit)
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
		text := session.ExtractTextFromContent(msg.Content)
		return &HistoryMessage{
			Role:    "user",
			Content: text,
			Steps:   []ChatStep{},
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

package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/user/flow/backend/internal/agent"
	"github.com/user/flow/backend/internal/session"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// streamCancel tracks cancellable streaming contexts per session.
var (
	agentStreamMu      sync.Mutex
	agentStreamCancels = map[string]context.CancelFunc{}
)

// TaskFileInfo holds metadata about a file in an agent task's working directory.
type TaskFileInfo struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// seqCounter increments to uniquely stamp every stream event so the frontend
// can deduplicate macOS WebKit bridge duplicates.
var agentSeqMu sync.Mutex
var agentSeqCounter int

func nextAgentSeq() int {
	agentSeqMu.Lock()
	defer agentSeqMu.Unlock()
	agentSeqCounter++
	return agentSeqCounter
}

// SendAgentTaskStream starts a streaming agent turn for an agent task.
// Events are emitted on "agent:stream:event" to avoid mixing with flow streams.
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

// FileAttachment is a file sent from the frontend for multimodal agent tasks.
type AgentFileAttachment struct {
	Name     string `json:"name"`
	MimeType string `json:"mime_type"`
	Data     string `json:"data"` // base64-encoded content
}

// SendAgentTaskStreamWithFiles starts a streaming agent turn with file attachments.
func (a *App) SendAgentTaskStreamWithFiles(input string, files []AgentFileAttachment, sessionID string) error {
	if a.llm == nil {
		return fmt.Errorf("LLM not configured — please add an API key in Settings")
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
			blocks = append(blocks, contentBlock{
				Type: "text",
				Text: fmt.Sprintf("[Attached file: %s]\n%s", f.Name, f.Data),
			})
		}
	}

	raw, err := json.Marshal(blocks)
	if err != nil {
		return fmt.Errorf("marshal content: %w", err)
	}

	sid := sessionID
	if sid == "" {
		sid = fmt.Sprintf("agent_%d", time.Now().UnixMilli())
	}

	workDir := filepath.Join(a.baseDir, "agents", sid)
	go a.runAgentStream(sid, raw, workDir)
	return nil
}

// runAgentStream runs the agent turn in a goroutine and emits events.
func (a *App) runAgentStream(sessionID string, userContent json.RawMessage, workDir string) {
	ctx, cancel := context.WithCancel(context.Background())
	agentStreamMu.Lock()
	if prev, ok := agentStreamCancels[sessionID]; ok {
		prev() // cancel any existing stream for this session
	}
	agentStreamCancels[sessionID] = cancel
	agentStreamMu.Unlock()

	defer func() {
		agentStreamMu.Lock()
		delete(agentStreamCancels, sessionID)
		agentStreamMu.Unlock()
		cancel()
	}()

	// Ensure session manager exists.
	if a.sessionMgr == nil {
		log.Println("[agent] session manager not initialized")
		a.emitAgentEvent(sessionID, map[string]interface{}{
			"type":       "error",
			"session_id": sessionID,
			"error":      "session manager not initialized",
			"seq":        nextAgentSeq(),
		})
		return
	}

	emit := func(evt agent.StreamEvent) {
		payload := map[string]interface{}{
			"type":       evt.Type,
			"session_id": sessionID,
			"seq":        nextAgentSeq(),
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

	deps := agent.Deps{
		SessionMgr:   a.sessionMgr,
		LLMClient:    a.llm,
		ToolRegistry: a.tools,
		WorkDir:      workDir,
		BaseDir:      a.baseDir,
	}

	result, err := agent.RunTurnStreamWithContent(ctx, sessionID, "", userContent, deps, emit)
	if err != nil {
		if ctx.Err() == nil {
			// Not cancelled — emit error event.
			a.emitAgentEvent(sessionID, map[string]interface{}{
				"type":       "error",
				"session_id": sessionID,
				"error":      err.Error(),
				"seq":        nextAgentSeq(),
			})
		}
		return
	}

	// Emit done event.
	stepsRaw, _ := json.Marshal(result.Steps)
	a.emitAgentEvent(sessionID, map[string]interface{}{
		"type":       "done",
		"session_id": sessionID,
		"final_text": result.FinalText,
		"steps":      json.RawMessage(stepsRaw),
		"seq":        nextAgentSeq(),
	})
}

func (a *App) emitAgentEvent(sessionID string, payload map[string]interface{}) {
	wailsRuntime.EventsEmit(a.ctx, "agent:stream:event", payload)
}

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
	// Cancel any active stream for this session.
	agentStreamMu.Lock()
	if cancel, ok := agentStreamCancels[sessionID]; ok {
		cancel()
		delete(agentStreamCancels, sessionID)
	}
	agentStreamMu.Unlock()

	if a.sessionMgr == nil {
		return nil
	}
	return a.sessionMgr.DeleteSession(sessionID)
}


// CancelStream cancels a streaming agent turn for the given session.
func (a *App) CancelStream(sessionID string) error {
	agentStreamMu.Lock()
	defer agentStreamMu.Unlock()
	if cancel, ok := agentStreamCancels[sessionID]; ok {
		cancel()
		delete(agentStreamCancels, sessionID)
	}
	return nil
}

// OpenFileInApp opens a file or directory in the default macOS application.
func (a *App) OpenFileInApp(filePath string) error {
	if strings.HasPrefix(filePath, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			filePath = filepath.Join(home, filePath[2:])
		}
	}
	return exec.Command("open", filePath).Run()
}

// RevealInFinder reveals a file in Finder, highlighting it in its parent folder.
func (a *App) RevealInFinder(filePath string) error {
	if strings.HasPrefix(filePath, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			filePath = filepath.Join(home, filePath[2:])
		}
	}
	return exec.Command("open", "-R", filePath).Run()
}

// GetTaskWorkDir returns the working directory path for an agent task.
func (a *App) GetTaskWorkDir(taskID string) (string, error) {
	dir := filepath.Join(a.baseDir, "agents", taskID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create task dir: %w", err)
	}
	return dir, nil
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

package session

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Message represents a single conversation message in Anthropic format.
type Message struct {
	Role    string          `json:"role"`    // "user", "assistant"
	Content json.RawMessage `json:"content"` // string or []ContentBlock
}

// Manager handles JSONL-based session persistence with per-session locking.
type Manager struct {
	baseDir string
	locks   sync.Map // map[string]*sync.Mutex
}

// NewManager creates a session manager that stores JSONL files directly
// under baseDir. Pass the full path to the sessions directory (e.g.
// "~/.flow/cowork"), not its parent.
func NewManager(baseDir string) *Manager {
	return &Manager{baseDir: baseDir}
}

// sessionsDir returns the path to the sessions directory.
func (m *Manager) sessionsDir() string {
	return m.baseDir
}

// filePath returns the JSONL file path for a given session ID.
func (m *Manager) filePath(sessionID string) string {
	return filepath.Join(m.sessionsDir(), sessionID+".jsonl")
}

// getMutex returns the mutex for a given session ID, creating one if needed.
func (m *Manager) getMutex(sessionID string) *sync.Mutex {
	mu, _ := m.locks.LoadOrStore(sessionID, &sync.Mutex{})
	return mu.(*sync.Mutex)
}

// Lock acquires the session mutex. Callers must call Unlock when done.
func (m *Manager) Lock(sessionID string) {
	m.getMutex(sessionID).Lock()
}

// Unlock releases the session mutex.
func (m *Manager) Unlock(sessionID string) {
	m.getMutex(sessionID).Unlock()
}

// Load reads all messages from a session's JSONL file.
// Returns an empty slice (not an error) if the file does not exist.
func (m *Manager) Load(sessionID string) ([]Message, error) {
	path := m.filePath(sessionID)
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return []Message{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open session %s: %w", sessionID, err)
	}
	defer f.Close()

	var msgs []Message
	scanner := bufio.NewScanner(f)
	// Increase buffer for large messages
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var msg Message
		if err := json.Unmarshal(line, &msg); err != nil {
			return nil, fmt.Errorf("unmarshal message in %s: %w", sessionID, err)
		}
		msgs = append(msgs, msg)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan session %s: %w", sessionID, err)
	}
	return msgs, nil
}

// Append adds one or more messages to the session's JSONL file (append-only).
func (m *Manager) Append(sessionID string, msgs ...Message) error {
	path := m.filePath(sessionID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir for session: %w", err)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open session %s for append: %w", sessionID, err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	for _, msg := range msgs {
		if err := enc.Encode(msg); err != nil {
			return fmt.Errorf("encode message in %s: %w", sessionID, err)
		}
	}
	return nil
}

// Overwrite replaces the session file with the given messages.
func (m *Manager) Overwrite(sessionID string, msgs []Message) error {
	path := m.filePath(sessionID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir for session: %w", err)
	}

	f, err := os.OpenFile(path, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open session %s for overwrite: %w", sessionID, err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	for _, msg := range msgs {
		if err := enc.Encode(msg); err != nil {
			return fmt.Errorf("encode message in %s: %w", sessionID, err)
		}
	}
	return nil
}

// EstimateTokens gives a rough token estimate for a list of messages.
// Uses the approximation: tokens ~ len(json_bytes) / 4.
func EstimateTokens(msgs []Message) int {
	total := 0
	for _, msg := range msgs {
		data, _ := json.Marshal(msg)
		total += len(data)
	}
	return total / 4
}

// Compactor defines the interface needed for summarization during compaction.
type Compactor interface {
	Summarize(ctx context.Context, system string, messages []Message) (string, error)
}

// CompactIfNeeded checks if the session exceeds the token threshold (100k)
// and, if so, summarizes the first half of the conversation.
func (m *Manager) CompactIfNeeded(ctx context.Context, sessionID string, compactor Compactor) error {
	msgs, err := m.Load(sessionID)
	if err != nil {
		return err
	}

	tokens := EstimateTokens(msgs)
	if tokens <= 100_000 {
		return nil
	}

	mid := len(msgs) / 2
	firstHalf := msgs[:mid]
	secondHalf := msgs[mid:]

	summary, err := compactor.Summarize(ctx, "Summarize the following conversation concisely, preserving all key facts and decisions.", firstHalf)
	if err != nil {
		return fmt.Errorf("compaction summarize: %w", err)
	}

	summaryContent, _ := json.Marshal(summary)
	summaryMsg := Message{
		Role:    "assistant",
		Content: summaryContent,
	}

	compacted := append([]Message{summaryMsg}, secondHalf...)
	return m.Overwrite(sessionID, compacted)
}

// SessionInfo contains metadata about a saved session for the sidebar.
type SessionInfo struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Timestamp int64  `json:"timestamp"`
}

// ListGUISessions scans the sessions directory for GUI sessions and returns
// metadata sorted by timestamp (newest first).
func (m *Manager) ListGUISessions() ([]SessionInfo, error) {
	dir := m.sessionsDir()
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []SessionInfo{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read sessions dir: %w", err)
	}

	var sessions []SessionInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		sessionID := strings.TrimSuffix(e.Name(), ".jsonl")
		if !strings.Contains(sessionID, "_gui_") {
			continue
		}

		// Extract timestamp from session ID (last underscore-separated part).
		parts := strings.Split(sessionID, "_")
		var ts int64
		if len(parts) > 0 {
			fmt.Sscanf(parts[len(parts)-1], "%d", &ts)
		}

		// Extract title from first user message.
		title := extractTitle(filepath.Join(dir, e.Name()))

		sessions = append(sessions, SessionInfo{
			ID:        sessionID,
			Title:     title,
			Timestamp: ts,
		})
	}

	// Sort by timestamp descending (newest first).
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].Timestamp > sessions[j].Timestamp
	})

	return sessions, nil
}

// ListTelegramSessions scans the sessions directory for Telegram sessions
// (those with "tg_" prefix) and returns metadata sorted by timestamp (newest first).
func (m *Manager) ListTelegramSessions() ([]SessionInfo, error) {
	dir := m.sessionsDir()
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []SessionInfo{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read sessions dir: %w", err)
	}

	var sessions []SessionInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		sessionID := strings.TrimSuffix(e.Name(), ".jsonl")
		if !strings.HasPrefix(sessionID, "tg_") {
			continue
		}

		// Use file modification time as timestamp for Telegram sessions.
		info, err := e.Info()
		var ts int64
		if err == nil {
			ts = info.ModTime().UnixMilli()
		}

		// Extract title from first user message.
		title := extractTitle(filepath.Join(dir, e.Name()))

		// Use the Telegram user ID as a subtitle hint.
		userID := strings.TrimPrefix(sessionID, "tg_")
		if title == "Untitled" {
			title = "Telegram user " + userID
		}

		sessions = append(sessions, SessionInfo{
			ID:        sessionID,
			Title:     title,
			Timestamp: ts,
		})
	}

	// Sort by timestamp descending (newest first).
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].Timestamp > sessions[j].Timestamp
	})

	return sessions, nil
}


// DeleteSession removes a session's JSONL file.
func (m *Manager) DeleteSession(sessionID string) error {
	path := m.filePath(sessionID)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete session %s: %w", sessionID, err)
	}
	return nil
}

// extractTitle reads the first user message from a JSONL file and returns
// a truncated string suitable for use as a sidebar title.
func extractTitle(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return "Untitled"
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var msg Message
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			continue
		}
		if msg.Role != "user" {
			continue
		}
		text := ExtractTextFromContent(msg.Content)
		if text == "" {
			continue
		}
		// Truncate for display.
		if len(text) > 60 {
			text = text[:60] + "..."
		}
		return text
	}

	return "Untitled"
}

// ExtractTextFromContent extracts plain text from a message content field.
// Content can be a JSON string or an array of content blocks.
func ExtractTextFromContent(content json.RawMessage) string {
	if len(content) == 0 {
		return ""
	}
	// Try as string first.
	var s string
	if err := json.Unmarshal(content, &s); err == nil {
		return s
	}
	// Try as array of content blocks.
	var blocks []map[string]interface{}
	if err := json.Unmarshal(content, &blocks); err == nil {
		var parts []string
		for _, block := range blocks {
			typ, _ := block["type"].(string)
			if typ == "text" {
				if text, ok := block["text"].(string); ok {
					if strings.HasPrefix(text, "[Attached file ") {
						continue
					}
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

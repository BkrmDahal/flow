package session

import (
	"log"
	"os"
	"strings"
	"time"
)

// ResolveSessionID computes the session ID based on the scoping policy.
//
//   - "main"             → prefix (single shared session)
//   - "per-peer"         → prefix:peer:<peerID>
//   - "per-channel-peer" → prefix:ch:<channelID>:peer:<peerID>
//
// Falls back to "main" for unknown scope values.
func ResolveSessionID(scope, prefix, peerID, channelID string) string {
	switch scope {
	case "per-peer":
		if peerID == "" {
			return prefix
		}
		return prefix + ":peer:" + peerID
	case "per-channel-peer":
		if peerID == "" && channelID == "" {
			return prefix
		}
		if channelID == "" {
			return prefix + ":peer:" + peerID
		}
		return prefix + ":ch:" + channelID + ":peer:" + peerID
	default:
		// "main" or any unknown value → single shared session.
		return prefix
	}
}

// ResetStaleSessions deletes session files under the sessions directory that
// match the given prefix and haven't been modified within maxAge.
func (m *Manager) ResetStaleSessions(prefix string, maxAge time.Duration) (int, error) {
	dir := m.sessionsDir()
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}

	cutoff := time.Now().Add(-maxAge)
	removed := 0

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		sessionID := strings.TrimSuffix(e.Name(), ".jsonl")
		if !strings.HasPrefix(sessionID, prefix) {
			continue
		}

		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			path := m.filePath(sessionID)
			if err := os.Remove(path); err != nil {
				log.Printf("[session] failed to remove stale session %s: %v", sessionID, err)
				continue
			}
			removed++
			log.Printf("[session] removed stale session %s (last modified %s)", sessionID, info.ModTime().Format(time.RFC3339))
		}
	}

	return removed, nil
}

// ListSessionsByPrefix returns all session IDs that start with the given prefix.
func (m *Manager) ListSessionsByPrefix(prefix string) ([]SessionInfo, error) {
	dir := m.sessionsDir()
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []SessionInfo{}, nil
	}
	if err != nil {
		return nil, err
	}

	var sessions []SessionInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		sessionID := strings.TrimSuffix(e.Name(), ".jsonl")
		if prefix != "" && !strings.HasPrefix(sessionID, prefix) {
			continue
		}

		info, err := e.Info()
		var ts int64
		if err == nil {
			ts = info.ModTime().UnixMilli()
		}

		sessions = append(sessions, SessionInfo{
			ID:        sessionID,
			Title:     sessionID,
			Timestamp: ts,
		})
	}

	return sessions, nil
}

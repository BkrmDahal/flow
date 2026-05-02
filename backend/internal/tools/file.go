package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const maxReadBytes = 10 * 1024 // 10KB

// ── Session directory context ──
//
// File tools resolve paths relative to the active session workspace
// directory (~/.flow/cowork/{session_id}/). The session dir is
// propagated via context so per-request scoping works even when
// the tool registry is shared across sessions.

type sessionDirKey struct{}

// WithSessionDir returns a context that carries the session workspace directory.
func WithSessionDir(ctx context.Context, dir string) context.Context {
	return context.WithValue(ctx, sessionDirKey{}, dir)
}

// SessionDirFromContext extracts the session workspace directory from ctx.
// Returns an empty string if none is set.
func SessionDirFromContext(ctx context.Context) string {
	if dir, ok := ctx.Value(sessionDirKey{}).(string); ok {
		return dir
	}
	return ""
}

// resolveSessionPath resolves a user-supplied file path within a session
// workspace directory. Relative paths are joined under sessionDir. Absolute
// paths are re-rooted under sessionDir to prevent writes outside the sandbox.
// Directory traversal (../) is cleaned and contained.
func resolveSessionPath(sessionDir, path string) string {
	if sessionDir == "" {
		return path // no sandbox — fallback to raw path
	}

	// Clean the input path.
	cleaned := filepath.Clean(path)

	// Strip leading slash so absolute paths are re-rooted under sessionDir.
	if filepath.IsAbs(cleaned) {
		cleaned = strings.TrimPrefix(cleaned, string(filepath.Separator))
	}

	resolved := filepath.Join(sessionDir, cleaned)

	// Safety check: ensure the resolved path hasn't escaped the session dir
	// via ../ traversal.
	absSession := filepath.Clean(sessionDir) + string(filepath.Separator)
	absResolved := filepath.Clean(resolved)
	if !strings.HasPrefix(absResolved+string(filepath.Separator), absSession) && absResolved != filepath.Clean(sessionDir) {
		// Traversal detected — force into session root with just the basename.
		resolved = filepath.Join(sessionDir, filepath.Base(cleaned))
	}

	return resolved
}

// --- read_file ---

// ReadFileTool reads a local text file.
type ReadFileTool struct{}

func (t *ReadFileTool) Name() string { return "read_file" }

func (t *ReadFileTool) Description() string {
	return "Read the contents of a file. Relative paths are resolved within the session workspace (~/.flow/cowork/{session_id}/). Absolute paths outside the session workspace are also allowed. Output is truncated to 10KB to save tokens."
}

func (t *ReadFileTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path to the file to read (relative paths resolve within the session workspace)",
			},
		},
		"required": []string{"path"},
	}
}

type filePathInput struct {
	Path string `json:"path"`
}

func (t *ReadFileTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var in filePathInput
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("parse input: %w", err)
	}

	readPath := in.Path

	// For relative paths, try the session workspace first.
	if sessionDir := SessionDirFromContext(ctx); sessionDir != "" && !filepath.IsAbs(in.Path) {
		candidate := resolveSessionPath(sessionDir, in.Path)
		if _, err := os.Stat(candidate); err == nil {
			readPath = candidate
		}
	}

	data, err := os.ReadFile(readPath)
	if err != nil {
		return fmt.Sprintf("Error reading file: %v", err), nil
	}

	content := string(data)
	if len(content) > maxReadBytes {
		content = content[:maxReadBytes] + "\n... [file truncated at 10KB]"
	}
	return content, nil
}

// --- write_file ---

// WriteFileTool writes content to a file within the session workspace,
// creating directories as needed.
type WriteFileTool struct{}

func (t *WriteFileTool) Name() string { return "write_file" }

func (t *WriteFileTool) Description() string {
	return "Write text content to a file. Files are saved in the session workspace (~/.flow/cowork/{session_id}/). Parent directories are created automatically. Use relative paths like 'README.md' or 'src/main.py'."
}

func (t *WriteFileTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Relative path for the file within the session workspace (e.g. 'README.md', 'src/main.py')",
			},
			"content": map[string]interface{}{
				"type":        "string",
				"description": "The text content to write to the file",
			},
		},
		"required": []string{"path", "content"},
	}
}

type writeFileInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (t *WriteFileTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var in writeFileInput
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("parse input: %w", err)
	}

	// Resolve the path within the session workspace.
	writePath := in.Path
	if sessionDir := SessionDirFromContext(ctx); sessionDir != "" {
		writePath = resolveSessionPath(sessionDir, in.Path)
	}

	dir := filepath.Dir(writePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Sprintf("Error creating directories: %v", err), nil
	}

	if err := os.WriteFile(writePath, []byte(in.Content), 0o644); err != nil {
		return fmt.Sprintf("Error writing file: %v", err), nil
	}

	return fmt.Sprintf("Successfully wrote %d bytes to %s", len(in.Content), writePath), nil
}

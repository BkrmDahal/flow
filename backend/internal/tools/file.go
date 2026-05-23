package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/user/flow/backend/internal/parser"
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
	return "Read the contents of a file at the given path. Supports plain text files as well as formatted documents (.pdf, .xlsx, .docx, .pptx). Relative paths resolve within the session workspace (~/.flow/cowork/{session_id}/). Absolute paths to external files are also allowed (except sensitive dirs like ~/.ssh). Output is truncated to 10KB. TIP: Always read a file before attempting to overwrite it."
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

	// Block access to sensitive directories and files to prevent
	// prompt-injection exfiltration of credentials and secrets.
	if err := checkSensitivePath(readPath); err != nil {
		return fmt.Sprintf("Error: access denied — %v", err), nil
	}

	data, err := os.ReadFile(readPath)
	if err != nil {
		if os.IsNotExist(err) {
			if sessionDir := SessionDirFromContext(ctx); sessionDir != "" {
				entries, errDir := os.ReadDir(sessionDir)
				if errDir == nil && len(entries) > 0 {
					var fileNames []string
					for _, entry := range entries {
						if !entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") && !strings.HasSuffix(entry.Name(), ".jsonl") {
							fileNames = append(fileNames, entry.Name())
						}
					}
					if len(fileNames) > 0 {
						return fmt.Sprintf("Error: file %q not found. The files currently in your workspace are: %s. Please correct the path or name.", in.Path, strings.Join(fileNames, ", ")), nil
					}
				}
			}
		}
		return fmt.Sprintf("Error reading file: %v", err), nil
	}

	ext := strings.ToLower(readPath)
	isRichDoc := strings.HasSuffix(ext, ".pdf") ||
		strings.HasSuffix(ext, ".xlsx") ||
		strings.HasSuffix(ext, ".docx") ||
		strings.HasSuffix(ext, ".pptx")

	var content string
	if isRichDoc {
		extracted, err := parser.ExtractText(filepath.Base(readPath), data)
		if err != nil {
			return fmt.Sprintf("Error extracting text from document: %v", err), nil
		}
		content = extracted
	} else {
		content = string(data)
	}

	if len(content) > maxReadBytes {
		content = content[:maxReadBytes] + "\n... [file truncated at 10KB]"
	}
	return content, nil
}

// sensitivePathPrefixes are directories that read_file must never access,
// even when the LLM requests them with an absolute path.
var sensitivePathPrefixes = []string{
	".ssh",
	".aws",
	".kube",
	".gnupg",
	".gpg",
	".config/gcloud",
}

// sensitiveExactFiles are specific files that must never be readable.
var sensitiveExactFiles = []string{
	"config.json", // ~/.flow/config.json contains API keys
}

// checkSensitivePath returns an error if the given path falls inside a
// blocked sensitive directory or matches a blocked file.
func checkSensitivePath(path string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil // can't resolve home, allow (defensive)
	}

	absPath := path
	if !filepath.IsAbs(absPath) {
		absPath, err = filepath.Abs(absPath)
		if err != nil {
			return nil
		}
	}
	absPath = filepath.Clean(absPath)

	for _, prefix := range sensitivePathPrefixes {
		sensitive := filepath.Join(homeDir, prefix)
		if absPath == sensitive || strings.HasPrefix(absPath, sensitive+string(filepath.Separator)) {
			return fmt.Errorf("reading from %s is not allowed for security reasons", prefix)
		}
	}

	// Block the Flow config file itself (contains API keys).
	flowConfigPath := filepath.Join(homeDir, ".flow", "config.json")
	if absPath == flowConfigPath {
		return fmt.Errorf("reading config.json is not allowed (contains API keys)")
	}

	return nil
}

// --- write_file ---

// WriteFileTool writes content to a file within the session workspace,
// creating directories as needed.
type WriteFileTool struct{}

func (t *WriteFileTool) Name() string { return "write_file" }

func (t *WriteFileTool) Description() string {
	return "Write text content to a file. Files are saved in the session workspace (~/.flow/cowork/{session_id}/). Parent directories are created automatically. Use relative paths like 'README.md' or 'src/main.py'. TIP: For edits, read the file first, modify the content, then write the full result back. Overwrites existing content entirely."
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

// --- list_dir ---

// ListDirTool lists all files and directories within the session workspace.
type ListDirTool struct{}

func (t *ListDirTool) Name() string { return "list_dir" }

func (t *ListDirTool) Description() string {
	return "List all files and subdirectories in the session workspace (~/.flow/cowork/{session_id}/). Use this tool to explore the workspace files instead of running bash 'ls' commands."
}

func (t *ListDirTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

type listDirItem struct {
	Name  string `json:"name"`
	IsDir bool   `json:"is_dir"`
	Size  int64  `json:"size"`
}

func (t *ListDirTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	sessionDir := SessionDirFromContext(ctx)
	if sessionDir == "" {
		return "Error: no active workspace session directory configured.", nil
	}

	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		return fmt.Sprintf("Error reading workspace directory: %v", err), nil
	}

	var items []listDirItem
	for _, entry := range entries {
		// Ignore hidden files and session log files (.jsonl)
		if strings.HasPrefix(entry.Name(), ".") || strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		info, err := entry.Info()
		var size int64
		if err == nil {
			size = info.Size()
		}
		items = append(items, listDirItem{
			Name:  entry.Name(),
			IsDir: entry.IsDir(),
			Size:  size,
		})
	}

	if len(items) == 0 {
		return "The workspace directory is currently empty.", nil
	}

	raw, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return fmt.Sprintf("Error encoding result: %v", err), nil
	}

	return string(raw), nil
}

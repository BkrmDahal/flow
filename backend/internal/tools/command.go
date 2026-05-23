package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/user/flow/backend/internal/config"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	commandTimeout = 60 * time.Second
	maxOutputBytes = 10 * 1024 // 10KB
)

// PendingSandboxApprovals maps approval IDs to blocked channels waiting for user decision
var (
	PendingSandboxApprovals = make(map[string]chan bool)
	SandboxApprovalMu       sync.Mutex
)

// hardBlocked is a small hardcoded blocklist of obviously destructive
// patterns. Any command containing one of these substrings is refused.
// We do not maintain an allowlist in v1 — the agent can run anything else.
var hardBlocked = []string{
	"rm -rf /",
	"rm -rf ~",
	"mkfs",
	"dd if=",
	":(){ :|:& };:",
	"shutdown",
	"reboot",
	"halt",
}

// RunBashTool executes a shell command via `sh -c`.
// Output is combined stdout+stderr, truncated to 10KB; the call is
// cancelled after 60 seconds.
type RunBashTool struct {
	baseDir string
}

func NewRunBashTool(baseDir string) *RunBashTool { return &RunBashTool{baseDir: baseDir} }

func (t *RunBashTool) Name() string { return "run_bash" }

func (t *RunBashTool) Description() string {
	return "Execute a shell command on the local machine via `sh -c`. Returns combined stdout and stderr, truncated to 10KB. Use this for things like ls, grep, running scripts, git, package managers, etc. The command is killed after 60 seconds."
}

func (t *RunBashTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"command": map[string]interface{}{
				"type":        "string",
				"description": "The shell command to execute",
			},
		},
		"required": []string{"command"},
	}
}

type bashInput struct {
	Command string `json:"command"`
}

func (t *RunBashTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var in bashInput
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("parse input: %w", err)
	}
	if strings.TrimSpace(in.Command) == "" {
		return "Error: command is empty", nil
	}

	for _, b := range hardBlocked {
		if strings.Contains(in.Command, b) {
			return fmt.Sprintf("Error: command refused — contains blocked pattern %q", b), nil
		}
	}

	// Enforce Allowed command list from exec-approvals.json
	if t.baseDir != "" {
		approvals, err := config.LoadExecApprovals(t.baseDir)
		if err == nil && approvals != nil && len(approvals.Allowed) > 0 {
			allowed := false
			trimmedCmd := strings.TrimSpace(in.Command)
			parts := strings.Fields(trimmedCmd)
			if len(parts) > 0 {
				exe := parts[0]
				for _, allowedCmd := range approvals.Allowed {
					if exe == allowedCmd || strings.HasPrefix(trimmedCmd, allowedCmd) {
						allowed = true
						break
					}
				}
			}
			if !allowed {
				return fmt.Sprintf("Error: command %q is not in the allowed commands list. You can allow it in Settings -> General.", in.Command), nil
			}
		}
	}

	execCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		homeDir, _ := os.UserHomeDir()
		var allowedWritePaths []string

		// 1. Base directory (e.g., ~/.flow)
		if t.baseDir != "" {
			if absBase, err := filepath.Abs(t.baseDir); err == nil {
				allowedWritePaths = append(allowedWritePaths, absBase)
			}
		}

		// 2. Session working directory
		sessionDir := SessionDirFromContext(ctx)
		if sessionDir != "" {
			if absSession, err := filepath.Abs(sessionDir); err == nil {
				allowedWritePaths = append(allowedWritePaths, absSession)
			}
		}

		// 3. Current working directory of the application
		if cwd, err := os.Getwd(); err == nil {
			if absCwd, err := filepath.Abs(cwd); err == nil {
				allowedWritePaths = append(allowedWritePaths, absCwd)
			}
		}

		// 4. Standard temporary / system storage paths
		allowedWritePaths = append(allowedWritePaths, "/tmp", "/private/tmp", "/var", "/private/var", os.TempDir())

		// Clean, absolute, and deduplicate default directories
		uniquePaths := make(map[string]bool)
		for _, p := range allowedWritePaths {
			cleaned := filepath.Clean(p)
			if cleaned != "" && cleaned != "/" {
				uniquePaths[cleaned] = true
			}
		}

		// 5. Scan the command for potential external paths and prompt the user dynamically
		potentialPaths := extractPotentialPaths(in.Command)
		for _, rawPath := range potentialPaths {
			dir := rawPath
			if info, err := os.Stat(rawPath); err != nil || !info.IsDir() {
				dir = filepath.Dir(rawPath)
			}

			// Resolve directory to absolute path
			absDir, err := filepath.Abs(dir)
			if err != nil {
				continue
			}
			absDir = filepath.Clean(absDir)

			// Check if this directory is already inside our whitelisted write paths
			isAllowed := false
			for allowed := range uniquePaths {
				if absDir == allowed || strings.HasPrefix(absDir, allowed+string(filepath.Separator)) {
					isAllowed = true
					break
				}
			}

			// If it's not whitelisted, and is a valid external path, prompt the user!
			if !isAllowed && absDir != "/" && absDir != "." && absDir != ".." {
				if AskSandboxApproval(ctx, absDir) {
					uniquePaths[absDir] = true
				}
			}
		}

		var writeRules []string
		for p := range uniquePaths {
			writeRules = append(writeRules, fmt.Sprintf("(subpath %q)", p))
		}

		// Build SBPL Profile
		profile := fmt.Sprintf(`(version 1)
(allow default)
(deny file-write* (subpath "/"))
(allow file-write*
    %s
)
`, strings.Join(writeRules, "\n    "))

		// Block access to highly sensitive paths
		if homeDir != "" {
			sensitivePaths := []string{
				filepath.Join(homeDir, ".ssh"),
				filepath.Join(homeDir, ".aws"),
				filepath.Join(homeDir, ".kube"),
			}
			var readDenyRules []string
			for _, sp := range sensitivePaths {
				readDenyRules = append(readDenyRules, fmt.Sprintf("(subpath %q)", filepath.Clean(sp)))
			}
			profile += fmt.Sprintf("\n(deny file-read* %s)\n", strings.Join(readDenyRules, " "))
		}

		// Wrap with sandbox-exec
		cmd = exec.CommandContext(execCtx, "sandbox-exec", "-p", profile, "sh", "-c", in.Command)
	} else {
		cmd = exec.CommandContext(execCtx, "sh", "-c", in.Command)
	}

	if dir := SessionDirFromContext(ctx); dir != "" {
		cmd.Dir = dir
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	output := out.String()
	if len(output) > maxOutputBytes {
		output = output[:maxOutputBytes] + "\n... [output truncated at 10KB]"
	}

	if err != nil {
		return fmt.Sprintf("Exit error: %v\n\n%s", err, output), nil
	}
	if output == "" {
		return "(command completed with no output)", nil
	}
	return output, nil
}

// extractPotentialPaths scans the command string for absolute or home-relative path substrings
func extractPotentialPaths(cmdStr string) []string {
	var paths []string
	homeDir, _ := os.UserHomeDir()

	words := strings.Fields(cmdStr)
	for _, word := range words {
		cleaned := strings.Trim(word, "'\",;()|&<>")
		
		isHomeRelative := strings.HasPrefix(cleaned, "~/")
		isAbsolute := strings.HasPrefix(cleaned, "/")
		
		if isHomeRelative || isAbsolute {
			var fullPath string
			if isHomeRelative && homeDir != "" {
				fullPath = filepath.Join(homeDir, cleaned[2:])
			} else {
				fullPath = cleaned
			}

			fullPath = filepath.Clean(fullPath)
			paths = append(paths, fullPath)
		}
	}
	return paths
}

// AskSandboxApproval emits an event to the Svelte frontend requesting folder permission,
// and blocks Go thread execution until the user clicks Allow or Deny (with a 60s timeout).
func AskSandboxApproval(ctx context.Context, path string) bool {
	id := fmt.Sprintf("sb_%d", time.Now().UnixNano())
	ch := make(chan bool, 1)

	SandboxApprovalMu.Lock()
	PendingSandboxApprovals[id] = ch
	SandboxApprovalMu.Unlock()

	defer func() {
		SandboxApprovalMu.Lock()
		delete(PendingSandboxApprovals, id)
		SandboxApprovalMu.Unlock()
	}()

	// Emit event to the frontend
	wailsRuntime.EventsEmit(ctx, "sandbox:request_approval", map[string]interface{}{
		"id":   id,
		"path": path,
	})

	// Wait for response or timeout
	select {
	case approved := <-ch:
		return approved
	case <-time.After(60 * time.Second): // 60s timeout
		return false
	case <-ctx.Done():
		return false
	}
}

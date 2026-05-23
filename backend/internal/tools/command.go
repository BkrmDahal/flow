package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

type CommandApprovalResponse struct {
	Choice string // "deny" | "session" | "always"
}

var (
	PendingCommandApprovals = make(map[string]chan CommandApprovalResponse)
	CommandApprovalMu       sync.Mutex

	// SessionAllowedCommands maps session working directory paths to whitelisted command prefixes
	SessionAllowedCommands = make(map[string][]string)
	SessionAllowedMu       sync.Mutex

	// SessionAllowedSandboxPaths maps session working directory paths to whitelisted absolute folder paths
	SessionAllowedSandboxPaths = make(map[string][]string)
	SessionAllowedSandboxMu    sync.Mutex
)

// hardBlocked is a small hardcoded blocklist of obviously destructive
// patterns. Any command containing one of these substrings is refused.
// This is defense-in-depth; the primary protection is the sandbox + approval system.
var hardBlocked = []string{
	"rm -rf /",
	"rm -rf ~",
	"rm -r -f /",
	"rm -r -f ~",
	"rm --recursive --force /",
	"rm --recursive --force ~",
	"rm -rf /*",
	"mkfs",
	"dd if=",
	":(){ :|:& };:",
	"shutdown",
	"reboot",
	"halt",
	"init 0",
	"init 6",
	"systemctl poweroff",
	"systemctl reboot",
	"> /dev/sda",
	"chmod -R 777 /",
	"chown -R",
	"launchctl unload",
	"diskutil eraseDisk",
	"diskutil partitionDisk",
}

// hardBlockedRegexps catches variants of destructive commands that
// substring matching can miss (e.g. flag reordering: rm -fr /).
var hardBlockedRegexps = []*regexp.Regexp{
	// rm with recursive + force flags in any order targeting / or ~
	regexp.MustCompile(`\brm\s+(-[a-zA-Z]*r[a-zA-Z]*f[a-zA-Z]*|-[a-zA-Z]*f[a-zA-Z]*r[a-zA-Z]*)\s+[/~]`),
	// curl/wget piped to sh/bash (common RCE pattern)
	regexp.MustCompile(`(curl|wget)\s+.*\|\s*(sh|bash|zsh)`),
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
	desc := `Execute a shell command on macOS via sh -c. Returns combined stdout+stderr, truncated to 10KB. Killed after 60 seconds. Commands run inside a macOS sandbox with restricted write access (writes only allowed to the session workspace, /tmp, and explicitly approved directories).

TIPS:
- Prefer single-line commands; chain with && for multi-step.
- Use 'cat', 'head -n', or 'tail -n' for reading files inline.
- Always check if a binary exists before using it.
- For Python one-liners, use 'python3 -c "..."'.
- Destructive commands (rm -rf /, sudo, etc.) are blocked.`
	if t.baseDir != "" {
		if cfg, err := config.Load(t.baseDir); err == nil && cfg != nil && cfg.PythonPath != "" && cfg.PythonPath != "python" && cfg.PythonPath != "python3" {
			desc += fmt.Sprintf("\n\nPYTHON ENVIRONMENT: Standard 'python'/'python3' calls are dynamically rewritten to use your configured Python: %q. Use 'python -m pip install' to install packages into it.", cfg.PythonPath)
		}
	}
	return desc
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
	for _, re := range hardBlockedRegexps {
		if re.MatchString(in.Command) {
			return fmt.Sprintf("Error: command refused — matches blocked pattern %q", re.String()), nil
		}
	}

	// Enforce Allowed command list from exec-approvals.json
	if t.baseDir != "" {
		allowed := false
		trimmedCmd := strings.TrimSpace(in.Command)
		parts := strings.Fields(trimmedCmd)
		if len(parts) > 0 {
			exe := parts[0]

			// 1. Dynamic Python Path Whitelisting
			cfg, err := config.Load(t.baseDir)
			if err == nil && cfg != nil && cfg.PythonPath != "" {
				cleanExe := filepath.Clean(exe)
				cleanPy := filepath.Clean(cfg.PythonPath)
				basePy := filepath.Base(cfg.PythonPath)
				if exe == cfg.PythonPath || cleanExe == cleanPy || exe == basePy || exe == "python" || exe == "python3" {
					allowed = true
				}
			}

			// 2. Session Allowed Whitelisting
			sessionDir := SessionDirFromContext(ctx)
			if !allowed && sessionDir != "" {
				SessionAllowedMu.Lock()
				sessionList := SessionAllowedCommands[sessionDir]
				SessionAllowedMu.Unlock()
				for _, allowedCmd := range sessionList {
					if exe == allowedCmd || strings.HasPrefix(trimmedCmd, allowedCmd) {
						allowed = true
						break
					}
				}
			}

			// 3. Static approvals.Allowed Whitelisting
			if !allowed {
				approvals, err := config.LoadExecApprovals(t.baseDir)
				if err == nil && approvals != nil && len(approvals.Allowed) > 0 {
					for _, allowedCmd := range approvals.Allowed {
						if exe == allowedCmd || strings.HasPrefix(trimmedCmd, allowedCmd) {
							allowed = true
							break
						}
					}
				}
			}

			// 4. If still blocked, intercept and ask the user for permission dynamically!
			if !allowed {
			// Intercept and ask the user for permission dynamically.
			// Even if no allowlist is configured, we default to prompting
			// the user rather than silently allowing commands (fail-closed).
			approvals, _ := config.LoadExecApprovals(t.baseDir)

			// Check if the executable actually exists in PATH before prompting
			if _, lookErr := exec.LookPath(exe); lookErr == nil {
				decision := AskCommandApproval(ctx, in.Command, exe)
				if decision.Choice == "session" {
					if sessionDir != "" {
						SessionAllowedMu.Lock()
						SessionAllowedCommands[sessionDir] = append(SessionAllowedCommands[sessionDir], exe)
						SessionAllowedMu.Unlock()
					}
					allowed = true
				} else if decision.Choice == "always" {
					if approvals == nil {
						approvals = &config.ExecApprovals{}
					}
					approvals.Allowed = append(approvals.Allowed, exe)
					_ = config.SaveExecApprovals(t.baseDir, approvals)
					allowed = true
				}
			} else {
				// Allow it to pass through to let standard shell throw "command not found" error
				// rather than prompting the user for an executable that does not exist.
				allowed = true
			}
		}
		}

		if !allowed {
			return fmt.Sprintf("Error: command %q is not in the allowed commands list. Command execution rejected by user.", in.Command), nil
		}
	}

	execCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	actualCommand := in.Command
	if t.baseDir != "" {
		if cfg, err := config.Load(t.baseDir); err == nil && cfg != nil && cfg.PythonPath != "" {
			actualCommand = rewritePythonCommand(in.Command, cfg.PythonPath)
		}
	}

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
			// 2.5. Session whitelisted sandbox paths
			SessionAllowedSandboxMu.Lock()
			sessionSandboxList := SessionAllowedSandboxPaths[sessionDir]
			SessionAllowedSandboxMu.Unlock()
			for _, allowedSandboxPath := range sessionSandboxList {
				if absAllowedSandbox, err := filepath.Abs(allowedSandboxPath); err == nil {
					allowedWritePaths = append(allowedWritePaths, absAllowedSandbox)
				}
			}
		}

		// 3. Current working directory of the application
		if cwd, err := os.Getwd(); err == nil {
			if absCwd, err := filepath.Abs(cwd); err == nil {
				allowedWritePaths = append(allowedWritePaths, absCwd)
			}
		}

		// 4. Standard temporary / system storage paths and dev devices
		allowedWritePaths = append(allowedWritePaths, "/tmp", "/private/tmp", "/var", "/private/var", os.TempDir())
		allowedWritePaths = append(allowedWritePaths, "/dev/null", "/dev/urandom", "/dev/stdin", "/dev/stdout", "/dev/stderr", "/dev/tty")

		// Clean, absolute, deduplicate, and resolve symlinks for write paths.
		uniquePaths := make(map[string]bool)
		for _, p := range allowedWritePaths {
			cleaned := filepath.Clean(p)
			if cleaned != "" && cleaned != "/" {
				// Resolve symlinks to prevent symlink-based sandbox escapes
				// (e.g. creating a symlink inside allowed dir pointing to /etc).
				if resolved, err := filepath.EvalSymlinks(cleaned); err == nil {
					uniquePaths[resolved] = true
				} else {
					uniquePaths[cleaned] = true
				}
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

			// Resolve symlinks for the path check too.
			if resolved, err := filepath.EvalSymlinks(absDir); err == nil {
				absDir = resolved
			}

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
				if isWritable(absDir) {
					if AskSandboxApproval(ctx, absDir) {
						uniquePaths[absDir] = true
						if sessionDir != "" {
							SessionAllowedSandboxMu.Lock()
							SessionAllowedSandboxPaths[sessionDir] = append(SessionAllowedSandboxPaths[sessionDir], absDir)
							SessionAllowedSandboxMu.Unlock()
						}
					}
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

		// Block writes to critical system paths even if they somehow end up in the allow list.
		profile += "\n(deny file-write* (subpath \"/etc\") (subpath \"/System\") (subpath \"/Library\") (subpath \"/usr\") (subpath \"/bin\") (subpath \"/sbin\"))\n"

		// Block access to highly sensitive paths (read and write).
		if homeDir != "" {
			sensitivePaths := []string{
				filepath.Join(homeDir, ".ssh"),
				filepath.Join(homeDir, ".aws"),
				filepath.Join(homeDir, ".kube"),
				filepath.Join(homeDir, ".gnupg"),
				filepath.Join(homeDir, ".config", "gcloud"),
			}
			var readDenyRules []string
			for _, sp := range sensitivePaths {
				readDenyRules = append(readDenyRules, fmt.Sprintf("(subpath %q)", filepath.Clean(sp)))
			}
			// Also block reading the Flow config file (contains API keys).
			configPath := filepath.Join(homeDir, ".flow", "config.json")
			readDenyRules = append(readDenyRules, fmt.Sprintf("(literal %q)", configPath))
			profile += fmt.Sprintf("\n(deny file-read* %s)\n", strings.Join(readDenyRules, " "))
		}

		// Wrap with sandbox-exec
		cmd = exec.CommandContext(execCtx, "sandbox-exec", "-p", profile, "sh", "-c", actualCommand)
	} else {
		cmd = exec.CommandContext(execCtx, "sh", "-c", actualCommand)
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

// rewritePythonCommand dynamically swaps standard "python" or "python3" calls with the user's custom path
func rewritePythonCommand(cmdStr string, pythonPath string) string {
	if pythonPath == "" || pythonPath == "python" || pythonPath == "python3" {
		return cmdStr
	}

	// Match "python3" or "python" only when they act as command names (preceded by spaces, operators, or start of string)
	rePy3 := regexp.MustCompile(`(^|[|&;(\s])python3\b`)
	rePy := regexp.MustCompile(`(^|[|&;(\s])python\b`)

	cmdStr = rePy3.ReplaceAllString(cmdStr, `${1}`+pythonPath)
	cmdStr = rePy.ReplaceAllString(cmdStr, `${1}`+pythonPath)

	return cmdStr
}

// AskCommandApproval emits an event to the Svelte frontend requesting command execution permission,
// and blocks Go thread execution until the user clicks Allow Always, Allow Session, or Block.
func AskCommandApproval(ctx context.Context, cmdStr string, exe string) CommandApprovalResponse {
	id := fmt.Sprintf("cmd_app_%d", time.Now().UnixNano())
	ch := make(chan CommandApprovalResponse, 1)

	CommandApprovalMu.Lock()
	PendingCommandApprovals[id] = ch
	CommandApprovalMu.Unlock()

	defer func() {
		CommandApprovalMu.Lock()
		delete(PendingCommandApprovals, id)
		CommandApprovalMu.Unlock()
	}()

	// Emit event to the frontend
	wailsRuntime.EventsEmit(ctx, "command:request_approval", map[string]interface{}{
		"id":      id,
		"command": cmdStr,
		"exe":     exe,
	})

	select {
	case resp := <-ch:
		return resp
	case <-time.After(90 * time.Second): // 90s timeout
		return CommandApprovalResponse{Choice: "deny"}
	case <-ctx.Done():
		return CommandApprovalResponse{Choice: "deny"}
	}
}

// isWritable checks if the current process has write permissions to a path (or its closest existing parent directory)
func isWritable(path string) bool {
	current := path
	for {
		if current == "" || current == "." || current == "/" {
			break
		}
		info, err := os.Stat(current)
		if err == nil {
			if !info.IsDir() {
				current = filepath.Dir(current)
				continue
			}
			// Directory exists, check if we can write by attempting to create a temp file
			tempFile, err := os.CreateTemp(current, ".flow_write_test_")
			if err != nil {
				return false
			}
			tempFile.Close()
			os.Remove(tempFile.Name())
			return true
		}
		// Go up one level
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}

	// Default fallback using TempDir
	tempFile, err := os.CreateTemp("", ".flow_write_test_")
	if err == nil {
		tempFile.Close()
		os.Remove(tempFile.Name())
		return true
	}
	return false
}

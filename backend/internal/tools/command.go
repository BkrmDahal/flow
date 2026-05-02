package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	commandTimeout = 60 * time.Second
	maxOutputBytes = 10 * 1024 // 10KB
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
type RunBashTool struct{}

func NewRunBashTool() *RunBashTool { return &RunBashTool{} }

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

	execCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, "sh", "-c", in.Command)
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

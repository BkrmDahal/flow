package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

// ReadClipboardTool reads the contents of the macOS system clipboard.
type ReadClipboardTool struct{}

func (t *ReadClipboardTool) Name() string { return "read_clipboard" }

func (t *ReadClipboardTool) Description() string {
	return "Read the current text contents of the macOS system clipboard. Use this when the user asks you to process, rewrite, or look at something they have copied."
}

func (t *ReadClipboardTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

func (t *ReadClipboardTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	cmd := exec.CommandContext(ctx, "pbpaste")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Sprintf("Error reading clipboard: %v", err), nil
	}
	return string(out), nil
}

// WriteClipboardTool writes text content to the macOS system clipboard.
type WriteClipboardTool struct{}

func (t *WriteClipboardTool) Name() string { return "write_clipboard" }

func (t *WriteClipboardTool) Description() string {
	return "Copy text content to the macOS system clipboard so the user can easily paste it. Use this when generating code, emails, or drafts that the user requested to be copied."
}

func (t *WriteClipboardTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"text": map[string]interface{}{
				"type":        "string",
				"description": "The text content to copy to the clipboard",
			},
		},
		"required": []string{"text"},
	}
}

type writeClipboardInput struct {
	Text string `json:"text"`
}

func (t *WriteClipboardTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var in writeClipboardInput
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("parse input: %w", err)
	}

	cmd := exec.CommandContext(ctx, "pbcopy")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Sprintf("Error accessing clipboard stdin: %v", err), nil
	}

	if err := cmd.Start(); err != nil {
		return fmt.Sprintf("Error starting pbcopy: %v", err), nil
	}

	if _, err := stdin.Write([]byte(in.Text)); err != nil {
		stdin.Close()
		return fmt.Sprintf("Error writing to clipboard: %v", err), nil
	}
	stdin.Close()

	if err := cmd.Wait(); err != nil {
		return fmt.Sprintf("Error finalizing clipboard copy: %v", err), nil
	}

	return fmt.Sprintf("Successfully copied %d characters to the system clipboard.", len(in.Text)), nil
}

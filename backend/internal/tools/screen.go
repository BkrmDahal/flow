package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"time"
)

// CaptureScreenTool captures a screenshot of the macOS screen and saves it in the session workspace.
type CaptureScreenTool struct{}

func (t *CaptureScreenTool) Name() string { return "capture_screen" }

func (t *CaptureScreenTool) Description() string {
	return "Capture a screenshot of the macOS screen and save it to the session workspace. This allows the model to see your current screen layout, active app windows, or output displays when resolving visual tasks."
}

func (t *CaptureScreenTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

func (t *CaptureScreenTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	sessionDir := SessionDirFromContext(ctx)
	if sessionDir == "" {
		return "Error: no session workspace directory active for this tool execution.", nil
	}

	filename := fmt.Sprintf("screenshot_%d.png", time.Now().Unix())
	destPath := filepath.Join(sessionDir, filename)

	if err := CaptureScreenshot(ctx, destPath); err != nil {
		return fmt.Sprintf("Error capturing screen: %v (make sure Flow has Screen Recording permissions in System Settings)", err), nil
	}

	return fmt.Sprintf("Screenshot successfully captured and saved to session workspace as %s.\nAbsolute Path: %s", filename, destPath), nil
}

// CaptureScreenshot quietly captures the full macOS screen to destPath as PNG.
// Shared by the capture_screen tool and the Quick Agent HUD's on-screen context.
func CaptureScreenshot(ctx context.Context, destPath string) error {
	// -x captures silently (no shutter sound).
	return exec.CommandContext(ctx, "screencapture", "-x", destPath).Run()
}

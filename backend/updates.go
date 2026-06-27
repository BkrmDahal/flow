package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const githubRepo = "BkrmDahal/flow"
const githubAPIURL = "https://api.github.com/repos/" + githubRepo + "/releases/latest"

// UpdateInfo describes the result of an update check.
type UpdateInfo struct {
	Available      bool   `json:"available"`
	CurrentVersion string `json:"currentVersion"`
	LatestVersion  string `json:"latestVersion"`
	DownloadURL    string `json:"downloadUrl"`
	ReleaseNotes   string `json:"releaseNotes"`
	ReleaseURL     string `json:"releaseUrl"`
}

// CheckForUpdates queries the GitHub API for the latest release and compares
// it against AppVersion. Returns UpdateInfo with Available=true if a newer
// version exists.
func (a *App) CheckForUpdates() (*UpdateInfo, error) {
	info := &UpdateInfo{CurrentVersion: AppVersion}

	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubAPIURL, nil)
	if err != nil {
		return info, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "flow-updater")
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		log.Printf("[updates] check failed: %v", err)
		return info, fmt.Errorf("check updates: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return info, fmt.Errorf("github api status %d", resp.StatusCode)
	}

	var release struct {
		TagName string `json:"tag_name"`
		Name    string `json:"name"`
		HTMLURL string `json:"html_url"`
		Body    string `json:"body"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return info, fmt.Errorf("decode release: %w", err)
	}

	latestVer := strings.TrimPrefix(release.TagName, "v")
	info.LatestVersion = latestVer
	info.ReleaseNotes = release.Body
	info.ReleaseURL = release.HTMLURL

	for _, asset := range release.Assets {
		if strings.HasSuffix(asset.Name, ".dmg") {
			info.DownloadURL = asset.BrowserDownloadURL
			break
		}
	}

	info.Available = compareVersions(latestVer, AppVersion) > 0
	log.Printf("[updates] current=%s latest=%s available=%v", AppVersion, latestVer, info.Available)
	return info, nil
}

// DownloadAndInstallUpdate downloads the DMG, mounts it, copies the new .app
// bundle over the existing /Applications/Flow.app, unmounts, cleans up, and
// relaunches the app. Progress is streamed to the frontend via the
// "flow:update:progress" event.
func (a *App) DownloadAndInstallUpdate(downloadURL string) error {
	if downloadURL == "" {
		return fmt.Errorf("no download URL provided")
	}

	emitProgress := func(downloaded, total int64, msg string) {
		if a.ctx != nil {
			wailsRuntime.EventsEmit(a.ctx, "flow:update:progress", map[string]interface{}{
				"downloaded": downloaded,
				"total":      total,
				"message":    msg,
			})
		}
	}

	// 1. Download the DMG to a temp file.
	tmpDir, err := os.MkdirTemp("", "flow-update")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	dmgPath := filepath.Join(tmpDir, "Flow-Installer.dmg")

	log.Printf("[updates] downloading DMG from %s", downloadURL)
	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return fmt.Errorf("create download request: %w", err)
	}
	req.Header.Set("User-Agent", "flow-updater")

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return fmt.Errorf("download DMG: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download status %d", resp.StatusCode)
	}

	total := resp.ContentLength
	out, err := os.Create(dmgPath)
	if err != nil {
		return fmt.Errorf("create dmg file: %w", err)
	}

	written := int64(0)
	buf := make([]byte, 32*1024)
	lastEmit := time.Now()
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				out.Close()
				return fmt.Errorf("write dmg: %w", werr)
			}
			written += int64(n)
			if time.Since(lastEmit) > 300*time.Millisecond {
				emitProgress(written, total, "Downloading…")
				lastEmit = time.Now()
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			out.Close()
			return fmt.Errorf("read dmg stream: %w", readErr)
		}
	}
	out.Close()
	emitProgress(written, total, "Download complete")

	// 2. Mount the DMG (no-browser, read-only).
	emitProgress(0, 0, "Mounting installer…")
	mountCmd := exec.Command("hdiutil", "attach", "-nobrowse", "-readonly", dmgPath)
	mountOutput, err := mountCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("mount dmg: %w\n%s", err, string(mountOutput))
	}

	// Parse mount output to find the volume path (last non-empty line).
	lines := strings.Split(strings.TrimSpace(string(mountOutput)), "\n")
	volumePath := strings.TrimSpace(lines[len(lines)-1])
	if volumePath == "" {
		return fmt.Errorf("could not parse mount volume path")
	}
	log.Printf("[updates] mounted at %s", volumePath)

	// 3. Find the .app inside the mounted volume.
	var appPath string
	entries, err := os.ReadDir(volumePath)
	if err != nil {
		_ = detachDMG(volumePath)
		return fmt.Errorf("read mounted volume: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() && strings.HasSuffix(e.Name(), ".app") {
			appPath = filepath.Join(volumePath, e.Name())
			break
		}
	}
	if appPath == "" {
		_ = detachDMG(volumePath)
		return fmt.Errorf("no .app found in DMG")
	}

	// 4. Determine install location.
	installPath := "/Applications/Flow.app"
	if _, err := os.Stat(installPath); os.IsNotExist(err) {
		// Fallback: use the path of the currently running executable.
		if exe, err := os.Executable(); err == nil {
			installPath = filepath.Dir(exe) // the .app/Contents/MacOS dir
			// Walk up to the .app bundle root.
			for p := installPath; p != "/" && p != "."; p = filepath.Dir(p) {
				if strings.HasSuffix(p, ".app") {
					installPath = p
					break
				}
			}
		}
	}

	// 5. Write a relauncher script. It waits for this process to exit, then
	//    copies the new app bundle and relaunches.
	relaunchScript := filepath.Join(tmpDir, "relaunch.sh")
	script := fmt.Sprintf(`#!/bin/bash
set -e
# Wait for the current Flow process to exit.
while pgrep -x Flow > /dev/null 2>&1; do sleep 0.5; done
sleep 1
# Replace the app bundle using ditto (preserves macOS metadata + signatures).
ditto "%s" "%s"
# Detach the DMG.
hdiutil detach "%s" -force 2>/dev/null || true
# Clean up.
rm -rf "%s"
# Relaunch.
open "%s"
`, appPath, installPath, volumePath, tmpDir, installPath)

	if err := os.WriteFile(relaunchScript, []byte(script), 0o755); err != nil {
		_ = detachDMG(volumePath)
		return fmt.Errorf("write relaunch script: %w", err)
	}

	emitProgress(0, 0, "Installing…")

	// 6. Launch the relauncher in the background and quit the app.
	log.Printf("[updates] launching relaunch script: %s", relaunchScript)
	if err := exec.Command("bash", relaunchScript).Start(); err != nil {
		_ = detachDMG(volumePath)
		return fmt.Errorf("start relaunch script: %w", err)
	}

	// 7. Quit the app so the script can replace the bundle.
	emitProgress(0, 0, "Restarting…")
	time.Sleep(500 * time.Millisecond)
	wailsRuntime.Quit(a.ctx)
	return nil
}

// detachDMG unmounts a mounted DMG volume.
func detachDMG(volumePath string) error {
	return exec.Command("hdiutil", "detach", volumePath, "-force").Run()
}

// compareVersions returns >0 if a is newer than b, 0 if equal, <0 if older.
// Both are expected in "0.8.2" format (no 'v' prefix).
func compareVersions(a, b string) int {
	pa := parseVersion(a)
	pb := parseVersion(b)
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var na, nb int
		if i < len(pa) {
			na = pa[i]
		}
		if i < len(pb) {
			nb = pb[i]
		}
		if na != nb {
			return na - nb
		}
	}
	return 0
}

func parseVersion(v string) []int {
	v = strings.TrimPrefix(v, "v")
	parts := strings.Split(v, ".")
	result := make([]int, len(parts))
	for i, p := range parts {
		n := 0
		for _, c := range p {
			if c >= '0' && c <= '9' {
				n = n*10 + int(c-'0')
			} else {
				break
			}
		}
		result[i] = n
	}
	return result
}

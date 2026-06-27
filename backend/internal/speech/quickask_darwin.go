//go:build darwin

package speech

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#include <stdlib.h>

void FlowSetQuickAskModifier(int keyCode);
void FlowStartHotkeyMonitor(void);
void FlowWarmUpAudioSystem(void);
char* FlowCopySelectedTextAX(void);
char* FlowFrontmostAppName(void);
*/
import "C"

import (
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
	"unsafe"
)

// CopySelectedText returns the currently selected text via the Accessibility
// API, or "" if nothing is selected / permission is missing.
func CopySelectedText() string {
	c := C.FlowCopySelectedTextAX()
	if c == nil {
		return ""
	}
	defer C.free(unsafe.Pointer(c))
	return C.GoString(c)
}

// FrontmostAppName returns the localized name of the frontmost application.
func FrontmostAppName() string {
	c := C.FlowFrontmostAppName()
	if c == nil {
		return ""
	}
	defer C.free(unsafe.Pointer(c))
	return C.GoString(c)
}

// ─── Quick Agent HUD push-to-talk ───
//
// A second push-to-talk hotkey, independent of dictation. Instead of pasting
// the transcript into the focused app, it hands the transcript to a registered
// handler (the backend), which runs the Cowork agent and renders the result in
// the floating Quick Agent HUD.

type quickAskState string

const (
	quickAskIdle         quickAskState = "idle"
	quickAskRecording    quickAskState = "recording"
	quickAskTranscribing quickAskState = "transcribing"
)

// QuickAskHandler receives the final transcript once recording + transcription
// complete. It runs on a background goroutine.
type QuickAskHandler func(transcript string)

var (
	qaMu        sync.Mutex
	qaEnabled   bool
	qaState     = quickAskIdle
	qaTempPath  string
	qaCfgLoader func() (TranscribeConfig, error)
	qaHandler   QuickAskHandler
	qaOnTap     func()
	qaOnState   func(string) // "listening" | "transcribing" | "cancelled"
	qaOnError   func(string)
	qaIsPressed bool
	qaListening bool // true once recording actually started (debounce passed)
	qaPressTime time.Time
)

// notifyQuickAskState fires the recording-state callback (used to drive the HUD).
func notifyQuickAskState(state string) {
	qaMu.Lock()
	cb := qaOnState
	qaMu.Unlock()
	if cb != nil {
		cb(state)
	}
}

// IsQuickAskEnabled reports whether the quick-ask hotkey is active.
func IsQuickAskEnabled() bool {
	qaMu.Lock()
	defer qaMu.Unlock()
	return qaEnabled
}

// SetupQuickAsk registers the quick-ask hotkey. It shares the global modifier
// monitor with dictation (starting it if necessary).
func SetupQuickAsk(modifier string, cfgLoader func() (TranscribeConfig, error), handler QuickAskHandler, onTap func(), onState func(string), onError func(string)) {
	if modifier == "" {
		modifier = "left_option"
	}
	modCode := ModifierCodeFromString(modifier)

	qaMu.Lock()
	qaCfgLoader = cfgLoader
	qaHandler = handler
	qaOnTap = onTap
	qaOnState = onState
	qaOnError = onError
	qaEnabled = true
	qaState = quickAskIdle
	qaMu.Unlock()

	C.FlowSetQuickAskModifier(C.int(modCode))
	C.FlowStartHotkeyMonitor() // idempotent — no-op if already running
	C.FlowWarmUpAudioSystem()

	log.Printf("[quickask] enabled — hold %q to ask the agent", modifier)
}

// TeardownQuickAsk disables the quick-ask hotkey. It leaves the shared monitor
// running so dictation continues to work.
func TeardownQuickAsk() {
	C.FlowSetQuickAskModifier(C.int(-1))
	qaMu.Lock()
	qaEnabled = false
	qaState = quickAskIdle
	qaMu.Unlock()
	log.Println("[quickask] disabled")
}

// ─── Hotkey callbacks (invoked from ObjC via CGO) ───

//export goQuickAskPressed
func goQuickAskPressed() {
	qaMu.Lock()
	if !qaEnabled || qaState != quickAskIdle {
		qaMu.Unlock()
		return
	}
	qaIsPressed = true
	now := time.Now()
	qaPressTime = now
	qaState = quickAskRecording
	qaMu.Unlock()

	// Debounce: only actually start recording if the key is still held after
	// 200ms, mirroring the dictation hotkey behaviour.
	go func(pTime time.Time) {
		time.Sleep(200 * time.Millisecond)
		qaMu.Lock()
		if qaEnabled && qaIsPressed && qaPressTime.Equal(pTime) && qaState == quickAskRecording {
			qaMu.Unlock()
			startQuickAskRecording()
			return
		}
		if qaState == quickAskRecording {
			qaState = quickAskIdle
		}
		qaMu.Unlock()
	}(now)
}

// ─── Overlay button callbacks (✕ cancel / ✓ confirm) ───
// The recording pill is shown by quick-ask, so its buttons route here. Both are
// guarded by qaState so they're safe even if the hotkey is also released.

//export goOverlayCancel
func goOverlayCancel() {
	qaMu.Lock()
	rec := qaState == quickAskRecording
	if rec {
		qaState = quickAskIdle
	}
	qaMu.Unlock()
	if rec {
		go cancelQuickAskRecording()
	}
}

//export goOverlayConfirm
func goOverlayConfirm() {
	qaMu.Lock()
	rec := qaState == quickAskRecording
	if rec {
		qaState = quickAskTranscribing
	}
	qaMu.Unlock()
	if rec {
		go stopQuickAskAndDispatch()
	}
}

// CancelQuickAskRecording / ConfirmQuickAskRecording let the HUD's ✕ / ✓
// buttons control the in-progress recording over HTTP (twins of the overlay
// button callbacks).
func CancelQuickAskRecording()  { goOverlayCancel() }
func ConfirmQuickAskRecording() { goOverlayConfirm() }

//export goQuickAskReleased
func goQuickAskReleased() {
	qaMu.Lock()
	if !qaEnabled {
		qaMu.Unlock()
		return
	}
	qaIsPressed = false
	dur := time.Since(qaPressTime)
	state := qaState

	// Short press → "tap". Cancel any partial recording. Only open suggestions
	// if recording never actually started (a pure tap) — if the HUD already
	// showed "Listening", just cancel so suggestions don't flash over it.
	if dur < 300*time.Millisecond {
		wasRecording := state == quickAskRecording
		listening := qaListening
		if wasRecording {
			qaState = quickAskIdle
		}
		onTap := qaOnTap
		qaMu.Unlock()
		if wasRecording {
			go cancelQuickAskRecording()
		}
		if onTap != nil && !listening {
			go onTap()
		}
		return
	}

	if state == quickAskRecording {
		qaState = quickAskTranscribing
		qaMu.Unlock()
		go stopQuickAskAndDispatch()
		return
	}
	qaMu.Unlock()
}

// ─── Internal pipeline ───

func startQuickAskRecording() {
	tmp := filepath.Join(os.TempDir(), fmt.Sprintf("flow-quickask-%d.m4a", time.Now().UnixMilli()))
	qaMu.Lock()
	qaTempPath = tmp
	qaMu.Unlock()

	// Show the unified HUD in its "listening" state (no separate native pill).
	qaMu.Lock()
	qaListening = true
	qaMu.Unlock()
	notifyQuickAskState("listening")
	log.Printf("[quickask] recording → %s", tmp)

	StartRecording(tmp, func(errMsg string) {
		log.Printf("[quickask] recording error: %s", errMsg)
		qaMu.Lock()
		qaState = quickAskIdle
		qaListening = false
		onErr := qaOnError
		qaMu.Unlock()
		notifyQuickAskState("cancelled")
		if onErr != nil {
			onErr(errMsg)
		}
	})
}

func cancelQuickAskRecording() {
	StopRecording()
	qaMu.Lock()
	path := qaTempPath
	qaTempPath = ""
	qaListening = false
	if qaState == quickAskRecording {
		qaState = quickAskIdle
	}
	qaMu.Unlock()
	notifyQuickAskState("cancelled")
	if path != "" {
		os.Remove(path)
	}
	log.Println("[quickask] short press — cancelled")
}

func stopQuickAskAndDispatch() {
	qaMu.Lock()
	path := qaTempPath
	qaTempPath = ""
	cfgLoader := qaCfgLoader
	handler := qaHandler
	onError := qaOnError
	qaMu.Unlock()

	defer func() {
		qaMu.Lock()
		qaState = quickAskIdle
		qaListening = false
		qaMu.Unlock()
	}()

	// HUD transitions from "Listening" to "Transcribing" in the same window.
	notifyQuickAskState("transcribing")

	fail := func(msg string) {
		log.Printf("[quickask] %s", msg)
		if onError != nil {
			onError(msg)
		}
	}

	StopRecording()
	time.Sleep(150 * time.Millisecond)

	if path == "" {
		fail("No recording in progress")
		return
	}
	defer os.Remove(path)

	data, err := os.ReadFile(path)
	if err != nil {
		fail(fmt.Sprintf("Failed to read recording: %v", err))
		return
	}
	if len(data) == 0 {
		fail("Recording was empty — try speaking louder or closer to the microphone")
		return
	}

	cfg, err := cfgLoader()
	if err != nil {
		fail(fmt.Sprintf("Speech config error: %v", err))
		return
	}

	audioBase64 := base64.StdEncoding.EncodeToString(data)
	result, err := Transcribe(cfg, audioBase64, "audio/m4a")
	if err != nil {
		fail(fmt.Sprintf("Transcription failed: %v", err))
		return
	}
	if result.Text == "" {
		fail("No speech detected — try speaking louder")
		return
	}

	log.Printf("[quickask] transcribed %d chars → dispatching to agent", len(result.Text))
	if handler != nil {
		handler(result.Text)
	}
}

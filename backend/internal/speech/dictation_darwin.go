//go:build darwin

package speech

/*
#include <stdlib.h>

// Defined in menubar_darwin.m
void FlowSetMenuBarState(int state);
void FlowSetMenuBarHotkeyLabel(const char *label);
void FlowSetMenuBarGrammarHotkeyLabel(const char *label);
void FlowSetHotkeyModifier(int keyCode);
void FlowStartHotkeyMonitor(void);
void FlowStopHotkeyMonitor(void);
void FlowTypeTextViaClipboard(const char *text);
char* FlowCopySelectedText(void);
void FlowSaveFocusedApp(void);
void FlowRestoreFocusedApp(void);
void FlowPlayDictationSound(int soundType);
void FlowWarmUpAudioSystem(void);

// Defined in overlay_darwin.m
void PreCreateDictationOverlay(void);
void ShowDictationOverlay(int state);
void HideDictationOverlay(void);
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

// ─── Dictation State Machine ───

// DictationState represents the current state of the dictation pipeline.
type DictationState string

const (
	DictationIdle          DictationState = "idle"
	DictationRecording     DictationState = "recording"
	DictationTranscribing  DictationState = "transcribing"
	DictationFixingGrammar DictationState = "fixing"
)

// DictationStatusHandler is called when the dictation state changes.
type DictationStatusHandler func(state DictationState, text string)

// TextTransformer is an optional function that transforms transcribed text
// before it is typed into the focused application.
type TextTransformer func(string) string

// GrammarFixer is called on double-tap to fix grammar of selected text via LLM.
type GrammarFixer func(string) (string, error)

var (
	dictMu               sync.Mutex
	dictState            DictationState = DictationIdle
	dictTempPath         string
	dictCfgLoader        func() (TranscribeConfig, error)
	dictOnStatus         DictationStatusHandler
	dictOnError          func(string)
	dictTextTransform    TextTransformer
	dictGrammarFixer     GrammarFixer
	dictEnabled          bool
	dictPressTime        time.Time
	dictLastShortRelease time.Time
)

// ─── Modifier Key Codes ───

const (
	ModLeftOption  = 0
	ModRightOption = 1
	ModLeftCmd     = 2
	ModRightCmd    = 3
	ModLeftCtrl    = 4
	ModRightCtrl   = 5
)

// ModifierCodeFromString converts a config string to an integer code.
func ModifierCodeFromString(s string) int {
	switch s {
	case "left_option":
		return ModLeftOption
	case "right_option":
		return ModRightOption
	case "left_cmd":
		return ModLeftCmd
	case "right_cmd":
		return ModRightCmd
	case "left_ctrl":
		return ModLeftCtrl
	case "right_ctrl":
		return ModRightCtrl
	default:
		return ModLeftOption
	}
}

// DefaultModifier is the default hotkey modifier string.
const DefaultModifier = "right_option"

// ModifierDisplayName returns a human-readable label for a modifier key string.
func ModifierDisplayName(mod string) string {
	switch mod {
	case "left_option":
		return "⌥ Left Option"
	case "right_option":
		return "⌥ Right Option"
	case "left_cmd":
		return "⌘ Left Command"
	case "right_cmd":
		return "⌘ Right Command"
	case "left_ctrl":
		return "⌃ Left Control"
	case "right_ctrl":
		return "⌃ Right Control"
	default:
		return mod
	}
}

// ─── Public API ───

// SetupDictation configures and starts the push-to-talk dictation pipeline.
func SetupDictation(modifier string, cfgLoader func() (TranscribeConfig, error), onStatus DictationStatusHandler, onError func(string), textTransform TextTransformer, grammarFixer GrammarFixer) {
	if modifier == "" {
		modifier = DefaultModifier
	}
	modCode := ModifierCodeFromString(modifier)

	dictMu.Lock()
	dictCfgLoader = cfgLoader
	dictOnStatus = onStatus
	dictOnError = onError
	dictTextTransform = textTransform
	dictGrammarFixer = grammarFixer
	dictEnabled = true
	dictMu.Unlock()

	C.FlowSetHotkeyModifier(C.int(modCode))
	C.FlowWarmUpAudioSystem()
	C.PreCreateDictationOverlay()
	C.FlowStartHotkeyMonitor()

	UpdateMenuBarHotkeyLabel(modifier)

	log.Printf("[dictation] enabled — hold %q to record, release to transcribe & paste", modifier)
}

// TeardownDictation stops the hotkey monitor.
func TeardownDictation() {
	C.FlowStopHotkeyMonitor()

	cLabel := C.CString("Hotkey: not configured")
	C.FlowSetMenuBarHotkeyLabel(cLabel)
	C.free(unsafe.Pointer(cLabel))

	cGrammarLabel := C.CString("Double-tap to fix grammar")
	C.FlowSetMenuBarGrammarHotkeyLabel(cGrammarLabel)
	C.free(unsafe.Pointer(cGrammarLabel))

	dictMu.Lock()
	dictEnabled = false
	dictState = DictationIdle
	dictMu.Unlock()

	log.Println("[dictation] disabled")
}

// IsDictationEnabled returns whether the dictation hotkey is active.
func IsDictationEnabled() bool {
	dictMu.Lock()
	defer dictMu.Unlock()
	return dictEnabled
}

// GetDictationState returns the current dictation pipeline state.
func GetDictationState() DictationState {
	dictMu.Lock()
	defer dictMu.Unlock()
	return dictState
}

// ─── Hotkey Callbacks (called from ObjC via CGO) ───

//export goDictationPressed
func goDictationPressed() {
	dictMu.Lock()
	if !dictEnabled {
		dictMu.Unlock()
		return
	}
	if dictState != DictationIdle {
		dictMu.Unlock()
		return
	}

	now := time.Now()
	isDoubleTap := !dictLastShortRelease.IsZero() && now.Sub(dictLastShortRelease) < 400*time.Millisecond
	hasGrammarFixer := dictGrammarFixer != nil
	dictPressTime = now
	dictMu.Unlock()

	if isDoubleTap && hasGrammarFixer {
		go fixSelectedTextGrammar()
		return
	}

	go startDictation()
}

//export goDictationReleased
func goDictationReleased() {
	dictMu.Lock()
	if !dictEnabled {
		dictMu.Unlock()
		return
	}
	dur := time.Since(dictPressTime)
	state := dictState
	dictMu.Unlock()

	if dur < 300*time.Millisecond {
		dictMu.Lock()
		dictLastShortRelease = time.Now()
		wasRecording := dictState == DictationRecording
		if wasRecording {
			dictState = DictationIdle
		}
		dictMu.Unlock()
		if wasRecording {
			go cancelDictation()
		}
		return
	}

	if state == DictationRecording {
		go stopDictationAndType()
	}
}

// ─── Internal State Machine ───

func startDictation() {
	dictMu.Lock()
	dictState = DictationRecording
	onStatus := dictOnStatus
	dictMu.Unlock()

	tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("flow-dictate-%d.m4a", time.Now().UnixMilli()))
	dictMu.Lock()
	dictTempPath = tmpFile
	dictMu.Unlock()

	log.Printf("[dictation] recording started → %s", tmpFile)

	StartRecording(tmpFile, func(errMsg string) {
		log.Printf("[dictation] recording error: %s", errMsg)
		C.FlowPlayDictationSound(3)
		C.FlowSetMenuBarState(0)

		dictMu.Lock()
		dictState = DictationIdle
		onErr := dictOnError
		onSt := dictOnStatus
		dictMu.Unlock()

		if onErr != nil {
			onErr(errMsg)
		}
		if onSt != nil {
			onSt(DictationIdle, "")
		}
	})

	C.FlowSetMenuBarState(1)
	C.FlowPlayDictationSound(0)

	if onStatus != nil {
		onStatus(DictationRecording, "")
	}
}

func cancelDictation() {
	StopRecording()
	C.FlowSetMenuBarState(0)

	dictMu.Lock()
	path := dictTempPath
	dictTempPath = ""
	if dictState == DictationRecording {
		dictState = DictationIdle
	}
	dictMu.Unlock()

	if path != "" {
		os.Remove(path)
	}
	log.Println("[dictation] short press — recording cancelled")
}

func stopDictationAndType() {
	dictMu.Lock()
	dictState = DictationTranscribing
	path := dictTempPath
	dictTempPath = ""
	cfgLoader := dictCfgLoader
	onStatus := dictOnStatus
	onError := dictOnError
	dictMu.Unlock()

	C.ShowDictationOverlay(2)
	C.FlowSetMenuBarState(2)

	if onStatus != nil {
		onStatus(DictationTranscribing, "")
	}

	log.Println("[dictation] stopping recording, starting transcription...")

	StopRecording()
	time.Sleep(150 * time.Millisecond)

	fail := func(msg string) {
		log.Printf("[dictation] error: %s", msg)
		C.FlowPlayDictationSound(3)
		C.HideDictationOverlay()
		C.FlowSetMenuBarState(0)
		dictMu.Lock()
		dictState = DictationIdle
		dictMu.Unlock()
		if onError != nil {
			onError(msg)
		}
		if onStatus != nil {
			onStatus(DictationIdle, "")
		}
	}

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

	log.Printf("[dictation] recorded %d bytes, transcribing...", len(data))

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

	finalText := result.Text
	dictMu.Lock()
	transform := dictTextTransform
	dictMu.Unlock()
	if transform != nil {
		finalText = transform(finalText)
	}

	log.Printf("[dictation] transcribed %d chars, pasting into focused app...", len(finalText))

	if !HasAccessibilityPermission(false) {
		log.Println("[dictation] accessibility permission not granted — prompting user")
		HasAccessibilityPermission(true)
		C.FlowPlayDictationSound(3)
		C.HideDictationOverlay()
		C.FlowSetMenuBarState(0)
		dictMu.Lock()
		dictState = DictationIdle
		dictMu.Unlock()
		if onError != nil {
			onError("Grant Accessibility permission in System Settings → Privacy & Security → Accessibility, then try again")
		}
		if onStatus != nil {
			onStatus(DictationIdle, finalText)
		}
		return
	}

	C.HideDictationOverlay()
	cText := C.CString(finalText)
	defer C.free(unsafe.Pointer(cText))
	C.FlowTypeTextViaClipboard(cText)

	C.FlowSetMenuBarState(0)

	log.Println("[dictation] text pasted successfully")

	dictMu.Lock()
	dictState = DictationIdle
	dictMu.Unlock()

	if onStatus != nil {
		onStatus(DictationIdle, finalText)
	}
}

func fixSelectedTextGrammar() {
	dictMu.Lock()
	dictState = DictationFixingGrammar
	dictLastShortRelease = time.Time{}
	fixer := dictGrammarFixer
	onStatus := dictOnStatus
	onError := dictOnError
	dictMu.Unlock()

	C.ShowDictationOverlay(2)
	C.FlowSetMenuBarState(2)

	if onStatus != nil {
		onStatus(DictationFixingGrammar, "")
	}

	log.Println("[dictation] double-tap detected — fixing grammar of selected text")

	fail := func(msg string) {
		log.Printf("[dictation] grammar fix error: %s", msg)
		C.FlowPlayDictationSound(3)
		C.HideDictationOverlay()
		C.FlowSetMenuBarState(0)
		dictMu.Lock()
		dictState = DictationIdle
		dictMu.Unlock()
		if onError != nil {
			onError(msg)
		}
		if onStatus != nil {
			onStatus(DictationIdle, "")
		}
	}

	if !HasAccessibilityPermission(false) {
		HasAccessibilityPermission(true)
		fail("Grant Accessibility permission in System Settings → Privacy & Security → Accessibility, then try again")
		return
	}

	C.FlowSaveFocusedApp()

	cSelected := C.FlowCopySelectedText()
	if cSelected == nil {
		fail("No text selected — select some text and double-tap to fix grammar")
		return
	}
	selectedText := C.GoString(cSelected)
	C.free(unsafe.Pointer(cSelected))

	if selectedText == "" {
		fail("No text selected — select some text and double-tap to fix grammar")
		return
	}

	log.Printf("[dictation] selected %d chars, sending to LLM for grammar fix...", len(selectedText))

	if fixer == nil {
		fail("Grammar fixer not configured")
		return
	}

	fixedText, err := fixer(selectedText)
	if err != nil {
		fail(fmt.Sprintf("Grammar fix failed: %v", err))
		return
	}

	if fixedText == "" {
		fail("LLM returned empty text")
		return
	}

	log.Printf("[dictation] grammar fixed: %d → %d chars, replacing selection...", len(selectedText), len(fixedText))

	C.HideDictationOverlay()
	C.FlowRestoreFocusedApp()

	log.Println("[dictation] focus restored, pasting corrected text via Cmd+V...")

	cText := C.CString(fixedText)
	defer C.free(unsafe.Pointer(cText))
	C.FlowTypeTextViaClipboard(cText)

	C.FlowSetMenuBarState(0)

	log.Println("[dictation] grammar-fixed text pasted successfully")

	dictMu.Lock()
	dictState = DictationIdle
	dictMu.Unlock()

	if onStatus != nil {
		onStatus(DictationIdle, fixedText)
	}
}

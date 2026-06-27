//go:build darwin

package speech

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework ApplicationServices -framework AudioToolbox -framework QuartzCore

#include <stdlib.h>

// Defined in menubar_darwin.m
void FlowShowMenuBar(void);
void FlowHideMenuBar(void);
void FlowSetMenuBarState(int state);
void FlowSetMenuBarHotkeyLabel(const char *label);
void FlowSetMenuBarGrammarHotkeyLabel(const char *label);
void FlowSetMenuBarQuickAskLabel(const char *label, int enabled);

void FlowSetHotkeyModifier(int keyCode);
void FlowStartHotkeyMonitor(void);
void FlowStopHotkeyMonitor(void);

void FlowTypeTextViaClipboard(const char *text);
char* FlowCopySelectedText(void);
int  FlowReplaceSelectedText(const char *newText);
void FlowSaveFocusedApp(void);
void FlowRestoreFocusedApp(void);
int  FlowCheckAccessibilityPermission(int promptUser);
int  FlowCheckScreenPermission(void);
int  FlowRequestScreenPermission(void);
void FlowPlayDictationSound(int soundType);
void FlowWarmUpAudioSystem(void);

// Defined in overlay_darwin.m
void PreCreateDictationOverlay(void);
void ShowDictationOverlay(int state, const char *label);
void HideDictationOverlay(void);
*/
import "C"
import "unsafe"

// ShowMenuBarIcon shows the Flow pinwheel icon in the macOS menu bar.
func ShowMenuBarIcon() {
	C.FlowShowMenuBar()
}

// HideMenuBarIcon removes the Flow icon from the macOS menu bar.
func HideMenuBarIcon() {
	C.FlowHideMenuBar()
}

// SetMenuBarState updates the menu bar icon state.
// 0 = idle, 1 = recording, 2 = transcribing.
func SetMenuBarState(state int) {
	C.FlowSetMenuBarState(C.int(state))
}

// UpdateMenuBarHotkeyLabel sets the hotkey info text shown in the menu bar dropdown.
func UpdateMenuBarHotkeyLabel(modifier string) {
	displayName := ModifierDisplayName(modifier)
	label := "Hold " + displayName + " and speak"
	cLabel := C.CString(label)
	defer C.free(unsafe.Pointer(cLabel))
	C.FlowSetMenuBarHotkeyLabel(cLabel)

	grammarLabel := "Double-tap " + displayName + " to fix grammar"
	cGrammarLabel := C.CString(grammarLabel)
	defer C.free(unsafe.Pointer(cGrammarLabel))
	C.FlowSetMenuBarGrammarHotkeyLabel(cGrammarLabel)
}

// SaveFocusedApp records the currently frontmost application so it can be
// re-activated later (so showing the HUD doesn't leave Flow in front).
func SaveFocusedApp() { C.FlowSaveFocusedApp() }

// RestoreFocusedApp re-activates the app saved by SaveFocusedApp, returning
// focus to the user's app (the HUD floats above it).
func RestoreFocusedApp() { C.FlowRestoreFocusedApp() }

// HasAccessibilityPermission checks whether the app has macOS Accessibility permission.
func HasAccessibilityPermission(promptUser bool) bool {
	prompt := 0
	if promptUser {
		prompt = 1
	}
	return C.FlowCheckAccessibilityPermission(C.int(prompt)) != 0
}

// HasScreenRecordingPermission reports whether the app has macOS Screen
// Recording permission (always true on pre-10.15). Does not prompt.
func HasScreenRecordingPermission() bool {
	return C.FlowCheckScreenPermission() != 0
}

// RequestScreenRecordingPermission triggers the system Screen Recording prompt
// (first time only) and returns whether access is now granted.
func RequestScreenRecordingPermission() bool {
	return C.FlowRequestScreenPermission() != 0
}

var showAppCallback func()

// RegisterShowAppCallback registers the callback to show the application window when triggered from the menu bar status item.
func RegisterShowAppCallback(cb func()) {
	showAppCallback = cb
}

//export goShowApp
func goShowApp() {
	if showAppCallback != nil {
		showAppCallback()
	}
}

var openQuickAskCallback func()

// RegisterOpenQuickAskCallback registers the callback fired when the user picks
// "Open Quick Ask" from the menu bar.
func RegisterOpenQuickAskCallback(cb func()) {
	openQuickAskCallback = cb
}

//export goOpenQuickAsk
func goOpenQuickAsk() {
	if openQuickAskCallback != nil {
		openQuickAskCallback()
	}
}

// UpdateMenuBarQuickAskLabel sets the Quick Ask hint in the menu bar and toggles
// the visibility of the Quick Ask menu entries. Pass enabled=false to hide them.
func UpdateMenuBarQuickAskLabel(modifier string, enabled bool) {
	var label string
	if enabled {
		label = "Hold " + ModifierDisplayName(modifier) + " to ask the agent"
	}
	cLabel := C.CString(label)
	defer C.free(unsafe.Pointer(cLabel))
	en := 0
	if enabled {
		en = 1
	}
	C.FlowSetMenuBarQuickAskLabel(cLabel, C.int(en))
}


//go:build darwin

package speech

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework ApplicationServices -framework AudioToolbox

#include <stdlib.h>

// Defined in menubar_darwin.m
void FlowShowMenuBar(void);
void FlowHideMenuBar(void);
void FlowSetMenuBarState(int state);
void FlowSetMenuBarHotkeyLabel(const char *label);

void FlowSetHotkeyModifier(int keyCode);
void FlowStartHotkeyMonitor(void);
void FlowStopHotkeyMonitor(void);

void FlowTypeTextViaClipboard(const char *text);
char* FlowCopySelectedText(void);
int  FlowReplaceSelectedText(const char *newText);
void FlowSaveFocusedApp(void);
void FlowRestoreFocusedApp(void);
int  FlowCheckAccessibilityPermission(int promptUser);
void FlowPlayDictationSound(int soundType);
void FlowWarmUpAudioSystem(void);

// Defined in overlay_darwin.m
void PreCreateDictationOverlay(void);
void ShowDictationOverlay(int state);
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
	label := "Hold " + ModifierDisplayName(modifier) + " and speak"
	cLabel := C.CString(label)
	defer C.free(unsafe.Pointer(cLabel))
	C.FlowSetMenuBarHotkeyLabel(cLabel)
}

// HasAccessibilityPermission checks whether the app has macOS Accessibility permission.
func HasAccessibilityPermission(promptUser bool) bool {
	prompt := 0
	if promptUser {
		prompt = 1
	}
	return C.FlowCheckAccessibilityPermission(C.int(prompt)) != 0
}

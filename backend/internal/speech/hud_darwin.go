//go:build darwin

package speech

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework WebKit
#include <stdlib.h>

void FlowShowHUD(const char *url);
void FlowHideHUD(void);
void FlowResizeHUD(int height);
*/
import "C"
import "unsafe"

// ShowHUD displays the floating Quick Agent HUD panel, loading `url`
// (the localhost HUD-server address) the first time it is shown.
func ShowHUD(url string) {
	c := C.CString(url)
	defer C.free(unsafe.Pointer(c))
	C.FlowShowHUD(c)
}

// HideHUD hides the Quick Agent HUD panel.
func HideHUD() {
	C.FlowHideHUD()
}

// ResizeHUD resizes the HUD panel to the given content height (top-anchored).
func ResizeHUD(height int) {
	C.FlowResizeHUD(C.int(height))
}

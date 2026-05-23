//go:build darwin

package speech

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Speech -framework AVFoundation -framework Foundation

#include <stdlib.h>

// Defined in recognizer_darwin.m — native speech recognition (legacy).
void StartSpeechRecognition(const char *locale);
void StopSpeechRecognition(void);

// Defined in recognizer_darwin.m — audio recording for API-based transcription.
void StartAudioRecording(const char *outputPath);
void StopAudioRecording(void);
int  IsAudioRecording(void);
int  CheckMicrophonePermission(void);
*/
import "C"
import (
	"sync"
	"unsafe"
)

// ─── Native Speech Recognition (legacy) ───

// ResultHandler is called when speech recognition produces text.
// text is the full transcription so far; isFinal indicates the segment is complete.
type ResultHandler func(text string, isFinal bool)

// ErrorHandler is called when an error occurs during recognition or recording.
type ErrorHandler func(err string)

var (
	mu            sync.Mutex
	resultHandler ResultHandler
	errorHandler  ErrorHandler
	recordErrHandler ErrorHandler
)

// Start begins native macOS speech recognition using Apple's Speech framework.
// Results are delivered asynchronously via the onResult callback.
// Errors (including permission issues) are delivered via onError.
func Start(locale string, onResult ResultHandler, onError ErrorHandler) {
	mu.Lock()
	resultHandler = onResult
	errorHandler = onError
	mu.Unlock()

	cLocale := C.CString(locale)
	defer C.free(unsafe.Pointer(cLocale))
	C.StartSpeechRecognition(cLocale)
}

// Stop ends the current speech recognition session.
func Stop() {
	C.StopSpeechRecognition()
	mu.Lock()
	resultHandler = nil
	errorHandler = nil
	mu.Unlock()
}

//export goSpeechResult
func goSpeechResult(text *C.char, isFinal C.int) {
	mu.Lock()
	h := resultHandler
	mu.Unlock()
	if h != nil {
		h(C.GoString(text), isFinal != 0)
	}
}

//export goSpeechError
func goSpeechError(errMsg *C.char) {
	mu.Lock()
	h := errorHandler
	mu.Unlock()
	if h != nil {
		h(C.GoString(errMsg))
	}
}

// ─── Audio Recording (for API-based transcription) ───

// StartRecording begins capturing microphone audio to an m4a file at outputPath.
// Permission dialogs are handled natively by macOS.
// If an error occurs (e.g. permission denied), onError is called asynchronously.
func StartRecording(outputPath string, onError ErrorHandler) {
	mu.Lock()
	recordErrHandler = onError
	mu.Unlock()

	cPath := C.CString(outputPath)
	defer C.free(unsafe.Pointer(cPath))
	C.StartAudioRecording(cPath)
}

// StopRecording stops the current audio recording session.
// This call blocks until the recording is fully stopped and the file is written.
func StopRecording() {
	C.StopAudioRecording()
	mu.Lock()
	recordErrHandler = nil
	mu.Unlock()
}

// IsRecording returns whether an audio recording is currently active.
func IsRecording() bool {
	return C.IsAudioRecording() != 0
}

// CheckMicrophonePermission returns the current microphone authorization status:
// 0 = NotDetermined, 1 = Restricted, 2 = Denied, 3 = Authorized
func CheckMicrophonePermission() int {
	return int(C.CheckMicrophonePermission())
}

//export goRecordingError
func goRecordingError(errMsg *C.char) {
	mu.Lock()
	h := recordErrHandler
	mu.Unlock()
	if h != nil {
		h(C.GoString(errMsg))
	}
}

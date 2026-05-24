//go:build darwin

package parser

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Vision -framework Foundation -framework AppKit

#include <stdlib.h>

const char* PerformVisionOCR(const char* base64Str);
*/
import "C"
import (
	"encoding/json"
	"fmt"
	"unsafe"
)

type OCRBlock struct {
	Text   string  `json:"text"`
	Top    float64 `json:"top"`
	Left   float64 `json:"left"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// PerformVisionOCR calls the native macOS Vision framework to perform high-accuracy OCR
// on the provided base64-encoded image data. Returns spatially ordered blocks.
func PerformVisionOCR(base64Str string) ([]OCRBlock, error) {
	cStr := C.CString(base64Str)
	defer C.free(unsafe.Pointer(cStr))

	cResult := C.PerformVisionOCR(cStr)
	if cResult == nil {
		return nil, fmt.Errorf("OCR returned nil")
	}
	defer C.free(unsafe.Pointer(cResult))

	jsonStr := C.GoString(cResult)

	// Check if the result is an error dict
	var errDict struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &errDict); err == nil && errDict.Error != "" {
		return nil, fmt.Errorf("macOS Vision error: %s", errDict.Error)
	}

	var blocks []OCRBlock
	if err := json.Unmarshal([]byte(jsonStr), &blocks); err != nil {
		return nil, fmt.Errorf("failed to unmarshal OCR result: %w, raw response: %s", err, jsonStr)
	}

	return blocks, nil
}

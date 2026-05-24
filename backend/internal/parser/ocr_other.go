//go:build !darwin

package parser

import "fmt"

type OCRBlock struct {
	Text   string  `json:"text"`
	Top    float64 `json:"top"`
	Left   float64 `json:"left"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// PerformVisionOCR is a platform stub for non-macOS compilation.
func PerformVisionOCR(base64Str string) ([]OCRBlock, error) {
	return nil, fmt.Errorf("macOS Vision OCR is only supported on macOS")
}

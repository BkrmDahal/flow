package parser

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/ledongthuc/pdf"
	"github.com/xuri/excelize/v2"
)

// ExtractText attempts to extract plain text from the given file bytes,
// based on the filename extension.
func ExtractText(filename string, data []byte) (string, error) {
	ext := strings.ToLower(filename)

	if strings.HasSuffix(ext, ".pdf") {
		return extractPDF(data)
	}

	if strings.HasSuffix(ext, ".xlsx") {
		return extractXLSX(data)
	}

	// For other known text types or if it's already plain text, just return it as string
	// But usually, we only call this for supported extensions.
	return string(data), nil
}

func extractPDF(data []byte) (string, error) {
	reader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("failed to open PDF: %w", err)
	}

	var buf bytes.Buffer
	b, err := reader.GetPlainText()
	if err != nil {
		return "", fmt.Errorf("failed to read PDF text: %w", err)
	}

	buf.ReadFrom(b)
	return buf.String(), nil
}

func extractXLSX(data []byte) (string, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("failed to open XLSX: %w", err)
	}
	defer f.Close()

	var buf bytes.Buffer
	for _, sheet := range f.GetSheetList() {
		buf.WriteString(fmt.Sprintf("--- Sheet: %s ---\n", sheet))
		rows, err := f.GetRows(sheet)
		if err != nil {
			continue
		}
		for _, row := range rows {
			buf.WriteString(strings.Join(row, "\t") + "\n")
		}
		buf.WriteString("\n")
	}

	return buf.String(), nil
}

package parser

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"sort"
	"strconv"
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

	if strings.HasSuffix(ext, ".docx") {
		return extractDOCX(data)
	}

	if strings.HasSuffix(ext, ".pptx") {
		return extractPPTX(data)
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

func extractDOCX(data []byte) (string, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("failed to open DOCX: %w", err)
	}

	var docFile *zip.File
	for _, f := range reader.File {
		if f.Name == "word/document.xml" {
			docFile = f
			break
		}
	}
	if docFile == nil {
		return "", fmt.Errorf("invalid DOCX: word/document.xml not found")
	}

	rc, err := docFile.Open()
	if err != nil {
		return "", fmt.Errorf("failed to open word/document.xml: %w", err)
	}
	defer rc.Close()

	var buf bytes.Buffer
	decoder := xml.NewDecoder(rc)
	inText := false
	for {
		t, err := decoder.Token()
		if err != nil {
			break
		}
		switch se := t.(type) {
		case xml.StartElement:
			if se.Name.Local == "p" {
				if buf.Len() > 0 && !bytes.HasSuffix(buf.Bytes(), []byte("\n")) {
					buf.WriteString("\n")
				}
			} else if se.Name.Local == "t" {
				inText = true
			}
		case xml.EndElement:
			if se.Name.Local == "t" {
				inText = false
			}
		case xml.CharData:
			if inText {
				buf.Write(se)
			}
		}
	}

	return strings.TrimSpace(buf.String()), nil
}

func extractPPTX(data []byte) (string, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("failed to open PPTX: %w", err)
	}

	type slideItem struct {
		num  int
		file *zip.File
	}
	var slides []slideItem

	for _, f := range reader.File {
		if strings.HasPrefix(f.Name, "ppt/slides/slide") && strings.HasSuffix(f.Name, ".xml") {
			// Extract number from ppt/slides/slideX.xml
			numStr := f.Name[16 : len(f.Name)-4]
			num, err := strconv.Atoi(numStr)
			if err == nil {
				slides = append(slides, slideItem{num: num, file: f})
			}
		}
	}

	sort.Slice(slides, func(i, j int) bool {
		return slides[i].num < slides[j].num
	})

	var buf bytes.Buffer
	for _, slide := range slides {
		if buf.Len() > 0 {
			buf.WriteString("\n")
		}
		buf.WriteString(fmt.Sprintf("--- Slide %d ---\n", slide.num))

		rc, err := slide.file.Open()
		if err != nil {
			continue
		}

		decoder := xml.NewDecoder(rc)
		inText := false
		for {
			t, err := decoder.Token()
			if err != nil {
				break
			}
			switch se := t.(type) {
			case xml.StartElement:
				if se.Name.Local == "p" {
					if buf.Len() > 0 && !bytes.HasSuffix(buf.Bytes(), []byte("\n")) {
						buf.WriteString("\n")
					}
				} else if se.Name.Local == "t" {
					inText = true
				}
			case xml.EndElement:
				if se.Name.Local == "t" {
					inText = false
				}
			case xml.CharData:
				if inText {
					buf.Write(se)
				}
			}
		}
		rc.Close()
	}

	return strings.TrimSpace(buf.String()), nil
}

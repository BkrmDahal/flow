package parser

import (
	"archive/zip"
	"bytes"
	"testing"
)

func TestExtractDOCX(t *testing.T) {
	// Create an in-memory DOCX file
	var docxBytes bytes.Buffer
	zw := zip.NewWriter(&docxBytes)

	// Add word/document.xml
	w, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatalf("failed to create zip file: %v", err)
	}
	xmlContent := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    <w:p>
      <w:r>
        <w:t>Hello, World!</w:t>
      </w:r>
    </w:p>
    <w:p>
      <w:r>
        <w:t>This is a test document.</w:t>
      </w:r>
    </w:p>
  </w:body>
</w:document>`
	_, err = w.Write([]byte(xmlContent))
	if err != nil {
		t.Fatalf("failed to write XML content: %v", err)
	}

	err = zw.Close()
	if err != nil {
		t.Fatalf("failed to close zip writer: %v", err)
	}

	// Test ExtractText
	extracted, err := ExtractText("test.docx", docxBytes.Bytes())
	if err != nil {
		t.Fatalf("failed to extract DOCX text: %v", err)
	}

	expected := "Hello, World!\nThis is a test document."
	if extracted != expected {
		t.Errorf("expected %q, got %q", expected, extracted)
	}
}

func TestExtractPPTX(t *testing.T) {
	// Create an in-memory PPTX file
	var pptxBytes bytes.Buffer
	zw := zip.NewWriter(&pptxBytes)

	// Add ppt/slides/slide2.xml
	w2, err := zw.Create("ppt/slides/slide2.xml")
	if err != nil {
		t.Fatalf("failed to create slide 2: %v", err)
	}
	xml2 := `<p:sld><p:cSld><p:spTree><p:sp><p:txBody><a:p><a:r><a:t>Slide Two Content</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:sld>`
	_, err = w2.Write([]byte(xml2))
	if err != nil {
		t.Fatalf("failed to write slide 2: %v", err)
	}

	// Add ppt/slides/slide1.xml (out of order, to verify sorting)
	w1, err := zw.Create("ppt/slides/slide1.xml")
	if err != nil {
		t.Fatalf("failed to create slide 1: %v", err)
	}
	xml1 := `<p:sld><p:cSld><p:spTree><p:sp><p:txBody><a:p><a:r><a:t>Slide One Content</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:sld>`
	_, err = w1.Write([]byte(xml1))
	if err != nil {
		t.Fatalf("failed to write slide 1: %v", err)
	}

	err = zw.Close()
	if err != nil {
		t.Fatalf("failed to close zip writer: %v", err)
	}

	// Test ExtractText
	extracted, err := ExtractText("presentation.pptx", pptxBytes.Bytes())
	if err != nil {
		t.Fatalf("failed to extract PPTX text: %v", err)
	}

	expected := "--- Slide 1 ---\nSlide One Content\n--- Slide 2 ---\nSlide Two Content"
	if extracted != expected {
		t.Errorf("expected %q, got %q", expected, extracted)
	}
}

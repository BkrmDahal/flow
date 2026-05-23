package streaming

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

func TestStreamManager_StartCancel(t *testing.T) {
	sm := NewStreamManager()
	ctx := context.Background()

	streamCtx, cleanup := sm.Start(ctx, "session-1")
	defer cleanup()

	// Context should be alive.
	select {
	case <-streamCtx.Done():
		t.Fatal("context cancelled prematurely")
	default:
	}

	// Cancel should work.
	sm.Cancel("session-1")
	select {
	case <-streamCtx.Done():
		// expected
	case <-time.After(time.Second):
		t.Fatal("context not cancelled after Cancel()")
	}
}

func TestStreamManager_CancelAll(t *testing.T) {
	sm := NewStreamManager()
	ctx := context.Background()

	ctx1, cleanup1 := sm.Start(ctx, "s1")
	defer cleanup1()
	ctx2, cleanup2 := sm.Start(ctx, "s2")
	defer cleanup2()

	sm.Cancel("") // cancel all

	select {
	case <-ctx1.Done():
	case <-time.After(time.Second):
		t.Fatal("s1 not cancelled")
	}
	select {
	case <-ctx2.Done():
	case <-time.After(time.Second):
		t.Fatal("s2 not cancelled")
	}
}

func TestStreamManager_ReplacesPrevious(t *testing.T) {
	sm := NewStreamManager()
	ctx := context.Background()

	ctx1, cleanup1 := sm.Start(ctx, "session-1")
	defer cleanup1()

	// Starting a new stream for the same session should cancel the previous one.
	_, cleanup2 := sm.Start(ctx, "session-1")
	defer cleanup2()

	select {
	case <-ctx1.Done():
		// expected — previous context should be cancelled
	case <-time.After(time.Second):
		t.Fatal("previous context not cancelled when replaced")
	}
}

func TestSeqCounter_Monotonic(t *testing.T) {
	var sc SeqCounter
	var wg sync.WaitGroup
	results := make([]int64, 100)

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx] = sc.Next()
		}(i)
	}
	wg.Wait()

	seen := make(map[int64]bool)
	for _, v := range results {
		if v <= 0 {
			t.Fatalf("expected positive seq, got %d", v)
		}
		if seen[v] {
			t.Fatalf("duplicate seq %d", v)
		}
		seen[v] = true
	}
}

func TestBuildContent_TextOnly(t *testing.T) {
	raw, err := BuildContent("hello world", nil, ContentOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal("expected JSON string for text-only content")
	}
	if s != "hello world" {
		t.Fatalf("expected 'hello world', got %q", s)
	}
}

func TestBuildContent_WithImageFile(t *testing.T) {
	files := []FileAttachment{
		{Name: "photo.png", MimeType: "image/png", Data: "aGVsbG8="},
	}
	raw, err := BuildContent("check this", files, ContentOptions{})
	if err != nil {
		t.Fatal(err)
	}

	var blocks []map[string]interface{}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		t.Fatal("expected JSON array for multimodal content")
	}
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
	if blocks[0]["type"] != "text" {
		t.Fatalf("first block should be text, got %v", blocks[0]["type"])
	}
	if blocks[1]["type"] != "image" {
		t.Fatalf("second block should be image, got %v", blocks[1]["type"])
	}
}

func TestBuildContent_EmptyTextWithFiles(t *testing.T) {
	files := []FileAttachment{
		{Name: "doc.pdf", MimeType: "application/pdf", Data: "aGVsbG8="},
	}
	raw, err := BuildContent("", files, ContentOptions{})
	if err != nil {
		t.Fatal(err)
	}

	var blocks []map[string]interface{}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		t.Fatal("expected JSON array")
	}
	// Should have 1 block (the PDF doc), no empty text block.
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0]["type"] != "document" {
		t.Fatalf("expected document block, got %v", blocks[0]["type"])
	}
}

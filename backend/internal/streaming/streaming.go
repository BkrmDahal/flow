// Package streaming provides a shared streaming core for agent turns,
// consolidating stream cancellation, sequencing, and multimodal content
// building that was previously duplicated across agentapp.go and cowork.go.
package streaming

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/user/flow/backend/internal/parser"
)

// StreamManager tracks per-session cancellation functions.
type StreamManager struct {
	mu      sync.Mutex
	cancels map[string]context.CancelFunc
}

// NewStreamManager returns a ready-to-use StreamManager.
func NewStreamManager() *StreamManager {
	return &StreamManager{cancels: make(map[string]context.CancelFunc)}
}

// Start creates a cancellable child context for the given session.
// If a previous stream for this session exists, it is cancelled first.
// Returns the new context and a cleanup function the caller must defer.
func (sm *StreamManager) Start(parent context.Context, sessionID string) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)

	sm.mu.Lock()
	if prev, ok := sm.cancels[sessionID]; ok {
		prev()
	}
	sm.cancels[sessionID] = cancel
	sm.mu.Unlock()

	cleanup := func() {
		sm.mu.Lock()
		delete(sm.cancels, sessionID)
		sm.mu.Unlock()
		cancel()
	}
	return ctx, cleanup
}

// Cancel cancels the stream for the given session. If sessionID is empty,
// all active streams are cancelled.
func (sm *StreamManager) Cancel(sessionID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sessionID != "" {
		if cancel, ok := sm.cancels[sessionID]; ok {
			cancel()
			delete(sm.cancels, sessionID)
		}
		return
	}
	for sid, cancel := range sm.cancels {
		cancel()
		delete(sm.cancels, sid)
	}
}

// SeqCounter is a monotonically increasing counter for deduplicating stream
// events on the macOS WebKit bridge.
type SeqCounter struct {
	val int64
}

// Next returns the next sequence number.
func (sc *SeqCounter) Next() int64 {
	return atomic.AddInt64(&sc.val, 1)
}

// --- Multimodal content building ---

// FileAttachment is a file sent from the frontend for multimodal agent tasks.
type FileAttachment struct {
	Name     string `json:"name"`
	MimeType string `json:"mime_type"`
	Data     string `json:"data"` // base64-encoded content
}

// ContentOptions controls how BuildContent processes file attachments.
type ContentOptions struct {
	// ExtractText enables PDF/document text extraction via the parser package.
	ExtractText bool
	// WorkDir is the directory to save non-image files into.
	WorkDir string
}

// contentBlock is the multimodal content block format sent to the LLM API.
type contentBlock struct {
	Type   string      `json:"type"`
	Text   string      `json:"text,omitempty"`
	Source interface{} `json:"source,omitempty"`
}

// BuildContent constructs a JSON content array from text + file attachments.
// This unifies the duplicate content-building logic from agentapp and cowork.
func BuildContent(text string, files []FileAttachment, opts ContentOptions) (json.RawMessage, error) {
	// Simple text-only path.
	if len(files) == 0 {
		return json.Marshal(text)
	}

	var blocks []contentBlock
	if text != "" {
		blocks = append(blocks, contentBlock{Type: "text", Text: text})
	}

	for _, f := range files {
		if strings.HasPrefix(f.MimeType, "image/") {
			blocks = append(blocks, contentBlock{
				Type: "image",
				Source: map[string]interface{}{
					"type":       "base64",
					"media_type": f.MimeType,
					"data":       f.Data,
				},
			})
			// Save the image to the workspace if available, so that coding agents
			// looking for the file on disk can find and process it.
			if opts.WorkDir != "" {
				if rawBytes, err := base64.StdEncoding.DecodeString(f.Data); err == nil {
					destPath := filepath.Join(opts.WorkDir, f.Name)
					if err := os.WriteFile(destPath, rawBytes, 0o644); err != nil {
						log.Printf("failed to save image file %s to workspace: %v", f.Name, err)
					}
				} else {
					log.Printf("failed to decode base64 for image %s: %v", f.Name, err)
				}
			}
			continue
		}

		// Decode base64 for non-image files.
		rawBytes, err := base64.StdEncoding.DecodeString(f.Data)
		if err != nil {
			log.Printf("failed to decode base64 for file %s: %v", f.Name, err)
			continue
		}

		// Save to workspace if available.
		var destPath string
		if opts.WorkDir != "" {
			destPath = filepath.Join(opts.WorkDir, f.Name)
			if err := os.WriteFile(destPath, rawBytes, 0o644); err != nil {
				log.Printf("failed to save file %s to workspace: %v", f.Name, err)
			}
		}

		if opts.ExtractText {
			extracted, err := parser.ExtractText(f.Name, rawBytes)
			if err != nil {
				log.Printf("failed to extract text from %s: %v", f.Name, err)
				var textContent string
				if destPath != "" {
					textContent = fmt.Sprintf("[Attached file %s saved to workspace at %s. Text extraction failed or unsupported.]", f.Name, destPath)
				} else {
					textContent = fmt.Sprintf("[Attached file %s. Text extraction failed or unsupported.]", f.Name)
				}
				blocks = append(blocks, contentBlock{Type: "text", Text: textContent})
			} else {
				textContent := fmt.Sprintf("[Attached file %s content:]\n%s", f.Name, extracted)
				blocks = append(blocks, contentBlock{Type: "text", Text: textContent})
			}
		} else {
			// Native document support for PDFs, text fallback for others.
			if strings.HasSuffix(strings.ToLower(f.Name), ".pdf") {
				blocks = append(blocks, contentBlock{
					Type: "document",
					Source: map[string]interface{}{
						"type":       "base64",
						"media_type": f.MimeType,
						"data":       f.Data,
					},
				})
			} else {
				var textContent string
				if destPath != "" {
					textContent = fmt.Sprintf("[Attached file %s saved to workspace at %s. Context not extracted.]", f.Name, destPath)
				} else {
					textContent = fmt.Sprintf("[Attached file: %s]\n%s", f.Name, f.Data)
				}
				blocks = append(blocks, contentBlock{Type: "text", Text: textContent})
			}
		}
	}

	return json.Marshal(blocks)
}

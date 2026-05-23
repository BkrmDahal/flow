package backend

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/user/flow/backend/internal/config"
	"github.com/user/flow/backend/internal/speech"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// FlowRefinement is one LLM-refined version of a transcript (added in M4).
type FlowRefinement struct {
	Action       string `json:"action"`
	CustomPrompt string `json:"customPrompt,omitempty"`
	Model        string `json:"model"`
	Text         string `json:"text"`
	Timestamp    int64  `json:"timestamp"`
}

// FlowTranscriptInfo is the lightweight record listed in the sidebar.
type FlowTranscriptInfo struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Duration  int    `json:"duration"`
	WordCount int    `json:"wordCount"`
	Timestamp int64  `json:"timestamp"`
}

// FlowTranscript is the full saved transcript with optional refinements.
type FlowTranscript struct {
	ID          string           `json:"id"`
	Text        string           `json:"text"`
	Duration    int              `json:"duration"`
	WordCount   int              `json:"wordCount"`
	Timestamp   int64            `json:"timestamp"`
	Refinements []FlowRefinement `json:"refinements,omitempty"`
}

func (a *App) flowDir() (string, error) {
	base, err := config.FlowDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "flow")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func titleFromText(text string) string {
	t := strings.TrimSpace(text)
	if len(t) > 60 {
		t = t[:60] + "..."
	}
	if t == "" {
		t = "Empty recording"
	}
	return t
}

// SaveFlowTranscript persists a transcript and returns its ID.
func (a *App) SaveFlowTranscript(text string, durationSecs int) (string, error) {
	dir, err := a.flowDir()
	if err != nil {
		return "", err
	}

	ts := time.Now().UnixMilli()
	id := fmt.Sprintf("flow-%d", ts)

	t := FlowTranscript{
		ID:        id,
		Text:      text,
		Duration:  durationSecs,
		WordCount: len(strings.Fields(text)),
		Timestamp: ts,
	}

	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal transcript: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".json"), data, 0o644); err != nil {
		return "", fmt.Errorf("write transcript: %w", err)
	}

	log.Printf("[flow] saved %s (%d words, %ds)", id, t.WordCount, durationSecs)
	return id, nil
}

// ListFlowTranscripts returns metadata for every saved transcript, newest first.
func (a *App) ListFlowTranscripts() ([]FlowTranscriptInfo, error) {
	dir, err := a.flowDir()
	if err != nil {
		return []FlowTranscriptInfo{}, nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []FlowTranscriptInfo{}, nil
		}
		return nil, fmt.Errorf("read flow dir: %w", err)
	}

	items := make([]FlowTranscriptInfo, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var t FlowTranscript
		if err := json.Unmarshal(data, &t); err != nil {
			continue
		}
		items = append(items, FlowTranscriptInfo{
			ID:        t.ID,
			Title:     titleFromText(t.Text),
			Duration:  t.Duration,
			WordCount: t.WordCount,
			Timestamp: t.Timestamp,
		})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].Timestamp > items[j].Timestamp
	})
	return items, nil
}

// LoadFlowTranscript reads a transcript by id.
func (a *App) LoadFlowTranscript(id string) (*FlowTranscript, error) {
	dir, err := a.flowDir()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, id+".json"))
	if err != nil {
		return nil, fmt.Errorf("read transcript %s: %w", id, err)
	}
	var t FlowTranscript
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("parse transcript %s: %w", id, err)
	}
	return &t, nil
}

// DeleteFlowTranscript removes a transcript file.
func (a *App) DeleteFlowTranscript(id string) error {
	dir, err := a.flowDir()
	if err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(dir, id+".json")); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete transcript %s: %w", id, err)
	}
	log.Printf("[flow] deleted %s", id)
	return nil
}

// StartFlow begins capturing microphone audio to a temp m4a file.
// Errors are emitted on "flow:error". The actual transcription happens in StopFlow.
func (a *App) StartFlow(locale string) {
	tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("flow-record-%d.m4a", time.Now().UnixMilli()))

	a.voiceMu.Lock()
	a.voiceRecordingPath = tmpFile
	a.voiceMu.Unlock()

	log.Printf("[flow] start → %s", tmpFile)
	speech.SetMenuBarState(1) // recording

	speech.StartRecording(tmpFile, func(errMsg string) {
		log.Printf("[flow] recording error: %s", errMsg)
		speech.SetMenuBarState(0)
		wailsRuntime.EventsEmit(a.ctx, "flow:error", map[string]interface{}{
			"error": errMsg,
		})
	})
}

// StopFlow stops the current recording and transcribes it via the configured
// speech-to-text API. Returns the transcribed text.
func (a *App) StopFlow() (string, error) {
	log.Println("[flow] stop")
	speech.StopRecording()
	speech.SetMenuBarState(2) // transcribing
	defer speech.SetMenuBarState(0)

	// Brief pause to let the recorder fully flush the file.
	time.Sleep(150 * time.Millisecond)

	a.voiceMu.Lock()
	path := a.voiceRecordingPath
	a.voiceRecordingPath = ""
	a.voiceMu.Unlock()

	if path == "" {
		return "", fmt.Errorf("no recording in progress")
	}
	defer os.Remove(path)

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read recording file: %w", err)
	}
	if len(data) == 0 {
		return "", fmt.Errorf("recorded file is empty — try speaking louder or closer to the microphone")
	}

	log.Printf("[flow] recorded %d bytes, sending to API...", len(data))

	cfg, err := a.loadSpeechConfig()
	if err != nil {
		return "", err
	}

	audioBase64 := base64.StdEncoding.EncodeToString(data)
	result, err := speech.Transcribe(cfg, audioBase64, "audio/m4a")
	if err != nil {
		return "", err
	}

	log.Printf("[flow] transcription complete: %d chars", len(result.Text))
	return a.ApplySnippets(result.Text), nil
}

// loadSpeechConfig builds a TranscribeConfig from the app's persisted settings.
func (a *App) loadSpeechConfig() (speech.TranscribeConfig, error) {
	if a.cfg == nil {
		return speech.TranscribeConfig{}, fmt.Errorf("config not loaded")
	}
	provider := speech.Provider(a.cfg.SpeechProvider)
	if provider == "" {
		provider = speech.ProviderWhisper
	}
	return speech.TranscribeConfig{
		Provider: provider,
		APIKey:   a.cfg.SpeechAPIKey,
		Language: a.cfg.SpeechLanguage,
		Model:    a.cfg.SpeechModel,
		Prompt:   a.cfg.SpeechPrompt,
	}, nil
}

// appendRefinement is a helper used by M4 (flow_refine.go).
func (a *App) appendRefinement(transcriptID string, ref FlowRefinement) error {
	dir, err := a.flowDir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, transcriptID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read transcript %s: %w", transcriptID, err)
	}
	var t FlowTranscript
	if err := json.Unmarshal(data, &t); err != nil {
		return fmt.Errorf("parse transcript %s: %w", transcriptID, err)
	}
	t.Refinements = append(t.Refinements, ref)
	out, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal transcript: %w", err)
	}
	return os.WriteFile(path, out, 0o644)
}

// UpdateFlowTranscript updates the text and word count of an existing transcript.
func (a *App) UpdateFlowTranscript(id string, text string) error {
	dir, err := a.flowDir()
	if err != nil {
		return err
	}

	path := filepath.Join(dir, id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read transcript %s: %w", id, err)
	}

	var t FlowTranscript
	if err := json.Unmarshal(data, &t); err != nil {
		return fmt.Errorf("parse transcript %s: %w", id, err)
	}

	t.Text = text
	t.WordCount = len(strings.Fields(text))

	out, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal transcript: %w", err)
	}

	if err := os.WriteFile(path, out, 0o644); err != nil {
		return fmt.Errorf("write transcript: %w", err)
	}

	log.Printf("[flow] updated %s (%d words)", id, t.WordCount)
	return nil
}


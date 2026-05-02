package speech

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
)

// Provider represents a speech-to-text API provider.
type Provider string

const (
	ProviderWhisper  Provider = "whisper"
	ProviderDeepgram Provider = "deepgram"
	ProviderLocal    Provider = "local" // bundled whisper.cpp, no API key needed
)

// OpenAI Transcription models.
// See: https://developers.openai.com/api/docs/guides/speech-to-text
const (
	ModelGPT4oMiniTranscribe = "gpt-4o-mini-transcribe" // Fast, good quality (default)
	ModelGPT4oTranscribe     = "gpt-4o-transcribe"      // Highest quality
	ModelWhisper1            = "whisper-1"               // Legacy open-source model
)

// maxUploadBytes is the OpenAI file upload limit (25 MB).
const maxUploadBytes = 25 * 1024 * 1024

// TranscribeConfig holds configuration for API-based speech transcription.
type TranscribeConfig struct {
	Provider Provider
	APIKey   string
	Language string // e.g. "en"
	Model    string // OpenAI model: "gpt-4o-mini-transcribe", "gpt-4o-transcribe", or "whisper-1"
	Prompt   string // Optional prompt to guide transcription (e.g. remove filler words, fix grammar)
}

// TranscribeResult holds the result of a transcription.
type TranscribeResult struct {
	Text string `json:"text"`
}

// Transcribe sends audio data to a speech-to-text API and returns the transcription.
// audioBase64 is the base64-encoded audio data.
// mimeType is the MIME type of the audio (e.g. "audio/webm", "audio/wav").
func Transcribe(cfg TranscribeConfig, audioBase64 string, mimeType string) (*TranscribeResult, error) {
	audioBytes, err := base64.StdEncoding.DecodeString(audioBase64)
	if err != nil {
		return nil, fmt.Errorf("decode audio: %w", err)
	}
	if len(audioBytes) == 0 {
		return nil, fmt.Errorf("empty audio data")
	}

	switch cfg.Provider {
	case ProviderLocal:
		return transcribeLocal(cfg, audioBytes, mimeType)
	case ProviderWhisper:
		if cfg.APIKey == "" {
			return nil, fmt.Errorf("Please setup OPENAI_API_KEY in Settings")
		}
		return transcribeOpenAI(cfg, audioBytes, mimeType)
	case ProviderDeepgram:
		if cfg.APIKey == "" {
			return nil, fmt.Errorf("Please setup DEEPGRAM_API_KEY in Settings")
		}
		return transcribeDeepgram(cfg, audioBytes, mimeType)
	default:
		return nil, fmt.Errorf("unknown speech provider: %s", cfg.Provider)
	}
}

// transcribeOpenAI sends audio to OpenAI's Transcriptions API.
// Endpoint: POST https://api.openai.com/v1/audio/transcriptions
// Supported models: gpt-4o-mini-transcribe, gpt-4o-transcribe, whisper-1
// Supported input formats: mp3, mp4, mpeg, mpga, m4a, wav, webm
// Max file size: 25 MB
// Docs: https://developers.openai.com/api/docs/guides/speech-to-text
func transcribeOpenAI(cfg TranscribeConfig, audio []byte, mimeType string) (*TranscribeResult, error) {
	// Enforce 25 MB upload limit.
	if len(audio) > maxUploadBytes {
		return nil, fmt.Errorf("audio file too large (%d MB) — OpenAI limit is 25 MB", len(audio)/(1024*1024))
	}

	// Determine file extension from MIME type.
	ext := fileExtForMime(mimeType)

	model := cfg.Model
	if model == "" {
		model = ModelGPT4oMiniTranscribe
	}

	// Build multipart form data.
	// Required fields: file, model
	// Optional fields: language, response_format, prompt
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	// file — the audio file to transcribe.
	part, err := w.CreateFormFile("file", "audio"+ext)
	if err != nil {
		return nil, fmt.Errorf("create form file: %w", err)
	}
	if _, err := part.Write(audio); err != nil {
		return nil, fmt.Errorf("write audio: %w", err)
	}

	// model — the transcription model to use.
	if err := w.WriteField("model", model); err != nil {
		return nil, fmt.Errorf("write model field: %w", err)
	}

	// response_format — explicitly request JSON.
	// gpt-4o-transcribe and gpt-4o-mini-transcribe support "json" or "text".
	// whisper-1 supports "json", "text", "srt", "verbose_json", "vtt".
	if err := w.WriteField("response_format", "json"); err != nil {
		return nil, fmt.Errorf("write response_format field: %w", err)
	}

	// language — optional ISO-639-1 code to improve accuracy.
	if cfg.Language != "" {
		if err := w.WriteField("language", cfg.Language); err != nil {
			return nil, fmt.Errorf("write language field: %w", err)
		}
	}

	// prompt — optional text to guide the model's transcription style and vocabulary.
	// Helps remove filler words, fix grammar, and improve overall transcription quality.
	if cfg.Prompt != "" {
		if err := w.WriteField("prompt", cfg.Prompt); err != nil {
			return nil, fmt.Errorf("write prompt field: %w", err)
		}
	}

	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("close multipart: %w", err)
	}

	req, err := http.NewRequest("POST", "https://api.openai.com/v1/audio/transcriptions", &buf)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("OpenAI transcription API request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		// Try to extract a useful error message from the response.
		var apiErr struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(body, &apiErr) == nil && apiErr.Error.Message != "" {
			return nil, fmt.Errorf("OpenAI API error (%d): %s", resp.StatusCode, apiErr.Error.Message)
		}
		return nil, fmt.Errorf("OpenAI API error (%d): %s", resp.StatusCode, string(body))
	}

	var result TranscribeResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &result, nil
}

// transcribeDeepgram sends audio to Deepgram's API.
func transcribeDeepgram(cfg TranscribeConfig, audio []byte, mimeType string) (*TranscribeResult, error) {
	url := "https://api.deepgram.com/v1/listen?model=nova-2&smart_format=true"
	if cfg.Language != "" {
		url += "&language=" + cfg.Language
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(audio))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Token "+cfg.APIKey)
	req.Header.Set("Content-Type", mimeType)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("deepgram API request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("deepgram API error (%d): %s", resp.StatusCode, string(body))
	}

	// Parse Deepgram's response structure.
	var dgResp struct {
		Results struct {
			Channels []struct {
				Alternatives []struct {
					Transcript string `json:"transcript"`
				} `json:"alternatives"`
			} `json:"channels"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &dgResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	text := ""
	if len(dgResp.Results.Channels) > 0 && len(dgResp.Results.Channels[0].Alternatives) > 0 {
		text = dgResp.Results.Channels[0].Alternatives[0].Transcript
	}

	return &TranscribeResult{Text: text}, nil
}

// fileExtForMime returns a file extension for a given audio MIME type.
// OpenAI supports: mp3, mp4, mpeg, mpga, m4a, wav, webm
func fileExtForMime(mimeType string) string {
	switch mimeType {
	case "audio/wav", "audio/wave", "audio/x-wav":
		return ".wav"
	case "audio/mp3", "audio/mpeg", "audio/mpga":
		return ".mp3"
	case "audio/mp4":
		return ".mp4"
	case "audio/m4a", "audio/x-m4a":
		return ".m4a"
	case "audio/ogg":
		return ".ogg"
	case "audio/flac":
		return ".flac"
	case "audio/webm":
		return ".webm"
	default:
		return ".webm"
	}
}

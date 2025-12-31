package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// ==================== Audio API Types ====================

// SpeechRequest represents a text-to-speech request
type SpeechRequest struct {
	Model          string   `json:"model"`
	Input          string   `json:"input"`
	Voice          string   `json:"voice"`
	ResponseFormat string   `json:"response_format,omitempty"` // mp3, opus, aac, flac, wav, pcm
	Speed          *float64 `json:"speed,omitempty"`           // 0.25 to 4.0
}

// TranscriptionRequest represents a speech-to-text request
type TranscriptionRequest struct {
	Model          string   `json:"model"`
	Language       string   `json:"language,omitempty"`
	Prompt         string   `json:"prompt,omitempty"`
	ResponseFormat string   `json:"response_format,omitempty"` // json, text, srt, verbose_json, vtt
	Temperature    *float64 `json:"temperature,omitempty"`
}

// TranscriptionResponse represents the transcription result
type TranscriptionResponse struct {
	Text string `json:"text"`
}

// VerboseTranscriptionResponse represents detailed transcription result
type VerboseTranscriptionResponse struct {
	Task     string    `json:"task"`
	Language string    `json:"language"`
	Duration float64   `json:"duration"`
	Text     string    `json:"text"`
	Segments []Segment `json:"segments,omitempty"`
}

// Segment represents a transcription segment
type Segment struct {
	ID               int     `json:"id"`
	Seek             int     `json:"seek"`
	Start            float64 `json:"start"`
	End              float64 `json:"end"`
	Text             string  `json:"text"`
	Tokens           []int   `json:"tokens"`
	Temperature      float64 `json:"temperature"`
	AvgLogprob       float64 `json:"avg_logprob"`
	CompressionRatio float64 `json:"compression_ratio"`
	NoSpeechProb     float64 `json:"no_speech_prob"`
}

// TranslationResponse represents the translation result
type TranslationResponse struct {
	Text string `json:"text"`
}

// ==================== Audio API Handlers ====================

// Speech handles POST /v1/audio/speech - Text to Speech
func (h *Handler) Speech(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	requestID := fmt.Sprintf("speech_%d", startTime.UnixNano())

	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed", "invalid_request_error")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("[%s] ERR read body: %v", requestID, err)
		writeError(w, http.StatusBadRequest, "Failed to read request body", "invalid_request_error")
		return
	}
	defer r.Body.Close()

	var req SpeechRequest
	if err := json.Unmarshal(body, &req); err != nil {
		log.Printf("[%s] ERR invalid JSON: %v", requestID, err)
		writeError(w, http.StatusBadRequest, "Invalid JSON", "invalid_request_error")
		return
	}

	// Validate required fields
	if req.Model == "" {
		writeError(w, http.StatusBadRequest, "Missing required parameter: model", "invalid_request_error")
		return
	}
	if req.Input == "" {
		writeError(w, http.StatusBadRequest, "Missing required parameter: input", "invalid_request_error")
		return
	}
	if req.Voice == "" {
		writeError(w, http.StatusBadRequest, "Missing required parameter: voice", "invalid_request_error")
		return
	}

	// Validate voice
	validVoices := map[string]bool{
		"alloy": true, "echo": true, "fable": true,
		"onyx": true, "nova": true, "shimmer": true,
	}
	if !validVoices[req.Voice] {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid voice: %s", req.Voice), "invalid_request_error")
		return
	}

	// Default response format
	if req.ResponseFormat == "" {
		req.ResponseFormat = "mp3"
	}

	log.Printf("[%s] ⇣ REQ (Speech) model=%s voice=%s format=%s input_len=%d",
		requestID, req.Model, req.Voice, req.ResponseFormat, len(req.Input))

	// Note: This is a mock implementation
	// In production, you would proxy to an actual TTS service
	// For now, return an error indicating the feature is not fully implemented
	writeError(w, http.StatusNotImplemented,
		"Text-to-speech is not supported by the upstream service. This endpoint requires a TTS-capable backend.",
		"not_implemented")
}

// Transcriptions handles POST /v1/audio/transcriptions - Speech to Text
func (h *Handler) Transcriptions(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	requestID := fmt.Sprintf("transcribe_%d", startTime.UnixNano())

	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed", "invalid_request_error")
		return
	}

	// Parse multipart form (max 25MB as per OpenAI spec)
	if err := r.ParseMultipartForm(25 << 20); err != nil {
		log.Printf("[%s] ERR parse form: %v", requestID, err)
		writeError(w, http.StatusBadRequest, "Failed to parse multipart form", "invalid_request_error")
		return
	}

	// Get model
	model := r.FormValue("model")
	if model == "" {
		writeError(w, http.StatusBadRequest, "Missing required parameter: model", "invalid_request_error")
		return
	}

	// Get file
	file, header, err := r.FormFile("file")
	if err != nil {
		log.Printf("[%s] ERR get file: %v", requestID, err)
		writeError(w, http.StatusBadRequest, "Missing required parameter: file", "invalid_request_error")
		return
	}
	defer file.Close()

	// Get optional parameters
	language := r.FormValue("language")
	prompt := r.FormValue("prompt")
	responseFormat := r.FormValue("response_format")
	if responseFormat == "" {
		responseFormat = "json"
	}

	log.Printf("[%s] ⇣ REQ (Transcription) model=%s file=%s size=%d format=%s",
		requestID, model, header.Filename, header.Size, responseFormat)

	// Note: This is a mock implementation
	// In production, you would proxy to an actual STT service
	_ = language
	_ = prompt

	writeError(w, http.StatusNotImplemented,
		"Speech-to-text transcription is not supported by the upstream service. This endpoint requires a Whisper-capable backend.",
		"not_implemented")
}

// Translations handles POST /v1/audio/translations - Translate audio to English
func (h *Handler) Translations(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	requestID := fmt.Sprintf("translate_%d", startTime.UnixNano())

	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed", "invalid_request_error")
		return
	}

	// Parse multipart form (max 25MB as per OpenAI spec)
	if err := r.ParseMultipartForm(25 << 20); err != nil {
		log.Printf("[%s] ERR parse form: %v", requestID, err)
		writeError(w, http.StatusBadRequest, "Failed to parse multipart form", "invalid_request_error")
		return
	}

	// Get model
	model := r.FormValue("model")
	if model == "" {
		writeError(w, http.StatusBadRequest, "Missing required parameter: model", "invalid_request_error")
		return
	}

	// Get file
	file, header, err := r.FormFile("file")
	if err != nil {
		log.Printf("[%s] ERR get file: %v", requestID, err)
		writeError(w, http.StatusBadRequest, "Missing required parameter: file", "invalid_request_error")
		return
	}
	defer file.Close()

	// Get optional parameters
	prompt := r.FormValue("prompt")
	responseFormat := r.FormValue("response_format")
	if responseFormat == "" {
		responseFormat = "json"
	}

	log.Printf("[%s] ⇣ REQ (Translation) model=%s file=%s size=%d format=%s",
		requestID, model, header.Filename, header.Size, responseFormat)

	// Note: This is a mock implementation
	_ = prompt

	writeError(w, http.StatusNotImplemented,
		"Audio translation is not supported by the upstream service. This endpoint requires a Whisper-capable backend.",
		"not_implemented")
}

// ValidateAudioFormat checks if the audio format is supported
func ValidateAudioFormat(filename string) bool {
	validFormats := []string{".mp3", ".mp4", ".mpeg", ".mpga", ".m4a", ".wav", ".webm", ".flac", ".ogg"}
	lower := strings.ToLower(filename)
	for _, format := range validFormats {
		if strings.HasSuffix(lower, format) {
			return true
		}
	}
	return false
}

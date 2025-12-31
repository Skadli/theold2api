package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// ==================== Images API Types ====================

// ImageGenerationRequest represents an image generation request
type ImageGenerationRequest struct {
	Model          string `json:"model,omitempty"`           // dall-e-2, dall-e-3
	Prompt         string `json:"prompt"`
	N              *int   `json:"n,omitempty"`               // Number of images (1-10 for dall-e-2, 1 for dall-e-3)
	Size           string `json:"size,omitempty"`            // 256x256, 512x512, 1024x1024, 1792x1024, 1024x1792
	Quality        string `json:"quality,omitempty"`         // standard, hd (dall-e-3 only)
	Style          string `json:"style,omitempty"`           // vivid, natural (dall-e-3 only)
	ResponseFormat string `json:"response_format,omitempty"` // url, b64_json
	User           string `json:"user,omitempty"`
}

// ImageEditRequest represents an image edit request
type ImageEditRequest struct {
	Image          string `json:"image"`                     // Base64 encoded image
	Mask           string `json:"mask,omitempty"`            // Base64 encoded mask
	Prompt         string `json:"prompt"`
	Model          string `json:"model,omitempty"`
	N              *int   `json:"n,omitempty"`
	Size           string `json:"size,omitempty"`
	ResponseFormat string `json:"response_format,omitempty"`
	User           string `json:"user,omitempty"`
}

// ImageVariationRequest represents an image variation request
type ImageVariationRequest struct {
	Image          string `json:"image"` // Base64 encoded image
	Model          string `json:"model,omitempty"`
	N              *int   `json:"n,omitempty"`
	Size           string `json:"size,omitempty"`
	ResponseFormat string `json:"response_format,omitempty"`
	User           string `json:"user,omitempty"`
}

// ImagesResponse represents the response for image operations
type ImagesResponse struct {
	Created int64       `json:"created"`
	Data    []ImageData `json:"data"`
}

// ImageData represents a single image in the response
type ImageData struct {
	URL           string `json:"url,omitempty"`
	B64JSON       string `json:"b64_json,omitempty"`
	RevisedPrompt string `json:"revised_prompt,omitempty"`
}

// ==================== Images API Handlers ====================

// ImageGenerations handles POST /v1/images/generations
func (h *Handler) ImageGenerations(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	requestID := fmt.Sprintf("img_gen_%d", startTime.UnixNano())

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

	var req ImageGenerationRequest
	if err := json.Unmarshal(body, &req); err != nil {
		log.Printf("[%s] ERR invalid JSON: %v", requestID, err)
		writeError(w, http.StatusBadRequest, "Invalid JSON", "invalid_request_error")
		return
	}

	// Validate required fields
	if req.Prompt == "" {
		writeError(w, http.StatusBadRequest, "Missing required parameter: prompt", "invalid_request_error")
		return
	}

	// Set defaults
	if req.Model == "" {
		req.Model = "dall-e-2"
	}
	if req.Size == "" {
		req.Size = "1024x1024"
	}
	if req.ResponseFormat == "" {
		req.ResponseFormat = "url"
	}
	n := 1
	if req.N != nil {
		n = *req.N
	}

	// Validate size
	validSizes := map[string]bool{
		"256x256":   true,
		"512x512":   true,
		"1024x1024": true,
		"1792x1024": true,
		"1024x1792": true,
	}
	if !validSizes[req.Size] {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid size: %s", req.Size), "invalid_request_error")
		return
	}

	// Validate n
	if n < 1 || n > 10 {
		writeError(w, http.StatusBadRequest, "n must be between 1 and 10", "invalid_request_error")
		return
	}
	if req.Model == "dall-e-3" && n > 1 {
		writeError(w, http.StatusBadRequest, "dall-e-3 only supports n=1", "invalid_request_error")
		return
	}

	log.Printf("[%s] ⇣ REQ (ImageGen) model=%s size=%s n=%d prompt_len=%d",
		requestID, req.Model, req.Size, n, len(req.Prompt))

	// Note: This is a mock implementation
	// In production, you would proxy to an actual image generation service
	writeError(w, http.StatusNotImplemented,
		"Image generation is not supported by the upstream service. This endpoint requires a DALL-E capable backend.",
		"not_implemented")
}

// ImageEdits handles POST /v1/images/edits
func (h *Handler) ImageEdits(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	requestID := fmt.Sprintf("img_edit_%d", startTime.UnixNano())

	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed", "invalid_request_error")
		return
	}

	// Parse multipart form (max 4MB per image as per OpenAI spec)
	if err := r.ParseMultipartForm(20 << 20); err != nil {
		log.Printf("[%s] ERR parse form: %v", requestID, err)
		writeError(w, http.StatusBadRequest, "Failed to parse multipart form", "invalid_request_error")
		return
	}

	// Get required fields
	prompt := r.FormValue("prompt")
	if prompt == "" {
		writeError(w, http.StatusBadRequest, "Missing required parameter: prompt", "invalid_request_error")
		return
	}

	// Get image file
	_, header, err := r.FormFile("image")
	if err != nil {
		log.Printf("[%s] ERR get image: %v", requestID, err)
		writeError(w, http.StatusBadRequest, "Missing required parameter: image", "invalid_request_error")
		return
	}

	// Get optional parameters
	model := r.FormValue("model")
	if model == "" {
		model = "dall-e-2"
	}
	size := r.FormValue("size")
	if size == "" {
		size = "1024x1024"
	}

	log.Printf("[%s] ⇣ REQ (ImageEdit) model=%s size=%s image=%s prompt_len=%d",
		requestID, model, size, header.Filename, len(prompt))

	writeError(w, http.StatusNotImplemented,
		"Image editing is not supported by the upstream service. This endpoint requires a DALL-E capable backend.",
		"not_implemented")
}

// ImageVariations handles POST /v1/images/variations
func (h *Handler) ImageVariations(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	requestID := fmt.Sprintf("img_var_%d", startTime.UnixNano())

	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed", "invalid_request_error")
		return
	}

	// Parse multipart form
	if err := r.ParseMultipartForm(20 << 20); err != nil {
		log.Printf("[%s] ERR parse form: %v", requestID, err)
		writeError(w, http.StatusBadRequest, "Failed to parse multipart form", "invalid_request_error")
		return
	}

	// Get image file
	_, header, err := r.FormFile("image")
	if err != nil {
		log.Printf("[%s] ERR get image: %v", requestID, err)
		writeError(w, http.StatusBadRequest, "Missing required parameter: image", "invalid_request_error")
		return
	}

	// Get optional parameters
	model := r.FormValue("model")
	if model == "" {
		model = "dall-e-2"
	}
	size := r.FormValue("size")
	if size == "" {
		size = "1024x1024"
	}

	log.Printf("[%s] ⇣ REQ (ImageVariation) model=%s size=%s image=%s",
		requestID, model, size, header.Filename)

	writeError(w, http.StatusNotImplemented,
		"Image variations is not supported by the upstream service. This endpoint requires a DALL-E capable backend.",
		"not_implemented")
}

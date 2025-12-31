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

// ==================== Moderations API Types ====================

// ModerationRequest represents a moderation request
type ModerationRequest struct {
	Model string      `json:"model,omitempty"` // text-moderation-latest, text-moderation-stable, omni-moderation-latest
	Input interface{} `json:"input"`           // string or array of strings
}

// ModerationResponse represents the moderation result
type ModerationResponse struct {
	ID      string             `json:"id"`
	Model   string             `json:"model"`
	Results []ModerationResult `json:"results"`
}

// ModerationResult represents a single moderation result
type ModerationResult struct {
	Flagged        bool                   `json:"flagged"`
	Categories     ModerationCategories   `json:"categories"`
	CategoryScores ModerationCategoryScores `json:"category_scores"`
}

// ModerationCategories represents the boolean flags for each category
type ModerationCategories struct {
	Sexual                 bool `json:"sexual"`
	Hate                   bool `json:"hate"`
	Harassment             bool `json:"harassment"`
	SelfHarm               bool `json:"self-harm"`
	SexualMinors           bool `json:"sexual/minors"`
	HateThreatening        bool `json:"hate/threatening"`
	ViolenceGraphic        bool `json:"violence/graphic"`
	SelfHarmIntent         bool `json:"self-harm/intent"`
	SelfHarmInstructions   bool `json:"self-harm/instructions"`
	HarassmentThreatening  bool `json:"harassment/threatening"`
	Violence               bool `json:"violence"`
}

// ModerationCategoryScores represents the confidence scores for each category
type ModerationCategoryScores struct {
	Sexual                 float64 `json:"sexual"`
	Hate                   float64 `json:"hate"`
	Harassment             float64 `json:"harassment"`
	SelfHarm               float64 `json:"self-harm"`
	SexualMinors           float64 `json:"sexual/minors"`
	HateThreatening        float64 `json:"hate/threatening"`
	ViolenceGraphic        float64 `json:"violence/graphic"`
	SelfHarmIntent         float64 `json:"self-harm/intent"`
	SelfHarmInstructions   float64 `json:"self-harm/instructions"`
	HarassmentThreatening  float64 `json:"harassment/threatening"`
	Violence               float64 `json:"violence"`
}

// ==================== Moderations API Handler ====================

// Moderations handles POST /v1/moderations
func (h *Handler) Moderations(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	requestID := fmt.Sprintf("mod_%d", startTime.UnixNano())

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

	var req ModerationRequest
	if err := json.Unmarshal(body, &req); err != nil {
		log.Printf("[%s] ERR invalid JSON: %v", requestID, err)
		writeError(w, http.StatusBadRequest, "Invalid JSON", "invalid_request_error")
		return
	}

	// Validate input
	if req.Input == nil {
		writeError(w, http.StatusBadRequest, "Missing required parameter: input", "invalid_request_error")
		return
	}

	// Parse input (can be string or array of strings)
	var inputs []string
	switch v := req.Input.(type) {
	case string:
		inputs = []string{v}
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok {
				inputs = append(inputs, s)
			}
		}
	default:
		writeError(w, http.StatusBadRequest, "Input must be a string or array of strings", "invalid_request_error")
		return
	}

	if len(inputs) == 0 {
		writeError(w, http.StatusBadRequest, "Input cannot be empty", "invalid_request_error")
		return
	}

	// Set default model
	model := req.Model
	if model == "" {
		model = "text-moderation-latest"
	}

	log.Printf("[%s] ⇣ REQ (Moderation) model=%s inputs=%d", requestID, model, len(inputs))

	// Generate mock moderation results
	// In production, you would proxy to an actual moderation service
	var results []ModerationResult
	for _, input := range inputs {
		result := generateMockModerationResult(input)
		results = append(results, result)
	}

	response := ModerationResponse{
		ID:      fmt.Sprintf("modr-%d", time.Now().UnixNano()),
		Model:   model,
		Results: results,
	}

	log.Printf("[%s] ✔ OK  inputs=%d duration=%v", requestID, len(inputs), time.Since(startTime))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// generateMockModerationResult generates a mock moderation result
// This is a simple heuristic-based implementation
func generateMockModerationResult(input string) ModerationResult {
	lower := strings.ToLower(input)

	// Simple keyword-based detection (very basic, for demonstration only)
	// In production, use a proper ML model or external service

	categories := ModerationCategories{}
	scores := ModerationCategoryScores{}
	flagged := false

	// Check for potentially harmful content patterns
	// These are very basic checks and should not be used in production

	// Violence indicators
	violenceKeywords := []string{"kill", "murder", "attack", "weapon", "bomb", "shoot"}
	for _, kw := range violenceKeywords {
		if strings.Contains(lower, kw) {
			scores.Violence = 0.7
			categories.Violence = true
			flagged = true
			break
		}
	}

	// Hate speech indicators
	hateKeywords := []string{"hate", "racist", "discrimination"}
	for _, kw := range hateKeywords {
		if strings.Contains(lower, kw) {
			scores.Hate = 0.6
			categories.Hate = true
			flagged = true
			break
		}
	}

	// Self-harm indicators
	selfHarmKeywords := []string{"suicide", "self-harm", "hurt myself"}
	for _, kw := range selfHarmKeywords {
		if strings.Contains(lower, kw) {
			scores.SelfHarm = 0.8
			categories.SelfHarm = true
			flagged = true
			break
		}
	}

	// Set low scores for non-flagged categories
	if scores.Sexual == 0 {
		scores.Sexual = 0.001
	}
	if scores.Hate == 0 {
		scores.Hate = 0.001
	}
	if scores.Harassment == 0 {
		scores.Harassment = 0.001
	}
	if scores.SelfHarm == 0 {
		scores.SelfHarm = 0.001
	}
	if scores.SexualMinors == 0 {
		scores.SexualMinors = 0.0001
	}
	if scores.HateThreatening == 0 {
		scores.HateThreatening = 0.001
	}
	if scores.ViolenceGraphic == 0 {
		scores.ViolenceGraphic = 0.001
	}
	if scores.SelfHarmIntent == 0 {
		scores.SelfHarmIntent = 0.001
	}
	if scores.SelfHarmInstructions == 0 {
		scores.SelfHarmInstructions = 0.001
	}
	if scores.HarassmentThreatening == 0 {
		scores.HarassmentThreatening = 0.001
	}
	if scores.Violence == 0 {
		scores.Violence = 0.001
	}

	return ModerationResult{
		Flagged:        flagged,
		Categories:     categories,
		CategoryScores: scores,
	}
}

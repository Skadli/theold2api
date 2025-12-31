package handler

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"theold2api/config"
	"theold2api/proxy"
)

type Handler struct {
	client *proxy.Client
}

func New(client *proxy.Client) *Handler {
	return &Handler{client: client}
}

// ==================== Common Types ====================

// MessageContent can be either a string or an array of content parts (for vision)
type MessageContent struct {
	Text       string        // Used when content is a simple string
	Parts      []ContentPart // Used when content is an array (vision/multimodal)
	IsMultiPart bool         // True if content is array format
}

// ContentPart represents a part of multimodal content
type ContentPart struct {
	Type     string    `json:"type"`               // "text" or "image_url"
	Text     string    `json:"text,omitempty"`     // For type="text"
	ImageURL *ImageURL `json:"image_url,omitempty"` // For type="image_url"
}

// ImageURL represents an image URL in vision requests
type ImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"` // "low", "high", or "auto"
}

// UnmarshalJSON handles both string and array content formats
func (mc *MessageContent) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as string first
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		mc.Text = str
		mc.IsMultiPart = false
		return nil
	}

	// Try to unmarshal as array of content parts
	var parts []ContentPart
	if err := json.Unmarshal(data, &parts); err == nil {
		mc.Parts = parts
		mc.IsMultiPart = true
		return nil
	}

	return fmt.Errorf("content must be string or array of content parts")
}

// MarshalJSON serializes MessageContent back to JSON
func (mc MessageContent) MarshalJSON() ([]byte, error) {
	if mc.IsMultiPart {
		return json.Marshal(mc.Parts)
	}
	return json.Marshal(mc.Text)
}

// GetTextContent extracts text content from the message
func (mc *MessageContent) GetTextContent() string {
	if !mc.IsMultiPart {
		return mc.Text
	}
	var texts []string
	for _, part := range mc.Parts {
		if part.Type == "text" {
			texts = append(texts, part.Text)
		}
	}
	return strings.Join(texts, "\n")
}

type Message struct {
	Role             string         `json:"role"`
	Content          MessageContent `json:"content"`
	ReasoningContent string         `json:"reasoning_content,omitempty"`
}

type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

type ErrorDetail struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code,omitempty"`
}

type Usage struct {
	PromptTokens            int                  `json:"prompt_tokens"`
	CompletionTokens        int                  `json:"completion_tokens"`
	TotalTokens             int                  `json:"total_tokens"`
	CompletionTokensDetails *CompletionTokensDetails `json:"completion_tokens_details,omitempty"`
}

type CompletionTokensDetails struct {
	ReasoningTokens int `json:"reasoning_tokens"`
}

// ==================== Chat Completions Types ====================

type ChatRequest struct {
	Model            string    `json:"model"`
	Messages         []Message `json:"messages"`
	Stream           bool      `json:"stream"`
	Temperature      *float64  `json:"temperature,omitempty"`
	TopP             *float64  `json:"top_p,omitempty"`
	MaxTokens        *int      `json:"max_tokens,omitempty"`
	FrequencyPenalty *float64  `json:"frequency_penalty,omitempty"`
	PresencePenalty  *float64  `json:"presence_penalty,omitempty"`
	ReasoningEffort  *string   `json:"reasoning_effort,omitempty"`
}

type StreamChunk struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   *Usage   `json:"usage,omitempty"`
}

type Choice struct {
	Index        int     `json:"index"`
	Delta        Delta   `json:"delta"`
	FinishReason *string `json:"finish_reason"`
}

type Delta struct {
	Role             string `json:"role,omitempty"`
	Content          string `json:"content,omitempty"`
	ReasoningContent string `json:"reasoning_content,omitempty"`
}

type ChatCompletionResponse struct {
	ID      string                   `json:"id"`
	Object  string                   `json:"object"`
	Created int64                    `json:"created"`
	Model   string                   `json:"model"`
	Choices []ChatCompletionChoice   `json:"choices"`
	Usage   *Usage                   `json:"usage,omitempty"`
}

type ChatCompletionChoice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

// ==================== Responses API Types ====================

type ResponsesRequest struct {
	Model       string                 `json:"model"`
	Input       interface{}            `json:"input"` // string or array
	Reasoning   *ReasoningConfig       `json:"reasoning,omitempty"`
	Temperature *float64               `json:"temperature,omitempty"`
	TopP        *float64               `json:"top_p,omitempty"`
	MaxTokens   *int                   `json:"max_output_tokens,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

type ReasoningConfig struct {
	Effort  string `json:"effort,omitempty"`
	Summary string `json:"summary,omitempty"`
}

type ResponsesResponse struct {
	ID               string                 `json:"id"`
	Object           string                 `json:"object"`
	CreatedAt        int64                  `json:"created_at"`
	Status           string                 `json:"status"`
	Error            interface{}            `json:"error"`
	Model            string                 `json:"model"`
	Output           []ResponseOutput       `json:"output"`
	Usage            *ResponsesUsage        `json:"usage,omitempty"`
	Temperature      float64                `json:"temperature"`
	TopP             float64                `json:"top_p"`
	Metadata         map[string]interface{} `json:"metadata"`
}

type ResponseOutput struct {
	Type    string          `json:"type"`
	ID      string          `json:"id"`
	Status  string          `json:"status"`
	Role    string          `json:"role"`
	Content []OutputContent `json:"content"`
}

type OutputContent struct {
	Type        string        `json:"type"`
	Text        string        `json:"text"`
	Annotations []interface{} `json:"annotations,omitempty"`
}

type ResponsesUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// ==================== Models API Types ====================

type Model struct {
	ID          string `json:"id"`
	Object      string `json:"object"`
	Created     int64  `json:"created"`
	OwnedBy     string `json:"owned_by"`
	APIProvider string `json:"-"` // Internal use only
	Category    string `json:"-"` // Internal use only
	PersonaID   int    `json:"-"` // Internal use only
}

type ModelsResponse struct {
	Object string  `json:"object"`
	Data   []Model `json:"data"`
}

// ==================== Embeddings API Types ====================

type EmbeddingsRequest struct {
	Model          string      `json:"model"`
	Input          interface{} `json:"input"` // string or array
	EncodingFormat string      `json:"encoding_format,omitempty"`
	Dimensions     *int        `json:"dimensions,omitempty"`
}

type EmbeddingsResponse struct {
	Object string           `json:"object"`
	Data   []EmbeddingData  `json:"data"`
	Model  string           `json:"model"`
	Usage  *EmbeddingsUsage `json:"usage"`
}

type EmbeddingData struct {
	Object    string    `json:"object"`
	Embedding []float64 `json:"embedding"`
	Index     int       `json:"index"`
}

type EmbeddingsUsage struct {
	PromptTokens int `json:"prompt_tokens"`
	TotalTokens  int `json:"total_tokens"`
}


// ==================== Chat Completions Handler ====================

func (h *Handler) ChatCompletions(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	requestID := fmt.Sprintf("req_%d", startTime.UnixNano())

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

	var chatReq ChatRequest
	if err := json.Unmarshal(body, &chatReq); err != nil {
		log.Printf("[%s] ERR invalid JSON: %v", requestID, err)
		writeError(w, http.StatusBadRequest, "Invalid JSON", "invalid_request_error")
		return
	}

	// Get model info to determine processing strategy
	model := GetModelByID(chatReq.Model)
	
	// Check if model uses chat session API (ONLY AO category)
	// kO and CO categories should use direct proxy, not ChatSession
	useChatSession := false
	if model != nil && model.Category == "AO" {
		useChatSession = true
	}
	// Note: Removed persona fallback for non-AO models to prevent kO/CO from incorrectly using ChatSession API

	if useChatSession {
		var personaID int

		// Prioritize PersonaID from loaded model config
		if model != nil && model.PersonaID != 0 {
			personaID = model.PersonaID
		} else {
			// Fallback to dynamic lookup
			persona := GetPersonaForModel(chatReq.Model)
			if persona != nil {
				personaID = persona.ID
			}
		}

		if personaID != 0 {
			// LINE 1: Downstream request details (Chat Session)
			log.Printf("[%s] ⇣ REQ (ChatSession) model=%s persona_id=%d msgs=%d stream=%v client=%s",
				requestID, chatReq.Model, personaID, len(chatReq.Messages), chatReq.Stream, r.RemoteAddr)
			h.handleChatSessionRequest(w, r, chatReq, personaID, requestID, startTime)
			return
		}
		// If AO but no persona found, fall through to proxy? Or error?
		// For now, fall through to proxy if persona missing.
	}


	// LINE 1: Downstream request details
	log.Printf("[%s] ⇣ REQ model=%s msgs=%d stream=%v client=%s", 
		requestID, chatReq.Model, len(chatReq.Messages), chatReq.Stream, r.RemoteAddr)

	// Use direct proxy API
	// Always send stream=true to upstream
	var upstreamBody map[string]interface{}
	json.Unmarshal(body, &upstreamBody)
	upstreamBody["stream"] = true
	modifiedBody, _ := json.Marshal(upstreamBody)

	upstreamURL := h.getUpstreamURLForModel(chatReq.Model)
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, upstreamURL, bytes.NewReader(modifiedBody))
	if err != nil {
		log.Printf("[%s] ERR create request: %v", requestID, err)
		writeError(w, http.StatusInternalServerError, "Failed to create request", "server_error")
		return
	}

	h.setUpstreamHeaders(req, model)

	resp, proxyUsed, err := h.client.Do(req)
	if err != nil {
		log.Printf("[%s] ERR proxy=%s: %v", requestID, proxyUsed, err)
		writeError(w, http.StatusBadGateway, "Upstream request failed", "server_error")
		return
	}
	defer resp.Body.Close()

	// LINE 2: Upstream response details
	log.Printf("[%s] ⇡ UP  status=%d proxy=%s", requestID, resp.StatusCode, proxyUsed)

	// Handle upstream error responses
	if resp.StatusCode != http.StatusOK {
		h.handleUpstreamError(w, resp, requestID, startTime)
		return
	}

	if chatReq.Stream {
		h.handleStreamResponse(w, resp, requestID, startTime)
	} else {
		h.handleNonStreamResponse(w, resp, requestID, startTime)
	}
}

// handleChatSessionRequest handles requests using the chat session API
func (h *Handler) handleChatSessionRequest(w http.ResponseWriter, r *http.Request, chatReq ChatRequest, personaID int, requestID string, startTime time.Time) {
	sessionClient := NewChatSessionClient(h.client.HTTPClient())

	// Create chat session
	sessionID, err := sessionClient.CreateSession(r.Context(), personaID, chatReq.Model)
	if err != nil {
		log.Printf("[%s] ✘ ERR create session: %v", requestID, err)
		writeError(w, http.StatusBadGateway, "Failed to create chat session", "server_error")
		return
	}

	log.Printf("[%s] ℹ INF session=%s", requestID, sessionID)

	// Convert messages to single message
	message := ConvertMessagesToSingleMessage(chatReq.Messages)

	// Send message
	resp, err := sessionClient.SendMessage(r.Context(), sessionID, message)
	if err != nil {
		log.Printf("[%s] ✘ ERR send message: %v", requestID, err)
		writeError(w, http.StatusBadGateway, "Failed to send message", "server_error")
		return
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		log.Printf("[%s] ✘ ERR send message status=%d: %s", requestID, resp.StatusCode, string(body))
		writeError(w, resp.StatusCode, fmt.Sprintf("Chat session error: %s", string(body)), "upstream_error")
		return
	}

	// LINE 2: Upstream response details
	log.Printf("[%s] ⇡ UP  status=%d type=chatsession", requestID, resp.StatusCode)

	// Handle streaming response
	HandleChatSessionStream(w, resp, chatReq.Model, requestID, startTime, chatReq.Stream)
}

func (h *Handler) setUpstreamHeaders(req *http.Request, model *Model) {
	// Get dynamically generated API key
	apiKey := config.GetUpstreamAPIKey()

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", proxy.RandomAcceptLanguage())
	req.Header.Set("User-Agent", proxy.RandomUserAgent())
	req.Header.Set("sec-ch-ua", proxy.RandomSecChUa())
	req.Header.Set("sec-ch-ua-mobile", proxy.RandomSecChUaMobile())
	req.Header.Set("sec-ch-ua-platform", proxy.RandomSecChUaPlatform())
	req.Header.Set("sec-fetch-dest", "empty")
	req.Header.Set("sec-fetch-mode", "cors")
	req.Header.Set("sec-fetch-site", "same-origin")
	req.Header.Set("Priority", "u=1, i") // Fixed priority as per apiprovider.md
	req.Header.Set("Referer", "https://theoldllm.vercel.app/")
	req.Header.Set("Origin", "https://theoldllm.vercel.app")

	// API Key handling:
	// - AO: Handled by session client (different flow)
	// - kO/CO: Do NOT send authentication headers (as per apiprovider.md)
	// - Unknown/Default: Send authentication headers safety fallack
	
	shouldSendAuth := true
	if model != nil {
		if model.Category == "kO" || model.Category == "CO" {
			shouldSendAuth = false
		}
	}

	if shouldSendAuth && apiKey != "" {
		req.Header.Set("apikey", apiKey)
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
}

func (h *Handler) handleUpstreamError(w http.ResponseWriter, resp *http.Response, requestID string, startTime time.Time) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[%s] ✘ ERR read body: %v | status=%d", requestID, err, resp.StatusCode)
		writeError(w, resp.StatusCode, fmt.Sprintf("Upstream error (status %d)", resp.StatusCode), "upstream_error")
		return
	}

	// Try to parse upstream error as JSON
	var upstreamErr map[string]interface{}
	if err := json.Unmarshal(body, &upstreamErr); err == nil {
		// Extract error message from common formats
		errMsg := extractErrorMessage(upstreamErr)
		log.Printf("[%s] ✘ ERR msg=%s | status=%d duration=%v", requestID, errMsg, resp.StatusCode, time.Since(startTime))

		// Forward the upstream error response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(body)
		return
	}

	// Plain text error
	errText := string(body)
	if len(errText) > 200 {
		errText = errText[:200] + "..."
	}
	log.Printf("[%s] ✘ ERR msg=%s | status=%d duration=%v", requestID, errText, resp.StatusCode, time.Since(startTime))

	writeError(w, resp.StatusCode, fmt.Sprintf("Upstream error: %s", errText), "upstream_error")
}

func extractErrorMessage(data map[string]interface{}) string {
	// Try common error formats
	if errObj, ok := data["error"].(map[string]interface{}); ok {
		if msg, ok := errObj["message"].(string); ok {
			return msg
		}
	}
	if msg, ok := data["message"].(string); ok {
		return msg
	}
	if msg, ok := data["error"].(string); ok {
		return msg
	}
	if detail, ok := data["detail"].(string); ok {
		return detail
	}
	return "Unknown error"
}

func (h *Handler) getUpstreamURLForModel(modelID string) string {
	baseURL := h.client.UpstreamURL()
	model := GetModelByID(modelID)
	if model == nil || model.APIProvider == "" {
		return baseURL
	}

	u, err := url.Parse(baseURL)
	if err != nil {
		return baseURL
	}

	q := u.Query()
	q.Set("provider", model.APIProvider)
	u.RawQuery = q.Encode()

	return u.String()
}

func (h *Handler) handleStreamResponse(w http.ResponseWriter, resp *http.Response, requestID string, startTime time.Time) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		log.Printf("[%s] ✘ ERR streaming not supported", requestID)
		writeError(w, http.StatusInternalServerError, "Streaming not supported", "server_error")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(resp.StatusCode)

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	chunkCount := 0
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		w.Write([]byte(line + "\n\n"))
		flusher.Flush()
		chunkCount++
	}

	if err := scanner.Err(); err != nil {
		log.Printf("[%s] ✘ ERR chunks=%d duration=%v | %v", requestID, chunkCount, time.Since(startTime), err)
		return
	}

	// LINE 3: Summary
	if chunkCount == 0 {
		log.Printf("[%s] ⚠ WARN chunks=0 duration=%v | no data received", requestID, time.Since(startTime))
	} else {
		log.Printf("[%s] ✔ OK  chunks=%d duration=%v", requestID, chunkCount, time.Since(startTime))
	}
}

func (h *Handler) handleNonStreamResponse(w http.ResponseWriter, resp *http.Response, requestID string, startTime time.Time) {
	var id, model string
	var content strings.Builder
	var reasoningContent strings.Builder
	var finishReason string
	var usage *Usage

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	chunkCount := 0
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || line == "data: [DONE]" {
			continue
		}

		if strings.HasPrefix(line, "data: ") {
			line = strings.TrimPrefix(line, "data: ")
		}

		var chunk StreamChunk
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			continue
		}

		if id == "" {
			id = chunk.ID
		}
		if model == "" {
			model = chunk.Model
		}

		for _, choice := range chunk.Choices {
			content.WriteString(choice.Delta.Content)
			reasoningContent.WriteString(choice.Delta.ReasoningContent)
			if choice.FinishReason != nil {
				finishReason = *choice.FinishReason
			}
		}

		if chunk.Usage != nil {
			usage = chunk.Usage
		}
		chunkCount++
	}

	if err := scanner.Err(); err != nil {
		log.Printf("[%s] ✘ ERR read stream: %v", requestID, err)
		writeError(w, http.StatusBadGateway, "Failed to read upstream response", "server_error")
		return
	}

	msg := Message{
		Role:    "assistant",
		Content: MessageContent{Text: content.String(), IsMultiPart: false},
	}
	if reasoningContent.Len() > 0 {
		msg.ReasoningContent = reasoningContent.String()
	}

	response := ChatCompletionResponse{
		ID:      id,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []ChatCompletionChoice{
			{
				Index:        0,
				Message:      msg,
				FinishReason: finishReason,
			},
		},
		Usage: usage,
	}

	contentLen := len(content.String())
	reasoningLen := reasoningContent.Len()
	tokens := 0
	if usage != nil {
		tokens = usage.TotalTokens
	}
	// LINE 3: Summary
	if contentLen == 0 && reasoningLen == 0 {
		log.Printf("[%s] ⚠ WARN content=0 reasoning=0 tokens=%d duration=%v | no data received",
			requestID, tokens, time.Since(startTime))
	} else {
		log.Printf("[%s] ✔ OK  content=%d reasoning=%d tokens=%d duration=%v",
			requestID, contentLen, reasoningLen, tokens, time.Since(startTime))
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// ==================== Responses API Handler ====================

func (h *Handler) Responses(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	requestID := fmt.Sprintf("req_%d", startTime.UnixNano())

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

	var respReq ResponsesRequest
	if err := json.Unmarshal(body, &respReq); err != nil {
		log.Printf("[%s] ERR invalid JSON: %v", requestID, err)
		writeError(w, http.StatusBadRequest, "Invalid JSON", "invalid_request_error")
		return
	}

	// Convert input to string
	var inputStr string
	switch v := respReq.Input.(type) {
	case string:
		inputStr = v
	case []interface{}:
		// Handle array of messages or strings
		var parts []string
		for _, item := range v {
			if s, ok := item.(string); ok {
				parts = append(parts, s)
			} else if m, ok := item.(map[string]interface{}); ok {
				if content, ok := m["content"].(string); ok {
					parts = append(parts, content)
				}
			}
		}
		inputStr = strings.Join(parts, "\n")
	default:
		inputStr = fmt.Sprintf("%v", v)
	}

	// Convert to chat completions format
	chatBody := map[string]interface{}{
		"model": respReq.Model,
		"messages": []map[string]string{
			{"role": "user", "content": inputStr},
		},
		"stream": true,
	}
	if respReq.Temperature != nil {
		chatBody["temperature"] = *respReq.Temperature
	}
	if respReq.TopP != nil {
		chatBody["top_p"] = *respReq.TopP
	}
	if respReq.MaxTokens != nil {
		chatBody["max_tokens"] = *respReq.MaxTokens
	}

	modifiedBody, _ := json.Marshal(chatBody)

	upstreamURL := h.getUpstreamURLForModel(respReq.Model)
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, upstreamURL, bytes.NewReader(modifiedBody))
	if err != nil {
		log.Printf("[%s] ERR create request: %v", requestID, err)
		writeError(w, http.StatusInternalServerError, "Failed to create request", "server_error")
		return
	}

	// LINE 1: Downstream request details (Responses API)
	log.Printf("[%s] ⇣ REQ (Responses) model=%s client=%s", requestID, respReq.Model, r.RemoteAddr)

	// Get model for header configuration
	model := GetModelByID(respReq.Model)
	h.setUpstreamHeaders(req, model)

	resp, proxyUsed, err := h.client.Do(req)
	if err != nil {
		log.Printf("[%s] ✘ ERR proxy request failed: %v", requestID, err)
		writeError(w, http.StatusBadGateway, "Upstream request failed", "server_error")
		return
	}
	defer resp.Body.Close()

	// LINE 2: Upstream response details
	log.Printf("[%s] ⇡ UP  status=%d proxy=%s", requestID, resp.StatusCode, proxyUsed)

	// Handle upstream error responses
	if resp.StatusCode != http.StatusOK {
		h.handleUpstreamError(w, resp, requestID, startTime)
		return
	}

	// Aggregate streaming response
	var id, responseModel string
	var content strings.Builder
	var usage *Usage

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || line == "data: [DONE]" {
			continue
		}

		if strings.HasPrefix(line, "data: ") {
			line = strings.TrimPrefix(line, "data: ")
		}

		var chunk StreamChunk
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			continue
		}

		if id == "" {
			id = chunk.ID
		}
		if responseModel == "" {
			responseModel = chunk.Model
		}

		for _, choice := range chunk.Choices {
			content.WriteString(choice.Delta.Content)
		}

		if chunk.Usage != nil {
			usage = chunk.Usage
		}
	}

	now := time.Now().Unix()
	response := ResponsesResponse{
		ID:        fmt.Sprintf("resp_%s", id),
		Object:    "response",
		CreatedAt: now,
		Status:    "completed",
		Error:     nil,
		Model:     responseModel,
		Output: []ResponseOutput{
			{
				Type:   "message",
				ID:     fmt.Sprintf("msg_%s", id),
				Status: "completed",
				Role:   "assistant",
				Content: []OutputContent{
					{
						Type:        "output_text",
						Text:        content.String(),
						Annotations: []interface{}{},
					},
				},
			},
		},
		Temperature: 1.0,
		TopP:        1.0,
		Metadata:    respReq.Metadata,
	}

	if usage != nil {
		response.Usage = &ResponsesUsage{
			InputTokens:  usage.PromptTokens,
			OutputTokens: usage.CompletionTokens,
			TotalTokens:  usage.TotalTokens,
		}
	}

	contentLen := len(content.String())
	tokens := 0
	if usage != nil {
		tokens = usage.TotalTokens
	}
	// LINE 3: Summary
	log.Printf("[%s] ✔ OK  content=%d tokens=%d duration=%v",
		requestID, contentLen, tokens, time.Since(startTime))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// ==================== Models API Handler ====================

func (h *Handler) Models(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed", "invalid_request_error")
		return
	}

	models := GetAvailableModels()
	log.Printf("[Models] List models request from %s, returning %d models", r.RemoteAddr, len(models))

	response := ModelsResponse{
		Object: "list",
		Data:   models,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (h *Handler) GetModel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed", "invalid_request_error")
		return
	}

	// Extract model ID from path: /v1/models/{model_id}
	path := r.URL.Path
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) < 3 {
		writeError(w, http.StatusBadRequest, "Model ID required", "invalid_request_error")
		return
	}
	modelID := parts[2]

	log.Printf("[GetModel] Get model request: model=%s, client=%s", modelID, r.RemoteAddr)

	model := GetModelByID(modelID)
	if model != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(model)
		return
	}

	log.Printf("[GetModel] Model not found: %s", modelID)
	writeError(w, http.StatusNotFound, fmt.Sprintf("Model '%s' not found", modelID), "invalid_request_error")
}

// ==================== Embeddings API Handler ====================

func (h *Handler) Embeddings(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed", "invalid_request_error")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Failed to read request body", "invalid_request_error")
		return
	}
	defer r.Body.Close()

	var embReq EmbeddingsRequest
	if err := json.Unmarshal(body, &embReq); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON", "invalid_request_error")
		return
	}

	log.Printf("[Embeddings] Request: model=%s, client=%s", embReq.Model, r.RemoteAddr)

	// Generate mock embeddings (upstream doesn't support embeddings)
	// In production, you would proxy to a real embeddings service
	var inputs []string
	switch v := embReq.Input.(type) {
	case string:
		inputs = []string{v}
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok {
				inputs = append(inputs, s)
			}
		}
	}

	dimensions := 1536 // Default for ada-002
	if embReq.Dimensions != nil {
		dimensions = *embReq.Dimensions
	}

	var data []EmbeddingData
	totalTokens := 0
	for i, input := range inputs {
		// Generate deterministic mock embedding based on input hash
		embedding := make([]float64, dimensions)
		hash := 0
		for _, c := range input {
			hash = (hash*31 + int(c)) % 1000000
		}
		for j := 0; j < dimensions; j++ {
			hash = (hash*1103515245 + 12345) % (1 << 31)
			embedding[j] = float64(hash%2000-1000) / 10000.0
		}

		data = append(data, EmbeddingData{
			Object:    "embedding",
			Embedding: embedding,
			Index:     i,
		})
		totalTokens += len(strings.Fields(input)) + 1
	}

	response := EmbeddingsResponse{
		Object: "list",
		Data:   data,
		Model:  embReq.Model,
		Usage: &EmbeddingsUsage{
			PromptTokens: totalTokens,
			TotalTokens:  totalTokens,
		},
	}

	log.Printf("[Embeddings] Completed: model=%s, inputs=%d, dimensions=%d, tokens=%d, duration=%v",
		embReq.Model, len(inputs), dimensions, totalTokens, time.Since(startTime))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// ==================== Health Check Handler ====================

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}

// ==================== Helper Functions ====================

func writeError(w http.ResponseWriter, statusCode int, message, errType string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(ErrorResponse{
		Error: ErrorDetail{
			Message: message,
			Type:    errType,
		},
	})
}

package handler

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"theold2api/config"
	"theold2api/proxy"
)

// ==================== Persona Types ====================

const (
	// Note: These endpoints require user session authentication
	// If authentication fails, persona mapping will not be available
	personaAPIURL     = "https://theoldllm.vercel.app/sv5/persona"
	createSessionURL  = "https://theoldllm.vercel.app/sv5/chat/create-chat-session"
	sendMessageURL    = "https://theoldllm.vercel.app/sv5/chat/send-message"
	personaRefreshInterval = 5 * time.Minute
)

// Persona represents a persona from the upstream API
type Persona struct {
	ID                      int    `json:"id"`
	Name                    string `json:"name"`
	Description             string `json:"description"`
	LLMModelProviderOverride string `json:"llm_model_provider_override"`
	LLMModelVersionOverride  string `json:"llm_model_version_override"`
	IsPublic                bool   `json:"is_public"`
}

// PersonaCache caches personas and provides model-to-persona mapping
type PersonaCache struct {
	personas        []Persona
	modelToPersona  map[string]*Persona
	lastFetched     time.Time
	mu              sync.RWMutex
	client          *http.Client
	stopCh          chan struct{}
}

var personaCache *PersonaCache

// InitPersonaCache initializes the persona cache
func InitPersonaCache(httpClient *http.Client) {
	personaCache = &PersonaCache{
		client:         httpClient,
		modelToPersona: make(map[string]*Persona),
		stopCh:         make(chan struct{}),
	}

	// Initial fetch
	personaCache.refresh()

	// Start background refresh
	go personaCache.refreshLoop()
}

func (pc *PersonaCache) refreshLoop() {
	ticker := time.NewTicker(personaRefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-pc.stopCh:
			return
		case <-ticker.C:
			pc.refresh()
		}
	}
}

func (pc *PersonaCache) refresh() {
	personas, err := pc.fetchPersonas()
	if err != nil {
		log.Printf("[Persona] Failed to fetch personas: %v", err)
		return
	}

	pc.mu.Lock()
	pc.personas = personas
	pc.modelToPersona = make(map[string]*Persona)
	for i := range personas {
		p := &personas[i]
		if p.LLMModelVersionOverride != "" {
			pc.modelToPersona[p.LLMModelVersionOverride] = p
		}
	}
	pc.lastFetched = time.Now()
	pc.mu.Unlock()

	log.Printf("[Persona] Refreshed %d personas, %d model mappings", len(personas), len(pc.modelToPersona))
}

func (pc *PersonaCache) fetchPersonas() ([]Persona, error) {
	req, err := http.NewRequest(http.MethodGet, personaAPIURL, nil)
	if err != nil {
		return nil, err
	}

	// Get dynamically generated API key
	apiKey := config.GetUpstreamAPIKey()

	// Set headers
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", proxy.RandomAcceptLanguage())
	req.Header.Set("User-Agent", proxy.RandomUserAgent())
	req.Header.Set("sec-ch-ua", proxy.RandomSecChUa())
	req.Header.Set("sec-ch-ua-mobile", proxy.RandomSecChUaMobile())
	req.Header.Set("sec-ch-ua-platform", proxy.RandomSecChUaPlatform())
	req.Header.Set("sec-fetch-dest", "empty")
	req.Header.Set("sec-fetch-mode", "cors")
	req.Header.Set("sec-fetch-site", "same-origin")
	req.Header.Set("Priority", proxy.RandomPriority())
	req.Header.Set("Referer", "https://theoldllm.vercel.app/")
	req.Header.Set("Origin", "https://theoldllm.vercel.app")
	// Supabase authentication headers (dynamically generated)
	if apiKey != "" {
		req.Header.Set("apikey", apiKey)
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := pc.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var personas []Persona
	if err := json.Unmarshal(body, &personas); err != nil {
		return nil, err
	}

	return personas, nil
}

// GetPersonaForModel returns the persona for a given model, or nil if not found
func (pc *PersonaCache) GetPersonaForModel(model string) *Persona {
	pc.mu.RLock()
	defer pc.mu.RUnlock()
	return pc.modelToPersona[model]
}

// GetPersonaForModel is the public function to get persona for a model
func GetPersonaForModel(model string) *Persona {
	if personaCache == nil {
		return nil
	}
	return personaCache.GetPersonaForModel(model)
}

// GetModelsFromPersonas derives available models from cached personas
func GetModelsFromPersonas() []Model {
	if personaCache == nil {
		return []Model{}
	}
	return personaCache.getModels()
}

func (pc *PersonaCache) getModels() []Model {
	pc.mu.RLock()
	defer pc.mu.RUnlock()

	var models []Model
	seen := make(map[string]bool)
	now := time.Now().Unix()

	for _, p := range pc.personas {
		if p.LLMModelVersionOverride == "" {
			continue
		}
		if seen[p.LLMModelVersionOverride] {
			continue
		}
		seen[p.LLMModelVersionOverride] = true

		models = append(models, Model{
			ID:      p.LLMModelVersionOverride,
			Object:  "model",
			Created: now,
			OwnedBy: p.LLMModelProviderOverride,
		})
	}
	return models
}

// ==================== Chat Session Types ====================

type CreateSessionRequest struct {
	PersonaID   int    `json:"persona_id"`
	Description string `json:"description"`
}

type CreateSessionResponse struct {
	ChatSessionID string `json:"chat_session_id"`
}

type SendMessageRequest struct {
	ChatSessionID    string                 `json:"chat_session_id"`
	ParentMessageID  *int                   `json:"parent_message_id"`
	Message          string                 `json:"message"`
	FileDescriptors  []interface{}          `json:"file_descriptors"`
	SearchDocIDs     []interface{}          `json:"search_doc_ids"`
	RetrievalOptions map[string]interface{} `json:"retrieval_options"`
}

type SendMessageResponse struct {
	UserMessageID             int `json:"user_message_id"`
	ReservedAssistantMessageID int `json:"reserved_assistant_message_id"`
}

type ChatSessionStreamEvent struct {
	Placement struct {
		TurnIndex    int  `json:"turn_index"`
		TabIndex     int  `json:"tab_index"`
		SubTurnIndex *int `json:"sub_turn_index"`
	} `json:"placement"`
	Obj struct {
		Type           string      `json:"type"`
		Content        string      `json:"content,omitempty"`
		FinalDocuments interface{} `json:"final_documents,omitempty"`
		StopReason     string      `json:"stop_reason,omitempty"`
	} `json:"obj"`
}

// ==================== Chat Session Client ====================

type ChatSessionClient struct {
	httpClient *http.Client
}

func NewChatSessionClient(httpClient *http.Client) *ChatSessionClient {
	return &ChatSessionClient{httpClient: httpClient}
}

// CreateSession creates a new chat session for a persona
func (c *ChatSessionClient) CreateSession(ctx context.Context, personaID int, model string) (string, error) {
	reqBody := CreateSessionRequest{
		PersonaID:   personaID,
		Description: fmt.Sprintf("Streaming chat session using %s", model),
	}

	bodyBytes, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, createSessionURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}

	setSessionHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("create session failed: status %d, body: %s", resp.StatusCode, string(body))
	}

	var result CreateSessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	return result.ChatSessionID, nil
}

// SendMessage sends a message to a chat session and returns the response body for streaming
func (c *ChatSessionClient) SendMessage(ctx context.Context, sessionID string, message string) (*http.Response, error) {
	reqBody := SendMessageRequest{
		ChatSessionID:    sessionID,
		ParentMessageID:  nil,
		Message:          message,
		FileDescriptors:  []interface{}{},
		SearchDocIDs:     []interface{}{},
		RetrievalOptions: map[string]interface{}{},
	}

	bodyBytes, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sendMessageURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}

	setSessionHeaders(req)

	return c.httpClient.Do(req)
}

func setSessionHeaders(req *http.Request) {
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
	req.Header.Set("Priority", proxy.RandomPriority())
	req.Header.Set("Referer", "https://theoldllm.vercel.app/")
	req.Header.Set("Origin", "https://theoldllm.vercel.app")
	// Supabase authentication headers (dynamically generated)
	if apiKey != "" {
		req.Header.Set("apikey", apiKey)
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
}

// ==================== Chat Session Response Handler ====================

// HandleChatSessionStream handles streaming response from chat session API
// and converts it to OpenAI-compatible SSE format
func HandleChatSessionStream(w http.ResponseWriter, resp *http.Response, model string, requestID string, startTime time.Time, stream bool) {
	defer resp.Body.Close()

	flusher, ok := w.(http.Flusher)
	if !ok && stream {
		log.Printf("[%s] ✘ ERR streaming not supported", requestID)
		writeError(w, http.StatusInternalServerError, "Streaming not supported", "server_error")
		return
	}

	if stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	id := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	created := time.Now().Unix()
	chunkCount := 0
	var contentBuilder strings.Builder
	firstChunk := true

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		// First line is the SendMessageResponse JSON
		if firstChunk {
			var msgResp SendMessageResponse
			if err := json.Unmarshal([]byte(line), &msgResp); err == nil {
				firstChunk = false
				continue
			}
			firstChunk = false
		}

		// Parse chat session event
		var event ChatSessionStreamEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}

		// Handle different event types
		switch event.Obj.Type {
		case "message_start":
			// Send initial chunk with role
			if stream {
				chunk := StreamChunk{
					ID:      id,
					Object:  "chat.completion.chunk",
					Created: created,
					Model:   model,
					Choices: []Choice{
						{
							Index: 0,
							Delta: Delta{Role: "assistant"},
						},
					},
				}
				data, _ := json.Marshal(chunk)
				w.Write([]byte("data: " + string(data) + "\n\n"))
				flusher.Flush()
			}

		case "message_delta":
			content := event.Obj.Content
			contentBuilder.WriteString(content)
			chunkCount++

			if stream {
				chunk := StreamChunk{
					ID:      id,
					Object:  "chat.completion.chunk",
					Created: created,
					Model:   model,
					Choices: []Choice{
						{
							Index: 0,
							Delta: Delta{Content: content},
						},
					},
				}
				data, _ := json.Marshal(chunk)
				w.Write([]byte("data: " + string(data) + "\n\n"))
				flusher.Flush()
			}

		case "stop":
			// Send final chunk with finish_reason
			if stream {
				finishReason := "stop"
				chunk := StreamChunk{
					ID:      id,
					Object:  "chat.completion.chunk",
					Created: created,
					Model:   model,
					Choices: []Choice{
						{
							Index:        0,
							Delta:        Delta{},
							FinishReason: &finishReason,
						},
					},
				}
				data, _ := json.Marshal(chunk)
				w.Write([]byte("data: " + string(data) + "\n\n"))
				w.Write([]byte("data: [DONE]\n\n"))
				flusher.Flush()
			}
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("[%s] ✘ ERR chunks=%d duration=%v | %v", requestID, chunkCount, time.Since(startTime), err)
		return
	}

	// If not streaming, return complete response
	if !stream {
		response := ChatCompletionResponse{
			ID:      id,
			Object:  "chat.completion",
			Created: created,
			Model:   model,
			Choices: []ChatCompletionChoice{
				{
					Index: 0,
					Message: Message{
						Role:    "assistant",
						Content: MessageContent{Text: contentBuilder.String(), IsMultiPart: false},
					},
					FinishReason: "stop",
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}

	// LINE 3: Summary
	contentLen := contentBuilder.Len()
	if chunkCount == 0 && contentLen == 0 {
		log.Printf("[%s] ⚠ WARN chunks=0 content=0 duration=%v | no data received",
			requestID, time.Since(startTime))
	} else {
		log.Printf("[%s] ✔ OK  chunks=%d content=%d duration=%v",
			requestID, chunkCount, contentLen, time.Since(startTime))
	}
}

// ConvertMessagesToSingleMessage converts OpenAI messages array to a single message string
func ConvertMessagesToSingleMessage(messages []Message) string {
	var parts []string
	for _, msg := range messages {
		textContent := msg.Content.GetTextContent()
		if msg.Role == "system" {
			parts = append(parts, fmt.Sprintf("[System]: %s", textContent))
		} else if msg.Role == "user" {
			parts = append(parts, fmt.Sprintf("[User]: %s", textContent))
		} else if msg.Role == "assistant" {
			parts = append(parts, fmt.Sprintf("[Assistant]: %s", textContent))
		}
	}
	return strings.Join(parts, "\n\n")
}

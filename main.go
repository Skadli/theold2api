package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"theold2api/config"
	"theold2api/handler"
	"theold2api/middleware"
	"theold2api/proxy"
)

func main() {
	cfg := config.Load()

	client := proxy.NewClient(cfg)

	// Initialize persona cache for chat session API routing (must be before models cache for fallback)
	handler.InitPersonaCache(client.HTTPClient())

	// Initialize models cache
	handler.InitModelsCache()

	h := handler.New(client)

	mux := http.NewServeMux()

	// OpenAI compatible endpoints
	// Chat Completions
	mux.HandleFunc("/v1/chat/completions", h.ChatCompletions)
	mux.HandleFunc("/chat/completions", h.ChatCompletions)

	// Responses API (new OpenAI API)
	mux.HandleFunc("/v1/responses", h.Responses)
	mux.HandleFunc("/responses", h.Responses)

	// Models
	mux.HandleFunc("/v1/models/", h.GetModel) // Must be before /v1/models
	mux.HandleFunc("/v1/models", h.Models)
	mux.HandleFunc("/models", h.Models)

	// Embeddings
	mux.HandleFunc("/v1/embeddings", h.Embeddings)
	mux.HandleFunc("/embeddings", h.Embeddings)

	// Files API
	mux.HandleFunc("/v1/files", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			h.UploadFile(w, r)
		case http.MethodGet:
			h.ListFiles(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/v1/files/", func(w http.ResponseWriter, r *http.Request) {
		// Handle /v1/files/{file_id} and /v1/files/{file_id}/content
		if strings.HasSuffix(r.URL.Path, "/content") {
			h.GetFileContent(w, r)
			return
		}
		switch r.Method {
		case http.MethodGet:
			h.GetFile(w, r)
		case http.MethodDelete:
			h.DeleteFile(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// Audio API
	mux.HandleFunc("/v1/audio/speech", h.Speech)
	mux.HandleFunc("/v1/audio/transcriptions", h.Transcriptions)
	mux.HandleFunc("/v1/audio/translations", h.Translations)

	// Images API
	mux.HandleFunc("/v1/images/generations", h.ImageGenerations)
	mux.HandleFunc("/v1/images/edits", h.ImageEdits)
	mux.HandleFunc("/v1/images/variations", h.ImageVariations)

	// Moderations API
	mux.HandleFunc("/v1/moderations", h.Moderations)

	// Health check
	mux.HandleFunc("/health", h.Health)

	// Root
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"service":"theold2api","version":"2.0.0","status":"running"}`))
			return
		}
		http.NotFound(w, r)
	})

	// Apply middleware chain (order matters: Recovery -> CORS -> Auth -> Logger -> Handler)
	var handler http.Handler = mux
	handler = middleware.Logger(handler)
	handler = middleware.Auth(handler)
	handler = middleware.CORS(handler)
	handler = middleware.Recovery(handler)

	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      handler,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan

		log.Println("Shutting down server...")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			log.Printf("Server shutdown error: %v", err)
		}
	}()

	log.Printf(`
  _______ _                  _     _ ___              _ 
 |__   __| |                | |   | |__ \            (_)
    | |  | |__   ___  ___   | | __| |  ) | __ _ _ __  _ 
    | |  | '_ \ / _ \/ _ \  | |/ _  | / / / _  | '_ \| |
    | |  | | | |  __/ (_) | | | (_| |/ /_| (_| | |_) | |
    |_|  |_| |_|\___|\___/  |_|\__,_|____|\__,_| .__/|_|
                                               | |      
                                               |_|      
Server starting...
Version: 2.0.0
Port: %s
Upstream: %s`, cfg.Port, cfg.UpstreamURL)

	if os.Getenv("API_KEY") != "" {
		log.Printf("Auth: Enabled")
	} else {
		log.Printf("Auth: Disabled")
	}
	log.Printf("OpenAI compatible endpoints:")
	log.Printf("  POST /v1/chat/completions  - Chat completions")
	log.Printf("  POST /v1/responses         - Responses API")
	log.Printf("  POST /v1/embeddings        - Embeddings")
	log.Printf("  GET  /v1/models            - List models")
	log.Printf("  GET  /v1/models/{id}       - Get model")
	log.Printf("  POST /v1/files             - Upload file")
	log.Printf("  GET  /v1/files             - List files")
	log.Printf("  GET  /v1/files/{id}        - Get file")
	log.Printf("  DELETE /v1/files/{id}      - Delete file")
	log.Printf("  GET  /v1/files/{id}/content - Get file content")
	log.Printf("  POST /v1/audio/speech      - Text to speech")
	log.Printf("  POST /v1/audio/transcriptions - Speech to text")
	log.Printf("  POST /v1/audio/translations - Audio translation")
	log.Printf("  POST /v1/images/generations - Image generation")
	log.Printf("  POST /v1/images/edits      - Image editing")
	log.Printf("  POST /v1/images/variations - Image variations")
	log.Printf("  POST /v1/moderations       - Content moderation")

	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}

	log.Println("Server stopped")
}

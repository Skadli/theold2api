package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ==================== Files API Types ====================

type FileObject struct {
	ID        string `json:"id"`
	Object    string `json:"object"`
	Bytes     int64  `json:"bytes"`
	CreatedAt int64  `json:"created_at"`
	Filename  string `json:"filename"`
	Purpose   string `json:"purpose"`
}

type FilesListResponse struct {
	Object string       `json:"object"`
	Data   []FileObject `json:"data"`
}

type FileDeleteResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Deleted bool   `json:"deleted"`
}

// ==================== Files Storage ====================

var (
	filesStore   = make(map[string]*FileObject)
	filesStoreMu sync.RWMutex
)

func generateFileID() string {
	return fmt.Sprintf("file-%d", time.Now().UnixNano())
}

// ==================== Files API Handlers ====================

// UploadFile handles POST /v1/files
// Note: This implementation only logs file info without saving to disk
func (h *Handler) UploadFile(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	requestID := fmt.Sprintf("file_%d", startTime.UnixNano())

	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed", "invalid_request_error")
		return
	}

	// Parse multipart form (max 512MB as per OpenAI spec)
	if err := r.ParseMultipartForm(512 << 20); err != nil {
		log.Printf("[%s] ✘ ERR parse form: %v", requestID, err)
		writeError(w, http.StatusBadRequest, "Failed to parse multipart form", "invalid_request_error")
		return
	}

	// Get purpose
	purpose := r.FormValue("purpose")
	if purpose == "" {
		writeError(w, http.StatusBadRequest, "Missing required parameter: purpose", "invalid_request_error")
		return
	}

	// Validate purpose
	validPurposes := map[string]bool{
		"assistants": true,
		"batch":      true,
		"fine-tune":  true,
		"vision":     true,
		"user_data":  true,
		"evals":      true,
	}
	if !validPurposes[purpose] {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid purpose: %s", purpose), "invalid_request_error")
		return
	}

	// Get file
	file, header, err := r.FormFile("file")
	if err != nil {
		log.Printf("[%s] ✘ ERR get file: %v", requestID, err)
		writeError(w, http.StatusBadRequest, "Missing required parameter: file", "invalid_request_error")
		return
	}
	defer file.Close()

	// Read file content to get size (without saving)
	content, err := io.ReadAll(file)
	if err != nil {
		log.Printf("[%s] ✘ ERR read file: %v", requestID, err)
		writeError(w, http.StatusInternalServerError, "Failed to read file", "server_error")
		return
	}
	fileSize := int64(len(content))

	// Generate file ID
	fileID := generateFileID()

	// Detect content type
	contentType := http.DetectContentType(content)

	// Log detailed file information
	log.Printf("[%s] ⇣ FILE UPLOAD", requestID)
	log.Printf("[%s]   ├─ filename: %s", requestID, header.Filename)
	log.Printf("[%s]   ├─ size: %d bytes (%.2f KB)", requestID, fileSize, float64(fileSize)/1024)
	log.Printf("[%s]   ├─ purpose: %s", requestID, purpose)
	log.Printf("[%s]   ├─ content-type: %s", requestID, contentType)
	log.Printf("[%s]   ├─ client: %s", requestID, r.RemoteAddr)
	log.Printf("[%s]   └─ file_id: %s (generated)", requestID, fileID)

	// Create file object (in-memory only, not persisted)
	fileObj := &FileObject{
		ID:        fileID,
		Object:    "file",
		Bytes:     fileSize,
		CreatedAt: time.Now().Unix(),
		Filename:  header.Filename,
		Purpose:   purpose,
	}

	// Store in memory for subsequent API calls
	filesStoreMu.Lock()
	filesStore[fileID] = fileObj
	filesStoreMu.Unlock()

	log.Printf("[%s] ✔ OK  duration=%v (file not saved to disk)", requestID, time.Since(startTime))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(fileObj)
}

// ListFiles handles GET /v1/files
func (h *Handler) ListFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed", "invalid_request_error")
		return
	}

	// Optional purpose filter
	purposeFilter := r.URL.Query().Get("purpose")

	filesStoreMu.RLock()
	var files []FileObject
	for _, f := range filesStore {
		if purposeFilter == "" || f.Purpose == purposeFilter {
			files = append(files, *f)
		}
	}
	filesStoreMu.RUnlock()

	if files == nil {
		files = []FileObject{}
	}

	log.Printf("[Files] List files: count=%d purpose_filter=%s", len(files), purposeFilter)

	response := FilesListResponse{
		Object: "list",
		Data:   files,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// GetFile handles GET /v1/files/{file_id}
func (h *Handler) GetFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed", "invalid_request_error")
		return
	}

	// Extract file ID from path
	fileID := extractFileID(r.URL.Path)
	if fileID == "" {
		writeError(w, http.StatusBadRequest, "File ID required", "invalid_request_error")
		return
	}

	filesStoreMu.RLock()
	fileObj, exists := filesStore[fileID]
	filesStoreMu.RUnlock()

	if !exists {
		writeError(w, http.StatusNotFound, fmt.Sprintf("No file with id '%s' found", fileID), "invalid_request_error")
		return
	}

	log.Printf("[Files] Get file: id=%s", fileID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(fileObj)
}

// DeleteFile handles DELETE /v1/files/{file_id}
// Note: Only removes from memory since files are not persisted
func (h *Handler) DeleteFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed", "invalid_request_error")
		return
	}

	// Extract file ID from path
	fileID := extractFileID(r.URL.Path)
	if fileID == "" {
		writeError(w, http.StatusBadRequest, "File ID required", "invalid_request_error")
		return
	}

	filesStoreMu.Lock()
	fileObj, exists := filesStore[fileID]
	if exists {
		delete(filesStore, fileID)
	}
	filesStoreMu.Unlock()

	if !exists {
		writeError(w, http.StatusNotFound, fmt.Sprintf("No file with id '%s' found", fileID), "invalid_request_error")
		return
	}

	log.Printf("[Files] Deleted file from memory: id=%s filename=%s", fileID, fileObj.Filename)

	response := FileDeleteResponse{
		ID:      fileID,
		Object:  "file",
		Deleted: true,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// GetFileContent handles GET /v1/files/{file_id}/content
// Note: Since files are not saved to disk, this returns an error
func (h *Handler) GetFileContent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed", "invalid_request_error")
		return
	}

	// Extract file ID from path (remove /content suffix)
	path := strings.TrimSuffix(r.URL.Path, "/content")
	fileID := extractFileID(path)
	if fileID == "" {
		writeError(w, http.StatusBadRequest, "File ID required", "invalid_request_error")
		return
	}

	filesStoreMu.RLock()
	fileObj, exists := filesStore[fileID]
	filesStoreMu.RUnlock()

	if !exists {
		writeError(w, http.StatusNotFound, fmt.Sprintf("No file with id '%s' found", fileID), "invalid_request_error")
		return
	}

	log.Printf("[Files] Get file content requested: id=%s filename=%s (content not available - files not persisted)", fileID, fileObj.Filename)

	// Files are not saved to disk, return error
	writeError(w, http.StatusNotImplemented,
		"File content is not available. This proxy does not persist uploaded files.",
		"not_implemented")
}

// extractFileID extracts file ID from path like /v1/files/{file_id}
func extractFileID(path string) string {
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	// Expected: v1/files/{file_id} or files/{file_id}
	for i, part := range parts {
		if part == "files" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

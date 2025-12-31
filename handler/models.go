package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

// ==================== Models Fetcher ====================

const (
	modelsFilePath        = "models.json"
	modelsRefreshInterval = 5 * time.Minute
)

// InternalModelConfig represents a model entry in models.json
type InternalModelConfig struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Provider    string `json:"provider"`
	APIProvider string `json:"apiProvider"`
	PersonaID   int    `json:"persona_id"`
}

// ModelsConfig represents the structure of models.json
type ModelsConfig struct {
	AO []InternalModelConfig `json:"AO"`
	KO []InternalModelConfig `json:"kO"`
	CO []InternalModelConfig `json:"CO"`
}

type ModelsCache struct {
	models      []Model
	lastFetched time.Time
	mu          sync.RWMutex
	stopCh      chan struct{}
}

var modelsCache *ModelsCache

func InitModelsCache() {
	modelsCache = &ModelsCache{
		stopCh: make(chan struct{}),
	}

	// Initial load
	modelsCache.refresh()

	// Start background refresh
	go modelsCache.refreshLoop()
}

func (mc *ModelsCache) refreshLoop() {
	ticker := time.NewTicker(modelsRefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-mc.stopCh:
			return
		case <-ticker.C:
			mc.refresh()
		}
	}
}

func (mc *ModelsCache) refresh() {
	models, err := mc.loadModels()
	if err != nil {
		log.Printf("[Models] Failed to load models from %s: %v", modelsFilePath, err)
		
		// If we have existing models, keep them
		mc.mu.RLock()
		hasModels := len(mc.models) > 0
		mc.mu.RUnlock()
		
		if hasModels {
			return
		}
		
		// If no models at all, try persona models as last resort fallback
		// (optional based on requirements, but good to keep if file missing)
		personaModels := GetModelsFromPersonas()
		if len(personaModels) > 0 {
			models = personaModels
			log.Printf("[Models] Derived %d models from personas (fallback)", len(models))
		}
	}

	mc.mu.Lock()
	mc.models = models
	mc.lastFetched = time.Now()
	mc.mu.Unlock()

	log.Printf("[Models] Loaded %d models from %s", len(models), modelsFilePath)
}

func (mc *ModelsCache) loadModels() ([]Model, error) {
	data, err := os.ReadFile(modelsFilePath)
	if err != nil {
		return nil, err
	}

	var config ModelsConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("invalid json format: %v", err)
	}

	var models []Model
	now := time.Now().Unix()

	// Helper to add models
	addModels := func(items []InternalModelConfig, category string) {
		for _, item := range items {
			models = append(models, Model{
				ID:          item.ID,
				Object:      "model",
				Created:     now,
				OwnedBy:     item.Provider,
				APIProvider: item.APIProvider,
				Category:    category,
				PersonaID:   item.PersonaID,
			})
		}
	}

	addModels(config.AO, "AO")
	addModels(config.KO, "kO")
	addModels(config.CO, "CO")

	return models, nil
}

func (mc *ModelsCache) GetModels() []Model {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	if len(mc.models) == 0 {
		return getDefaultModels()
	}

	// Return a copy to avoid race conditions
	result := make([]Model, len(mc.models))
	copy(result, mc.models)
	return result
}

func (mc *ModelsCache) GetModel(id string) *Model {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	for _, m := range mc.models {
		if m.ID == id {
			return &Model{
				ID:          m.ID,
				Object:      m.Object,
				Created:     m.Created,
				OwnedBy:     m.OwnedBy,
				APIProvider: m.APIProvider,
				Category:    m.Category,
				PersonaID:   m.PersonaID,
			}
		}
	}
	return nil
}

func (mc *ModelsCache) Stop() {
	close(mc.stopCh)
}

func getDefaultModels() []Model {
	return []Model{}
}

// GetAvailableModels returns the current cached models
func GetAvailableModels() []Model {
	if modelsCache == nil {
		return getDefaultModels()
	}
	return modelsCache.GetModels()
}

// GetModelByID returns a specific model by ID
func GetModelByID(id string) *Model {
	if modelsCache == nil {
		for _, m := range getDefaultModels() {
			if m.ID == id {
				return &m
			}
		}
		return nil
	}
	return modelsCache.GetModel(id)
}

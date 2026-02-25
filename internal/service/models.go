package service

import (
	"context"
	"strings"

	"github.com/stratum/gateway/internal/bedrock"
	"github.com/stratum/gateway/internal/schema"
)

// ModelsService handles model list and lookup operations.
type ModelsService struct {
	models      bedrock.ModelDiscovery
	modelPolicy ModelPolicy
}

// NewModelsService creates a models service.
func NewModelsService(models bedrock.ModelDiscovery, modelPolicy ModelPolicy) *ModelsService {
	return &ModelsService{
		models:      models,
		modelPolicy: modelPolicy,
	}
}

// List returns available models.
func (s *ModelsService) List(ctx context.Context) ([]schema.Model, error) {
	models, err := s.models.GetModels(ctx)
	if err != nil {
		return nil, internal("failed to list models", err)
	}
	if s.modelPolicy == nil || len(models) == 0 {
		return models, nil
	}

	filtered := make([]schema.Model, 0, len(models))
	for _, model := range models {
		if s.modelPolicy.IsBlocked(model.ID) {
			continue
		}
		filtered = append(filtered, model)
	}
	return filtered, nil
}

// Get returns a model by ID.
func (s *ModelsService) Get(ctx context.Context, modelID string) (*schema.Model, error) {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return nil, badRequest("model id is required", nil)
	}
	if s.modelPolicy != nil && s.modelPolicy.IsBlocked(modelID) {
		return nil, notFound("model "+modelID+" not found", nil)
	}

	model, err := s.models.FindModel(ctx, modelID)
	if err != nil {
		return nil, internal("failed to find model", err)
	}
	if model == nil {
		return nil, notFound("model "+modelID+" not found", nil)
	}
	return model, nil
}

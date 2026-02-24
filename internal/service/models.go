package service

import (
	"context"
	"strings"

	"github.com/stratum/gateway/internal/bedrock"
	"github.com/stratum/gateway/internal/schema"
)

// ModelsService handles model list and lookup operations.
type ModelsService struct {
	models bedrock.ModelDiscovery
}

// NewModelsService creates a models service.
func NewModelsService(models bedrock.ModelDiscovery) *ModelsService {
	return &ModelsService{models: models}
}

// List returns available models.
func (s *ModelsService) List(ctx context.Context) ([]schema.Model, error) {
	models, err := s.models.GetModels(ctx)
	if err != nil {
		return nil, internal("failed to list models", err)
	}
	return models, nil
}

// Get returns a model by ID.
func (s *ModelsService) Get(ctx context.Context, modelID string) (*schema.Model, error) {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return nil, badRequest("model id is required", nil)
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

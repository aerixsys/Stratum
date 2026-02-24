package service

import (
	"context"
	"strings"

	"github.com/stratum/gateway/internal/bedrock"
	"github.com/stratum/gateway/internal/schema"
)

// EmbeddingsService handles embedding request orchestration.
type EmbeddingsService struct {
	runtime               bedrock.EmbeddingRuntime
	defaultEmbeddingModel string
}

// NewEmbeddingsService creates an embeddings service.
func NewEmbeddingsService(runtime bedrock.EmbeddingRuntime, defaultEmbeddingModel string) *EmbeddingsService {
	return &EmbeddingsService{
		runtime:               runtime,
		defaultEmbeddingModel: strings.TrimSpace(defaultEmbeddingModel),
	}
}

// Embed runs an embedding request.
func (s *EmbeddingsService) Embed(ctx context.Context, req *schema.EmbeddingRequest) (*schema.EmbeddingResponse, error) {
	if req == nil {
		return nil, badRequest("request body is required", nil)
	}

	normalized := *req
	normalized.Model = strings.TrimSpace(req.Model)
	if normalized.Model == "" {
		if s.defaultEmbeddingModel == "" {
			return nil, badRequest("model is required", nil)
		}
		normalized.Model = s.defaultEmbeddingModel
	}
	if len(normalized.InputStrings()) == 0 {
		return nil, badRequest("input is required", nil)
	}
	resp, err := s.runtime.Embed(ctx, &normalized)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

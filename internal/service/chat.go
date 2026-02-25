package service

import (
	"context"
	"strings"

	"github.com/stratum/gateway/internal/bedrock"
	"github.com/stratum/gateway/internal/schema"
)

// ChatService handles chat request orchestration.
type ChatService struct {
	runtime      bedrock.ChatRuntime
	models       bedrock.ModelDiscovery
	modelPolicy  ModelPolicy
	translateCfg bedrock.TranslateConfig
	defaultModel string
}

// NewChatService constructs a chat service.
func NewChatService(runtime bedrock.ChatRuntime, models bedrock.ModelDiscovery, cfg bedrock.TranslateConfig, defaultModel string, modelPolicy ModelPolicy) *ChatService {
	return &ChatService{
		runtime:      runtime,
		models:       models,
		modelPolicy:  modelPolicy,
		translateCfg: cfg,
		defaultModel: strings.TrimSpace(defaultModel),
	}
}

// Converse runs a non-streaming chat completion.
func (s *ChatService) Converse(ctx context.Context, req *schema.ChatRequest) (*schema.ChatResponse, error) {
	input, err := s.buildInput(ctx, req)
	if err != nil {
		return nil, err
	}
	resp, err := s.runtime.Converse(ctx, input)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// ConverseStream runs a streaming chat completion.
func (s *ChatService) ConverseStream(ctx context.Context, req *schema.ChatRequest) (<-chan []byte, error) {
	input, err := s.buildInput(ctx, req)
	if err != nil {
		return nil, err
	}
	return s.runtime.ConverseStream(ctx, input), nil
}

func (s *ChatService) buildInput(ctx context.Context, req *schema.ChatRequest) (*bedrock.ConverseInput, error) {
	if req == nil {
		return nil, badRequest("request body is required", nil)
	}
	if len(req.Messages) == 0 {
		return nil, badRequest("messages is required", nil)
	}

	normalized := *req
	normalized.Model = strings.TrimSpace(req.Model)
	if normalized.Model == "" {
		if s.defaultModel == "" {
			return nil, badRequest("model is required", nil)
		}
		normalized.Model = s.defaultModel
	}
	if s.modelPolicy != nil && s.modelPolicy.IsBlocked(normalized.Model) {
		return nil, badRequest("model "+normalized.Model+" is blocked by policy", nil)
	}

	if s.models != nil {
		model, err := s.models.FindModel(ctx, normalized.Model)
		if err != nil {
			return nil, internal("failed to validate model", err)
		}
		if model == nil {
			return nil, badRequest("unsupported model "+normalized.Model, nil)
		}
	}

	input, err := bedrock.TranslateRequest(&normalized, s.translateCfg)
	if err != nil {
		return nil, badRequest("invalid request for model translation", err)
	}
	return input, nil
}

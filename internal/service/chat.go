package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/stratum/gateway/internal/bedrock"
	"github.com/stratum/gateway/internal/schema"
)

// ChatService handles chat request orchestration.
type ChatService struct {
	runtime     bedrock.ChatRuntime
	models      bedrock.ModelDiscovery
	modelPolicy ModelPolicy
}

// NewChatService constructs a chat service.
func NewChatService(runtime bedrock.ChatRuntime, models bedrock.ModelDiscovery, modelPolicy ModelPolicy) *ChatService {
	return &ChatService{
		runtime:     runtime,
		models:      models,
		modelPolicy: modelPolicy,
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
	if strings.TrimSpace(req.ReasoningEffort) != "" {
		return nil, badRequest("reasoning_effort is not supported; use reasoning.exclude and extra_body.additional_model_request_fields", nil)
	}
	if req.Reasoning != nil && req.Reasoning.HasUnsupportedControls() {
		return nil, badRequest(
			fmt.Sprintf(
				"unsupported top-level reasoning controls (%s); only reasoning.exclude is supported. Use extra_body.additional_model_request_fields for provider-specific reasoning controls",
				strings.Join(req.Reasoning.UnsupportedControls(), ", "),
			),
			nil,
		)
	}
	if err := req.ValidateExtraBodyCoreOnly(); err != nil {
		return nil, badRequest(err.Error(), nil)
	}

	normalized := *req
	normalized.Model = strings.TrimSpace(req.Model)
	if normalized.Model == "" {
		return nil, badRequest("model is required", nil)
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

	input, err := bedrock.TranslateRequest(&normalized)
	if err != nil {
		return nil, badRequest("invalid request for model translation", err)
	}
	return input, nil
}

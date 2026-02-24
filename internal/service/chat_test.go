package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stratum/gateway/internal/bedrock"
	"github.com/stratum/gateway/internal/schema"
)

type fakeChatRuntime struct {
	lastInput *bedrock.ConverseInput
	resp      *schema.ChatResponse
	err       error
}

func (f *fakeChatRuntime) Converse(ctx context.Context, input *bedrock.ConverseInput) (*schema.ChatResponse, error) {
	f.lastInput = input
	return f.resp, f.err
}

func (f *fakeChatRuntime) ConverseStream(ctx context.Context, input *bedrock.ConverseInput) <-chan []byte {
	f.lastInput = input
	ch := make(chan []byte, 1)
	ch <- []byte("data: [DONE]\n\n")
	close(ch)
	return ch
}

type fakeModels struct {
	models map[string]schema.Model
	err    error
}

func (f *fakeModels) GetModels(ctx context.Context) ([]schema.Model, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make([]schema.Model, 0, len(f.models))
	for _, m := range f.models {
		out = append(out, m)
	}
	return out, nil
}

func (f *fakeModels) FindModel(ctx context.Context, modelID string) (*schema.Model, error) {
	if f.err != nil {
		return nil, f.err
	}
	m, ok := f.models[modelID]
	if !ok {
		return nil, nil
	}
	return &m, nil
}

func TestChatService_DefaultModelFallback(t *testing.T) {
	rt := &fakeChatRuntime{resp: &schema.ChatResponse{Object: "chat.completion"}}
	models := &fakeModels{
		models: map[string]schema.Model{
			"anthropic.claude-3-sonnet": {ID: "anthropic.claude-3-sonnet"},
		},
	}
	svc := NewChatService(rt, models, bedrock.TranslateConfig{}, "anthropic.claude-3-sonnet")

	req := &schema.ChatRequest{
		Messages: []schema.Message{
			{Role: "user", Content: json.RawMessage(`"hello"`)},
		},
	}
	_, err := svc.Converse(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rt.lastInput == nil {
		t.Fatalf("expected runtime input")
	}
	if rt.lastInput.ModelID != "anthropic.claude-3-sonnet" {
		t.Fatalf("expected fallback model, got %s", rt.lastInput.ModelID)
	}
}

func TestChatService_RejectUnsupportedModel(t *testing.T) {
	rt := &fakeChatRuntime{}
	models := &fakeModels{models: map[string]schema.Model{}}
	svc := NewChatService(rt, models, bedrock.TranslateConfig{}, "")

	req := &schema.ChatRequest{
		Model: "missing-model",
		Messages: []schema.Message{
			{Role: "user", Content: json.RawMessage(`"hello"`)},
		},
	}
	_, err := svc.Converse(context.Background(), req)
	if err == nil {
		t.Fatalf("expected error for unsupported model")
	}
	var svcErr *Error
	if !errors.As(err, &svcErr) {
		t.Fatalf("expected service error, got %T", err)
	}
	if svcErr.Kind != ErrorBadRequest {
		t.Fatalf("expected bad request, got %s", svcErr.Kind)
	}
}

func TestChatService_TranslatesStreamIncludeUsage(t *testing.T) {
	rt := &fakeChatRuntime{}
	models := &fakeModels{
		models: map[string]schema.Model{
			"anthropic.claude-3-sonnet": {ID: "anthropic.claude-3-sonnet"},
		},
	}
	svc := NewChatService(rt, models, bedrock.TranslateConfig{}, "")
	req := &schema.ChatRequest{
		Model: "anthropic.claude-3-sonnet",
		Messages: []schema.Message{
			{Role: "user", Content: json.RawMessage(`"hello"`)},
		},
		Stream:        true,
		StreamOptions: &schema.StreamOptions{IncludeUsage: true},
		Temperature:   float32Ptr(0.7),
		TopP:          float32Ptr(0.8),
		Stop:          []string{"stop"},
	}
	_, err := svc.ConverseStream(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rt.lastInput == nil {
		t.Fatal("expected runtime input")
	}
	if !rt.lastInput.IncludeUsage {
		t.Fatal("expected include usage to be true")
	}
	if rt.lastInput.InferenceConfig == nil {
		t.Fatal("expected inference config")
	}
	if rt.lastInput.InferenceConfig.StopSequences[0] != "stop" {
		t.Fatalf("unexpected stop sequence")
	}
}

func TestChatService_NilRequest(t *testing.T) {
	svc := NewChatService(&fakeChatRuntime{}, nil, bedrock.TranslateConfig{}, "")
	_, err := svc.Converse(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error")
	}
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Kind != ErrorBadRequest {
		t.Fatalf("expected bad request service error, got %v", err)
	}
}

func TestChatService_ModelRequiredWithoutFallback(t *testing.T) {
	svc := NewChatService(&fakeChatRuntime{}, nil, bedrock.TranslateConfig{}, "")
	_, err := svc.Converse(context.Background(), &schema.ChatRequest{
		Messages: []schema.Message{{Role: "user", Content: json.RawMessage(`"hello"`)}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Kind != ErrorBadRequest {
		t.Fatalf("expected bad request service error, got %v", err)
	}
}

func TestChatService_ModelLookupError(t *testing.T) {
	svc := NewChatService(&fakeChatRuntime{}, &fakeModels{err: errors.New("lookup failed")}, bedrock.TranslateConfig{}, "")
	_, err := svc.Converse(context.Background(), &schema.ChatRequest{
		Model:    "m1",
		Messages: []schema.Message{{Role: "user", Content: json.RawMessage(`"hello"`)}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Kind != ErrorInternal {
		t.Fatalf("expected internal service error, got %v", err)
	}
}

func TestChatService_TranslateError(t *testing.T) {
	models := &fakeModels{models: map[string]schema.Model{"m1": {ID: "m1"}}}
	svc := NewChatService(&fakeChatRuntime{}, models, bedrock.TranslateConfig{}, "")
	_, err := svc.Converse(context.Background(), &schema.ChatRequest{
		Model:    "m1",
		Messages: []schema.Message{{Role: "user", Content: json.RawMessage(`"hello"`)}},
		ExtraBody: json.RawMessage(`{
			"additional_model_response_field_paths": ["bad-path"]
		}`),
	})
	if err == nil {
		t.Fatal("expected error")
	}
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Kind != ErrorBadRequest {
		t.Fatalf("expected bad request service error, got %v", err)
	}
}

func TestChatService_RuntimeErrorPassThrough(t *testing.T) {
	rt := &fakeChatRuntime{err: errors.New("upstream failed")}
	models := &fakeModels{models: map[string]schema.Model{"m1": {ID: "m1"}}}
	svc := NewChatService(rt, models, bedrock.TranslateConfig{}, "")
	_, err := svc.Converse(context.Background(), &schema.ChatRequest{
		Model:    "m1",
		Messages: []schema.Message{{Role: "user", Content: json.RawMessage(`"hello"`)}},
	})
	if err == nil || err.Error() != "upstream failed" {
		t.Fatalf("expected runtime error passthrough, got %v", err)
	}
}

func float32Ptr(v float32) *float32 { return &v }

var _ bedrock.ChatRuntime = (*fakeChatRuntime)(nil)
var _ bedrock.ModelDiscovery = (*fakeModels)(nil)

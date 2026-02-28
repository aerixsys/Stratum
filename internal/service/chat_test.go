package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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

func TestChatService_RejectUnsupportedModel(t *testing.T) {
	rt := &fakeChatRuntime{}
	models := &fakeModels{models: map[string]schema.Model{}}
	svc := NewChatService(rt, models, nil)

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
	svc := NewChatService(rt, models, nil)
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
	svc := NewChatService(&fakeChatRuntime{}, nil, nil)
	_, err := svc.Converse(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error")
	}
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Kind != ErrorBadRequest {
		t.Fatalf("expected bad request service error, got %v", err)
	}
}

func TestChatService_ModelRequired(t *testing.T) {
	svc := NewChatService(&fakeChatRuntime{}, nil, nil)
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
	if svcErr.Message != "model is required" {
		t.Fatalf("expected model required message, got %q", svcErr.Message)
	}
}

func TestChatService_ModelRequiredWhitespace(t *testing.T) {
	svc := NewChatService(&fakeChatRuntime{}, nil, nil)
	_, err := svc.Converse(context.Background(), &schema.ChatRequest{
		Model:    "   ",
		Messages: []schema.Message{{Role: "user", Content: json.RawMessage(`"hello"`)}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Kind != ErrorBadRequest {
		t.Fatalf("expected bad request service error, got %v", err)
	}
	if !strings.Contains(svcErr.Message, "model is required") {
		t.Fatalf("expected model required message, got %q", svcErr.Message)
	}
}

func TestChatService_RejectsLegacyReasoningEffort(t *testing.T) {
	svc := NewChatService(&fakeChatRuntime{}, nil, nil)
	_, err := svc.Converse(context.Background(), &schema.ChatRequest{
		Model:           "m1",
		ReasoningEffort: "medium",
		Messages:        []schema.Message{{Role: "user", Content: json.RawMessage(`"hello"`)}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Kind != ErrorBadRequest {
		t.Fatalf("expected bad request service error, got %v", err)
	}
	if svcErr.Message != "reasoning_effort is not supported; use reasoning.exclude and extra_body.additional_model_request_fields" {
		t.Fatalf("unexpected error message: %q", svcErr.Message)
	}
}

func TestChatService_RejectsTopLevelReasoningControlsBeyondExclude(t *testing.T) {
	var req schema.ChatRequest
	if err := json.Unmarshal([]byte(`{
		"model":"m1",
		"messages":[{"role":"user","content":"hello"}],
		"reasoning":{"exclude":true,"enabled":true}
	}`), &req); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}

	svc := NewChatService(&fakeChatRuntime{}, nil, nil)
	_, err := svc.Converse(context.Background(), &req)
	if err == nil {
		t.Fatal("expected error")
	}
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Kind != ErrorBadRequest {
		t.Fatalf("expected bad request service error, got %v", err)
	}
	if !strings.Contains(svcErr.Message, "only reasoning.exclude is supported") {
		t.Fatalf("unexpected error message: %q", svcErr.Message)
	}
	if !strings.Contains(svcErr.Message, "extra_body.additional_model_request_fields") {
		t.Fatalf("unexpected error message: %q", svcErr.Message)
	}
}

func TestChatService_RejectsInvalidMessageShape(t *testing.T) {
	svc := NewChatService(&fakeChatRuntime{}, nil, nil)
	_, err := svc.Converse(context.Background(), &schema.ChatRequest{
		Model: "m1",
		Messages: []schema.Message{
			{Role: "moderator", Content: json.RawMessage(`"hello"`)},
		},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Kind != ErrorBadRequest {
		t.Fatalf("expected bad request service error, got %v", err)
	}
	if svcErr.Message != `messages[0].role "moderator" is not supported` {
		t.Fatalf("unexpected error message: %q", svcErr.Message)
	}
}

func TestChatService_ModelLookupError(t *testing.T) {
	svc := NewChatService(&fakeChatRuntime{}, &fakeModels{err: errors.New("lookup failed")}, nil)
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
	svc := NewChatService(&fakeChatRuntime{}, models, nil)
	_, err := svc.Converse(context.Background(), &schema.ChatRequest{
		Model:    "m1",
		Messages: []schema.Message{{Role: "user", Content: json.RawMessage(`"hello"`)}},
		ExtraBody: json.RawMessage(`{
			"guardrail_config": {"guardrail_identifier":"gr-1","guardrail_version":"1"}
		}`),
	})
	if err == nil {
		t.Fatal("expected error")
	}
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Kind != ErrorBadRequest {
		t.Fatalf("expected bad request service error, got %v", err)
	}
	if !strings.Contains(svcErr.Message, "unsupported extra_body fields") {
		t.Fatalf("unexpected error message: %q", svcErr.Message)
	}
}

func TestChatService_RuntimeErrorPassThrough(t *testing.T) {
	rt := &fakeChatRuntime{err: errors.New("upstream failed")}
	models := &fakeModels{models: map[string]schema.Model{"m1": {ID: "m1"}}}
	svc := NewChatService(rt, models, nil)
	_, err := svc.Converse(context.Background(), &schema.ChatRequest{
		Model:    "m1",
		Messages: []schema.Message{{Role: "user", Content: json.RawMessage(`"hello"`)}},
	})
	if err == nil || err.Error() != "upstream failed" {
		t.Fatalf("expected runtime error passthrough, got %v", err)
	}
}

func TestChatService_BlockedModel(t *testing.T) {
	rt := &fakeChatRuntime{}
	models := &fakeModels{
		models: map[string]schema.Model{
			"anthropic.claude-3-sonnet": {ID: "anthropic.claude-3-sonnet"},
		},
	}
	p := &testModelPolicy{
		blocked: map[string]bool{
			"anthropic.claude-3-sonnet": true,
		},
	}
	svc := NewChatService(rt, models, p)

	_, err := svc.Converse(context.Background(), &schema.ChatRequest{
		Model:    "anthropic.claude-3-sonnet",
		Messages: []schema.Message{{Role: "user", Content: json.RawMessage(`"hello"`)}},
	})
	if err == nil {
		t.Fatal("expected blocked model error")
	}
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Kind != ErrorBadRequest {
		t.Fatalf("expected bad request service error, got %v", err)
	}
	if svcErr.Message != "model anthropic.claude-3-sonnet is blocked by policy" {
		t.Fatalf("unexpected error message: %q", svcErr.Message)
	}
}

func TestServiceErrorMethodsViaServicePath(t *testing.T) {
	cause := errors.New("upstream boom")
	errWithCause := internal("failed to validate model", cause)

	if !strings.Contains(errWithCause.Error(), "failed to validate model: upstream boom") {
		t.Fatalf("unexpected formatted error: %q", errWithCause.Error())
	}
	if !errors.Is(errWithCause, cause) {
		t.Fatalf("expected errors.Is to unwrap service cause")
	}

	errNoCause := badRequest("model is required", nil)
	if errNoCause.Error() != "model is required" {
		t.Fatalf("unexpected no-cause formatted error: %q", errNoCause.Error())
	}
	if errors.Unwrap(errNoCause) != nil {
		t.Fatalf("expected nil unwrap cause for no-cause service error")
	}
}

type testModelPolicy struct {
	blocked map[string]bool
}

func (p *testModelPolicy) IsBlocked(modelID string) bool {
	return p.blocked[modelID]
}

func float32Ptr(v float32) *float32 { return &v }

var _ bedrock.ChatRuntime = (*fakeChatRuntime)(nil)
var _ bedrock.ModelDiscovery = (*fakeModels)(nil)

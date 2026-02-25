package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stratum/gateway/internal/schema"
)

func TestModelsService_Get(t *testing.T) {
	models := &fakeModels{
		models: map[string]schema.Model{
			"m1": {ID: "m1"},
		},
	}
	svc := NewModelsService(models, nil)

	model, err := svc.Get(context.Background(), "m1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if model == nil || model.ID != "m1" {
		t.Fatalf("unexpected model: %+v", model)
	}
}

func TestModelsService_List(t *testing.T) {
	models := &fakeModels{
		models: map[string]schema.Model{
			"m1": {ID: "m1"},
			"m2": {ID: "m2"},
		},
	}
	svc := NewModelsService(models, nil)
	out, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 models, got %d", len(out))
	}
}

func TestModelsService_GetNotFound(t *testing.T) {
	models := &fakeModels{models: map[string]schema.Model{}}
	svc := NewModelsService(models, nil)
	_, err := svc.Get(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected not found error")
	}
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Kind != ErrorNotFound {
		t.Fatalf("expected not found service error, got %v", err)
	}
}

func TestModelsService_ListError(t *testing.T) {
	svc := NewModelsService(&fakeModels{err: errors.New("boom")}, nil)
	_, err := svc.List(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Kind != ErrorInternal {
		t.Fatalf("expected internal service error, got %v", err)
	}
}

func TestModelsService_GetEmptyID(t *testing.T) {
	svc := NewModelsService(&fakeModels{}, nil)
	_, err := svc.Get(context.Background(), "   ")
	if err == nil {
		t.Fatal("expected error")
	}
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Kind != ErrorBadRequest {
		t.Fatalf("expected bad request service error, got %v", err)
	}
}

func TestModelsService_GetBlocked(t *testing.T) {
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
	svc := NewModelsService(models, p)

	_, err := svc.Get(context.Background(), "anthropic.claude-3-sonnet")
	if err == nil {
		t.Fatal("expected not found error")
	}
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Kind != ErrorNotFound {
		t.Fatalf("expected not found service error, got %v", err)
	}
}

func TestModelsService_ListFiltersBlocked(t *testing.T) {
	models := &fakeModels{
		models: map[string]schema.Model{
			"amazon.nova-micro-v1:0":    {ID: "amazon.nova-micro-v1:0"},
			"anthropic.claude-3-sonnet": {ID: "anthropic.claude-3-sonnet"},
		},
	}
	p := &testModelPolicy{
		blocked: map[string]bool{
			"anthropic.claude-3-sonnet": true,
		},
	}
	svc := NewModelsService(models, p)

	out, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 1 || out[0].ID != "amazon.nova-micro-v1:0" {
		t.Fatalf("unexpected filtered models: %+v", out)
	}
}

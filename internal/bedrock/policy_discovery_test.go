package bedrock

import (
	"context"
	"testing"

	"github.com/stratum/gateway/internal/schema"
)

type fakeDiscovery struct {
	models    []schema.Model
	modelByID map[string]schema.Model
	getErr    error
	findErr   error
	findCalls int
}

func (f *fakeDiscovery) GetModels(ctx context.Context) ([]schema.Model, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	out := make([]schema.Model, len(f.models))
	copy(out, f.models)
	return out, nil
}

func (f *fakeDiscovery) FindModel(ctx context.Context, modelID string) (*schema.Model, error) {
	f.findCalls++
	if f.findErr != nil {
		return nil, f.findErr
	}
	model, ok := f.modelByID[modelID]
	if !ok {
		return nil, nil
	}
	return &model, nil
}

type fakeBlockPolicy struct {
	blocked map[string]bool
}

func (f *fakeBlockPolicy) IsBlocked(modelID string) bool {
	return f.blocked[modelID]
}

func TestPolicyFilteredDiscovery_GetModels(t *testing.T) {
	inner := &fakeDiscovery{
		models: []schema.Model{
			{ID: "amazon.nova-micro-v1:0"},
			{ID: "anthropic.claude-3-sonnet"},
		},
	}
	p := &fakeBlockPolicy{
		blocked: map[string]bool{
			"anthropic.claude-3-sonnet": true,
		},
	}
	d := NewPolicyFilteredDiscovery(inner, p)

	models, err := d.GetModels(context.Background())
	if err != nil {
		t.Fatalf("GetModels() error = %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}
	if models[0].ID != "amazon.nova-micro-v1:0" {
		t.Fatalf("unexpected model ID %q", models[0].ID)
	}
}

func TestPolicyFilteredDiscovery_FindModelBlocked(t *testing.T) {
	inner := &fakeDiscovery{
		modelByID: map[string]schema.Model{
			"anthropic.claude-3-sonnet": {ID: "anthropic.claude-3-sonnet"},
		},
	}
	p := &fakeBlockPolicy{
		blocked: map[string]bool{
			"anthropic.claude-3-sonnet": true,
		},
	}
	d := NewPolicyFilteredDiscovery(inner, p)

	model, err := d.FindModel(context.Background(), "anthropic.claude-3-sonnet")
	if err != nil {
		t.Fatalf("FindModel() error = %v", err)
	}
	if model != nil {
		t.Fatalf("expected nil model for blocked ID, got %+v", model)
	}
	if inner.findCalls != 0 {
		t.Fatalf("expected inner FindModel not called for blocked model")
	}
}

func TestPolicyFilteredDiscovery_FindModelAllowed(t *testing.T) {
	inner := &fakeDiscovery{
		modelByID: map[string]schema.Model{
			"amazon.nova-micro-v1:0": {ID: "amazon.nova-micro-v1:0"},
		},
	}
	p := &fakeBlockPolicy{blocked: map[string]bool{}}
	d := NewPolicyFilteredDiscovery(inner, p)

	model, err := d.FindModel(context.Background(), "amazon.nova-micro-v1:0")
	if err != nil {
		t.Fatalf("FindModel() error = %v", err)
	}
	if model == nil || model.ID != "amazon.nova-micro-v1:0" {
		t.Fatalf("unexpected model %+v", model)
	}
	if inner.findCalls != 1 {
		t.Fatalf("expected inner FindModel call count 1, got %d", inner.findCalls)
	}
}

package bedrock

import (
	"context"

	"github.com/stratum/gateway/internal/schema"
)

// ModelBlockPolicy describes model block-list checks.
type ModelBlockPolicy interface {
	IsBlocked(modelID string) bool
}

// PolicyFilteredDiscovery wraps model discovery and excludes blocked models.
type PolicyFilteredDiscovery struct {
	inner  ModelDiscovery
	policy ModelBlockPolicy
}

// NewPolicyFilteredDiscovery creates a policy-aware model discovery wrapper.
func NewPolicyFilteredDiscovery(inner ModelDiscovery, policy ModelBlockPolicy) *PolicyFilteredDiscovery {
	return &PolicyFilteredDiscovery{
		inner:  inner,
		policy: policy,
	}
}

// GetModels returns discovered models excluding blocked IDs.
func (d *PolicyFilteredDiscovery) GetModels(ctx context.Context) ([]schema.Model, error) {
	if d == nil || d.inner == nil {
		return nil, nil
	}
	models, err := d.inner.GetModels(ctx)
	if err != nil {
		return nil, err
	}
	if d.policy == nil || len(models) == 0 {
		return models, nil
	}
	filtered := make([]schema.Model, 0, len(models))
	for _, model := range models {
		if d.policy.IsBlocked(model.ID) {
			continue
		}
		filtered = append(filtered, model)
	}
	return filtered, nil
}

// FindModel returns nil for blocked model IDs so they are undiscoverable.
func (d *PolicyFilteredDiscovery) FindModel(ctx context.Context, modelID string) (*schema.Model, error) {
	if d == nil {
		return nil, nil
	}
	if d.policy != nil && d.policy.IsBlocked(modelID) {
		return nil, nil
	}
	if d.inner == nil {
		return nil, nil
	}
	return d.inner.FindModel(ctx, modelID)
}

var _ ModelDiscovery = (*PolicyFilteredDiscovery)(nil)

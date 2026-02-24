package bedrock

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrock"
	bedrocktypes "github.com/aws/aws-sdk-go-v2/service/bedrock/types"
	"github.com/stratum/gateway/internal/schema"
)

// ModelCache holds discovered models with TTL.
type ModelCache struct {
	mu       sync.RWMutex
	models   []schema.Model
	loadedAt time.Time
	ttl      time.Duration
	client   *Client
}

// NewModelCache creates a model cache.
func NewModelCache(client *Client, ttl time.Duration) *ModelCache {
	return &ModelCache{
		client: client,
		ttl:    ttl,
	}
}

// GetModels returns cached models, refreshing if expired.
func (mc *ModelCache) GetModels(ctx context.Context) ([]schema.Model, error) {
	mc.mu.RLock()
	if mc.models != nil && time.Since(mc.loadedAt) < mc.ttl {
		models := mc.models
		mc.mu.RUnlock()
		return models, nil
	}
	mc.mu.RUnlock()

	return mc.refresh(ctx)
}

func (mc *ModelCache) refresh(ctx context.Context) ([]schema.Model, error) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	// Double-check after acquiring write lock.
	if mc.models != nil && time.Since(mc.loadedAt) < mc.ttl {
		return mc.models, nil
	}

	models, err := mc.discoverModels(ctx)
	if err != nil {
		return nil, err
	}

	mc.models = models
	mc.loadedAt = time.Now()
	log.Printf("[models] Discovered %d models", len(models))
	return models, nil
}

func (mc *ModelCache) discoverModels(ctx context.Context) ([]schema.Model, error) {
	now := time.Now().Unix()
	seen := make(map[string]bool)
	var models []schema.Model

	// 1. List foundation models (on-demand, Converse-capable)
	fmOut, err := mc.client.Bedrock.ListFoundationModels(ctx, &bedrock.ListFoundationModelsInput{})
	if err != nil {
		return nil, fmt.Errorf("ListFoundationModels: %w", err)
	}
	for _, fm := range fmOut.ModelSummaries {
		id := aws.ToString(fm.ModelId)
		if id == "" || seen[id] {
			continue
		}
		// Only include models that support on-demand and converse
		if !supportsOnDemand(fm) || !supportsConverse(fm) {
			continue
		}
		seen[id] = true
		models = append(models, schema.Model{
			ID:      id,
			Object:  "model",
			Created: now,
			OwnedBy: "bedrock",
		})
	}

	// 2. List inference profiles (cross-region + app profiles)
	if mc.client.Config.EnableCrossRegion || mc.client.Config.EnableAppInferenceProfile {
		profiles, err := mc.listAllInferenceProfiles(ctx)
		if err != nil {
			log.Printf("[models] Warning: ListInferenceProfiles failed: %v", err)
		} else {
			for _, profile := range profiles {
				id := aws.ToString(profile.InferenceProfileId)
				if id == "" || seen[id] {
					continue
				}
				isSystem := profile.Type == bedrocktypes.InferenceProfileTypeSystemDefined
				isApp := profile.Type == bedrocktypes.InferenceProfileTypeApplication

				if isSystem && !mc.client.Config.EnableCrossRegion {
					continue
				}
				if isApp && !mc.client.Config.EnableAppInferenceProfile {
					continue
				}

				seen[id] = true
				models = append(models, schema.Model{
					ID:      id,
					Object:  "model",
					Created: now,
					OwnedBy: "bedrock",
				})
			}
		}
	}

	return models, nil
}

func (mc *ModelCache) listAllInferenceProfiles(ctx context.Context) ([]bedrocktypes.InferenceProfileSummary, error) {
	var all []bedrocktypes.InferenceProfileSummary
	var nextToken *string

	for {
		out, err := mc.client.Bedrock.ListInferenceProfiles(ctx, &bedrock.ListInferenceProfilesInput{
			NextToken: nextToken,
		})
		if err != nil {
			return nil, err
		}
		all = append(all, out.InferenceProfileSummaries...)
		if out.NextToken == nil {
			break
		}
		nextToken = out.NextToken
	}
	return all, nil
}

func supportsOnDemand(fm bedrocktypes.FoundationModelSummary) bool {
	for _, t := range fm.InferenceTypesSupported {
		if t == bedrocktypes.InferenceTypeOnDemand {
			return true
		}
	}
	return false
}

func supportsConverse(fm bedrocktypes.FoundationModelSummary) bool {
	// Check if the model supports streaming (indicates Converse capability)
	// Models without streaming are typically legacy/embed-only
	for _, mode := range fm.OutputModalities {
		if mode == bedrocktypes.ModelModalityText {
			return true
		}
	}
	return false
}

// FindModel checks if a model ID exists.
func (mc *ModelCache) FindModel(ctx context.Context, modelID string) (*schema.Model, error) {
	models, err := mc.GetModels(ctx)
	if err != nil {
		return nil, err
	}
	modelID = strings.TrimSpace(modelID)
	for _, m := range models {
		if m.ID == modelID {
			return &m, nil
		}
	}
	return nil, nil
}

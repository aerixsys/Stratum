package bedrock

import (
	"context"

	"github.com/stratum/gateway/internal/schema"
)

// ChatRuntime describes chat operations against Bedrock.
type ChatRuntime interface {
	Converse(ctx context.Context, input *ConverseInput) (*schema.ChatResponse, error)
	ConverseStream(ctx context.Context, input *ConverseInput) <-chan []byte
}

// EmbeddingRuntime describes embeddings operations against Bedrock.
type EmbeddingRuntime interface {
	Embed(ctx context.Context, req *schema.EmbeddingRequest) (*schema.EmbeddingResponse, error)
}

// ModelDiscovery describes model listing and lookup operations.
type ModelDiscovery interface {
	GetModels(ctx context.Context) ([]schema.Model, error)
	FindModel(ctx context.Context, modelID string) (*schema.Model, error)
}

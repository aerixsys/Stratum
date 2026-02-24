package bedrock

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/stratum/gateway/internal/schema"
)

// Embed calls Bedrock InvokeModel for embedding models and returns OpenAI-format response.
func (c *Client) Embed(ctx context.Context, req *schema.EmbeddingRequest) (*schema.EmbeddingResponse, error) {
	inputs := req.InputStrings()
	if len(inputs) == 0 {
		return nil, fmt.Errorf("empty input")
	}

	model := req.Model
	isCohere := strings.Contains(strings.ToLower(model), "cohere")

	var allEmbeddings [][]float64
	var totalTokens int

	if isCohere {
		// Cohere supports batch
		embs, tokens, err := c.embedCohere(ctx, model, inputs)
		if err != nil {
			return nil, err
		}
		allEmbeddings = embs
		totalTokens = tokens
	} else {
		// Titan: one at a time
		for _, input := range inputs {
			emb, tokens, err := c.embedTitan(ctx, model, input)
			if err != nil {
				return nil, err
			}
			allEmbeddings = append(allEmbeddings, emb)
			totalTokens += tokens
		}
	}

	data := make([]schema.EmbeddingData, len(allEmbeddings))
	for i, emb := range allEmbeddings {
		data[i] = schema.EmbeddingData{
			Object:    "embedding",
			Embedding: emb,
			Index:     i,
		}
	}

	return &schema.EmbeddingResponse{
		Object: "list",
		Data:   data,
		Model:  model,
		Usage: &schema.EmbeddingUsage{
			PromptTokens: totalTokens,
			TotalTokens:  totalTokens,
		},
	}, nil
}

func (c *Client) embedCohere(ctx context.Context, model string, texts []string) ([][]float64, int, error) {
	payload := map[string]interface{}{
		"texts":      texts,
		"input_type": "search_document",
	}
	body, _ := json.Marshal(payload)

	out, err := c.BedrockRuntime.InvokeModel(ctx, &bedrockruntime.InvokeModelInput{
		ModelId:     aws.String(model),
		ContentType: aws.String("application/json"),
		Body:        body,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("InvokeModel (Cohere): %w", err)
	}

	var result struct {
		Embeddings [][]float64 `json:"embeddings"`
		Texts      []string    `json:"texts"`
	}
	if err := json.Unmarshal(out.Body, &result); err != nil {
		return nil, 0, fmt.Errorf("parse Cohere response: %w", err)
	}

	// Estimate tokens
	tokens := 0
	for _, t := range texts {
		tokens += len(strings.Fields(t))
	}
	return result.Embeddings, tokens, nil
}

func (c *Client) embedTitan(ctx context.Context, model, text string) ([]float64, int, error) {
	payload := map[string]interface{}{
		"inputText": text,
	}
	body, _ := json.Marshal(payload)

	out, err := c.BedrockRuntime.InvokeModel(ctx, &bedrockruntime.InvokeModelInput{
		ModelId:     aws.String(model),
		ContentType: aws.String("application/json"),
		Body:        body,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("InvokeModel (Titan): %w", err)
	}

	var result struct {
		Embedding      []float64 `json:"embedding"`
		InputTextToken int       `json:"inputTextTokenCount"`
	}
	if err := json.Unmarshal(out.Body, &result); err != nil {
		return nil, 0, fmt.Errorf("parse Titan response: %w", err)
	}

	return result.Embedding, result.InputTextToken, nil
}

package bedrock

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrock"
	bedrocktypes "github.com/aws/aws-sdk-go-v2/service/bedrock/types"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/stratum/gateway/internal/config"
)

type fakeRuntimeAPI struct {
	converseFn       func(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error)
	converseStreamFn func(ctx context.Context, params *bedrockruntime.ConverseStreamInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseStreamOutput, error)
}

func (f *fakeRuntimeAPI) Converse(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
	if f.converseFn != nil {
		return f.converseFn(ctx, params, optFns...)
	}
	return nil, errors.New("Converse not implemented")
}

func (f *fakeRuntimeAPI) ConverseStream(ctx context.Context, params *bedrockruntime.ConverseStreamInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseStreamOutput, error) {
	if f.converseStreamFn != nil {
		return f.converseStreamFn(ctx, params, optFns...)
	}
	return nil, errors.New("ConverseStream not implemented")
}

type fakeBedrockAPI struct {
	foundationOut   *bedrock.ListFoundationModelsOutput
	foundationErr   error
	foundationCalls int
}

func (f *fakeBedrockAPI) ListFoundationModels(ctx context.Context, params *bedrock.ListFoundationModelsInput, optFns ...func(*bedrock.Options)) (*bedrock.ListFoundationModelsOutput, error) {
	f.foundationCalls++
	if f.foundationErr != nil {
		return nil, f.foundationErr
	}
	if f.foundationOut == nil {
		return &bedrock.ListFoundationModelsOutput{}, nil
	}
	return f.foundationOut, nil
}

func TestClientConverse_Success(t *testing.T) {
	rt := &fakeRuntimeAPI{
		converseFn: func(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
			return &bedrockruntime.ConverseOutput{
				Output: &brtypes.ConverseOutputMemberMessage{
					Value: brtypes.Message{
						Role: brtypes.ConversationRoleAssistant,
						Content: []brtypes.ContentBlock{
							&brtypes.ContentBlockMemberText{Value: "ok"},
						},
					},
				},
				StopReason: brtypes.StopReasonEndTurn,
				Usage: &brtypes.TokenUsage{
					InputTokens:  aws.Int32(1),
					OutputTokens: aws.Int32(1),
					TotalTokens:  aws.Int32(2),
				},
			}, nil
		},
	}
	c := &Client{BedrockRuntime: rt, Config: &config.Config{}}

	resp, err := c.Converse(context.Background(), &ConverseInput{
		ModelID: "amazon.nova-micro-v1:0",
		Messages: []brtypes.Message{
			{
				Role:    brtypes.ConversationRoleUser,
				Content: []brtypes.ContentBlock{&brtypes.ContentBlockMemberText{Value: "hi"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil || len(resp.Choices) != 1 {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestClientConverseStream_StartupError(t *testing.T) {
	rt := &fakeRuntimeAPI{
		converseStreamFn: func(ctx context.Context, params *bedrockruntime.ConverseStreamInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseStreamOutput, error) {
			return nil, errors.New("upstream unavailable")
		},
	}
	c := &Client{BedrockRuntime: rt, Config: &config.Config{}}

	ch := c.ConverseStream(context.Background(), &ConverseInput{
		ModelID: "amazon.nova-micro-v1:0",
		Messages: []brtypes.Message{
			{
				Role:    brtypes.ConversationRoleUser,
				Content: []brtypes.ContentBlock{&brtypes.ContentBlockMemberText{Value: "hi"}},
			},
		},
	})

	var chunks []string
	for d := range ch {
		chunks = append(chunks, string(d))
	}
	if len(chunks) < 2 {
		t.Fatalf("expected at least error and done chunks, got %d", len(chunks))
	}
	if !strings.Contains(chunks[0], "\"error\"") {
		t.Fatalf("expected first chunk to contain JSON error payload: %q", chunks[0])
	}
	if !strings.Contains(chunks[len(chunks)-1], "[DONE]") {
		t.Fatalf("expected final chunk to contain [DONE]")
	}
}

func TestMarshalSSE(t *testing.T) {
	b, err := marshalSSE(map[string]string{"x": "y"})
	if err != nil {
		t.Fatalf("marshalSSE error: %v", err)
	}
	if !strings.HasPrefix(string(b), "data: ") || !strings.HasSuffix(string(b), "\n\n") {
		t.Fatalf("unexpected sse format: %q", string(b))
	}
}

func TestApplyConverseFields(t *testing.T) {
	dst := &bedrockruntime.ConverseInput{}
	applyConverseFields(dst, &ConverseInput{
		AdditionalModelRequestFields: document.NewLazyDocument(map[string]interface{}{"foo": "bar"}),
	})

	if dst.AdditionalModelRequestFields == nil {
		t.Fatalf("expected additional request fields")
	}
}

func TestApplyConverseStreamFields(t *testing.T) {
	dst := &bedrockruntime.ConverseStreamInput{}
	applyConverseStreamFields(dst, &ConverseInput{
		AdditionalModelRequestFields: document.NewLazyDocument(map[string]interface{}{"foo": "bar"}),
	})

	if dst.AdditionalModelRequestFields == nil {
		t.Fatalf("expected additional request fields")
	}
}

func TestModelCache_DiscoveryAndFind(t *testing.T) {
	br := &fakeBedrockAPI{
		foundationOut: &bedrock.ListFoundationModelsOutput{
			ModelSummaries: []bedrocktypes.FoundationModelSummary{
				{
					ModelId:                 aws.String("amazon.nova-micro-v1:0"),
					InferenceTypesSupported: []bedrocktypes.InferenceType{bedrocktypes.InferenceTypeOnDemand},
					OutputModalities:        []bedrocktypes.ModelModality{bedrocktypes.ModelModalityText},
				},
				{
					ModelId:                 aws.String("meta.llama3-1-8b-instruct-v1:0"),
					InferenceTypesSupported: []bedrocktypes.InferenceType{bedrocktypes.InferenceTypeProvisioned},
					OutputModalities:        []bedrocktypes.ModelModality{bedrocktypes.ModelModalityText},
				},
				{
					ModelId:                 aws.String("filtered.no-ondemand"),
					InferenceTypesSupported: []bedrocktypes.InferenceType{bedrocktypes.InferenceTypeProvisioned},
					OutputModalities:        []bedrocktypes.ModelModality{bedrocktypes.ModelModalityText},
				},
			},
		},
	}

	c := &Client{
		Bedrock: br,
		Config:  &config.Config{},
	}
	mc := NewModelCache(c, time.Minute)

	models, err := mc.GetModels(context.Background())
	if err != nil {
		t.Fatalf("GetModels error: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 discovered model, got %d", len(models))
	}
	found, err := mc.FindModel(context.Background(), "amazon.nova-micro-v1:0")
	if err != nil {
		t.Fatalf("FindModel error: %v", err)
	}
	if found == nil {
		t.Fatalf("expected model to be found")
	}
	// Cached path should avoid repeat discovery calls.
	if _, err := mc.GetModels(context.Background()); err != nil {
		t.Fatalf("GetModels cached error: %v", err)
	}
	if br.foundationCalls != 1 {
		t.Fatalf("expected one foundation discovery call, got %d", br.foundationCalls)
	}
}

var _ bedrockAPI = (*fakeBedrockAPI)(nil)
var _ bedrockRuntimeAPI = (*fakeRuntimeAPI)(nil)

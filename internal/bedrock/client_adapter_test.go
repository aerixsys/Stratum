package bedrock

import (
	"context"
	"encoding/json"
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
	"github.com/stratum/gateway/internal/schema"
)

type fakeRuntimeAPI struct {
	converseFn       func(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error)
	converseStreamFn func(ctx context.Context, params *bedrockruntime.ConverseStreamInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseStreamOutput, error)
	invokeFn         func(ctx context.Context, params *bedrockruntime.InvokeModelInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.InvokeModelOutput, error)
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

func (f *fakeRuntimeAPI) InvokeModel(ctx context.Context, params *bedrockruntime.InvokeModelInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.InvokeModelOutput, error) {
	if f.invokeFn != nil {
		return f.invokeFn(ctx, params, optFns...)
	}
	return nil, errors.New("InvokeModel not implemented")
}

type fakeBedrockAPI struct {
	foundationOut   *bedrock.ListFoundationModelsOutput
	foundationErr   error
	inferenceOut    *bedrock.ListInferenceProfilesOutput
	inferenceErr    error
	foundationCalls int
	inferenceCalls  int
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

func (f *fakeBedrockAPI) ListInferenceProfiles(ctx context.Context, params *bedrock.ListInferenceProfilesInput, optFns ...func(*bedrock.Options)) (*bedrock.ListInferenceProfilesOutput, error) {
	f.inferenceCalls++
	if f.inferenceErr != nil {
		return nil, f.inferenceErr
	}
	if f.inferenceOut == nil {
		return &bedrock.ListInferenceProfilesOutput{}, nil
	}
	return f.inferenceOut, nil
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
		ThinkingConfig:                    &ThinkingConfig{Enabled: true, BudgetToken: 1024},
		AdditionalModelRequestFields:      document.NewLazyDocument(map[string]interface{}{"foo": "bar"}),
		AdditionalModelResponseFieldPaths: []string{"/stop_sequence"},
		RequestMetadata:                   map[string]string{"tenant": "acme"},
		PerformanceLatency:                "optimized",
		ServiceTier:                       "priority",
		GuardrailConfig: &GuardrailConfig{
			Identifier: "gr-1",
			Version:    "1",
			Trace:      "enabled",
		},
	})

	if dst.AdditionalModelRequestFields == nil {
		t.Fatalf("expected additional request fields")
	}
	if len(dst.RequestMetadata) != 1 || dst.RequestMetadata["tenant"] != "acme" {
		t.Fatalf("unexpected request metadata: %+v", dst.RequestMetadata)
	}
	if dst.PerformanceConfig == nil || dst.PerformanceConfig.Latency != brtypes.PerformanceConfigLatencyOptimized {
		t.Fatalf("expected performance config")
	}
	if dst.ServiceTier == nil || dst.ServiceTier.Type != brtypes.ServiceTierTypePriority {
		t.Fatalf("expected priority service tier")
	}
	if dst.GuardrailConfig == nil || aws.ToString(dst.GuardrailConfig.GuardrailIdentifier) != "gr-1" {
		t.Fatalf("expected guardrail config")
	}
}

func TestApplyConverseStreamFields(t *testing.T) {
	dst := &bedrockruntime.ConverseStreamInput{}
	applyConverseStreamFields(dst, &ConverseInput{
		AdditionalModelRequestFields:      document.NewLazyDocument(map[string]interface{}{"foo": "bar"}),
		AdditionalModelResponseFieldPaths: []string{"/stop_sequence"},
		RequestMetadata:                   map[string]string{"tenant": "acme"},
		PerformanceLatency:                "optimized",
		ServiceTier:                       "priority",
		GuardrailConfig: &GuardrailConfig{
			Identifier:       "gr-1",
			Version:          "1",
			Trace:            "enabled",
			StreamProcessing: "sync",
		},
	})

	if dst.GuardrailConfig == nil {
		t.Fatalf("expected stream guardrail config")
	}
	if dst.GuardrailConfig.StreamProcessingMode != brtypes.GuardrailStreamProcessingModeSync {
		t.Fatalf("expected sync stream processing mode")
	}
}

func TestEmbed_CohereAndTitanPaths(t *testing.T) {
	callCount := 0
	rt := &fakeRuntimeAPI{
		invokeFn: func(ctx context.Context, params *bedrockruntime.InvokeModelInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.InvokeModelOutput, error) {
			callCount++
			model := aws.ToString(params.ModelId)
			if strings.Contains(strings.ToLower(model), "cohere") {
				return &bedrockruntime.InvokeModelOutput{
					Body: []byte(`{"embeddings":[[0.1],[0.2]],"texts":["a","b"]}`),
				}, nil
			}
			return &bedrockruntime.InvokeModelOutput{
				Body: []byte(`{"embedding":[0.5,0.7],"inputTextTokenCount":3}`),
			}, nil
		},
	}
	c := &Client{BedrockRuntime: rt, Config: &config.Config{}}

	cohereResp, err := c.Embed(context.Background(), &schema.EmbeddingRequest{
		Model: "cohere.embed-multilingual-v3",
		Input: json.RawMessage(`["a","b"]`),
	})
	if err != nil {
		t.Fatalf("cohere embed error: %v", err)
	}
	if len(cohereResp.Data) != 2 {
		t.Fatalf("expected 2 cohere embeddings, got %d", len(cohereResp.Data))
	}

	titanResp, err := c.Embed(context.Background(), &schema.EmbeddingRequest{
		Model: "amazon.titan-embed-text-v2:0",
		Input: json.RawMessage(`["x","y"]`),
	})
	if err != nil {
		t.Fatalf("titan embed error: %v", err)
	}
	if len(titanResp.Data) != 2 {
		t.Fatalf("expected 2 titan embeddings, got %d", len(titanResp.Data))
	}
	// 1 invoke for cohere + 2 invokes for titan inputs
	if callCount != 3 {
		t.Fatalf("unexpected invoke count: %d", callCount)
	}
}

func TestModelCache_DiscoveryAndFind(t *testing.T) {
	now := time.Now()
	br := &fakeBedrockAPI{
		foundationOut: &bedrock.ListFoundationModelsOutput{
			ModelSummaries: []bedrocktypes.FoundationModelSummary{
				{
					ModelId:                 aws.String("amazon.nova-micro-v1:0"),
					InferenceTypesSupported: []bedrocktypes.InferenceType{bedrocktypes.InferenceTypeOnDemand},
					OutputModalities:        []bedrocktypes.ModelModality{bedrocktypes.ModelModalityText},
				},
				{
					ModelId:                 aws.String("filtered.no-ondemand"),
					InferenceTypesSupported: []bedrocktypes.InferenceType{bedrocktypes.InferenceTypeProvisioned},
					OutputModalities:        []bedrocktypes.ModelModality{bedrocktypes.ModelModalityText},
				},
			},
		},
		inferenceOut: &bedrock.ListInferenceProfilesOutput{
			InferenceProfileSummaries: []bedrocktypes.InferenceProfileSummary{
				{
					InferenceProfileId:   aws.String("us.profile.system"),
					InferenceProfileArn:  aws.String("arn:aws:bedrock:us-east-1:123:inference-profile/us.profile.system"),
					InferenceProfileName: aws.String("system"),
					Type:                 bedrocktypes.InferenceProfileTypeSystemDefined,
					Status:               bedrocktypes.InferenceProfileStatusActive,
					Models:               []bedrocktypes.InferenceProfileModel{},
					CreatedAt:            &now,
				},
				{
					InferenceProfileId:   aws.String("app.profile.1"),
					InferenceProfileArn:  aws.String("arn:aws:bedrock:us-east-1:123:inference-profile/app.profile.1"),
					InferenceProfileName: aws.String("app"),
					Type:                 bedrocktypes.InferenceProfileTypeApplication,
					Status:               bedrocktypes.InferenceProfileStatusActive,
					Models:               []bedrocktypes.InferenceProfileModel{},
					CreatedAt:            &now,
				},
			},
		},
	}

	c := &Client{
		Bedrock: br,
		Config: &config.Config{
			EnableCrossRegion:         true,
			EnableAppInferenceProfile: true,
		},
	}
	mc := NewModelCache(c, time.Minute)

	models, err := mc.GetModels(context.Background())
	if err != nil {
		t.Fatalf("GetModels error: %v", err)
	}
	if len(models) != 3 {
		t.Fatalf("expected 3 discovered models, got %d", len(models))
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

func TestModelCapabilityHelpers(t *testing.T) {
	if !supportsOnDemand(bedrocktypes.FoundationModelSummary{
		InferenceTypesSupported: []bedrocktypes.InferenceType{bedrocktypes.InferenceTypeOnDemand},
	}) {
		t.Fatalf("expected on-demand support true")
	}
	if supportsOnDemand(bedrocktypes.FoundationModelSummary{
		InferenceTypesSupported: []bedrocktypes.InferenceType{bedrocktypes.InferenceTypeProvisioned},
	}) {
		t.Fatalf("expected on-demand support false")
	}
	if !supportsConverse(bedrocktypes.FoundationModelSummary{
		OutputModalities: []bedrocktypes.ModelModality{bedrocktypes.ModelModalityText},
	}) {
		t.Fatalf("expected converse support true")
	}
	if supportsConverse(bedrocktypes.FoundationModelSummary{
		OutputModalities: []bedrocktypes.ModelModality{bedrocktypes.ModelModalityImage},
	}) {
		t.Fatalf("expected converse support false")
	}
}

var _ bedrockAPI = (*fakeBedrockAPI)(nil)
var _ bedrockRuntimeAPI = (*fakeRuntimeAPI)(nil)

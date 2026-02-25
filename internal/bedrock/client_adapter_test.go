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
		inferenceOut: &bedrock.ListInferenceProfilesOutput{
			InferenceProfileSummaries: []bedrocktypes.InferenceProfileSummary{
				{
					InferenceProfileId:   aws.String("us.profile.system"),
					InferenceProfileArn:  aws.String("arn:aws:bedrock:us-east-1:123:inference-profile/us.profile.system"),
					InferenceProfileName: aws.String("system"),
					Type:                 bedrocktypes.InferenceProfileTypeSystemDefined,
					Status:               bedrocktypes.InferenceProfileStatusActive,
					Models: []bedrocktypes.InferenceProfileModel{
						{ModelArn: aws.String("arn:aws:bedrock:us-east-1::foundation-model/meta.llama3-1-8b-instruct-v1:0")},
					},
					CreatedAt: &now,
				},
				{
					InferenceProfileId:   aws.String("app.profile.1"),
					InferenceProfileArn:  aws.String("arn:aws:bedrock:us-east-1:123:inference-profile/app.profile.1"),
					InferenceProfileName: aws.String("app"),
					Type:                 bedrocktypes.InferenceProfileTypeApplication,
					Status:               bedrocktypes.InferenceProfileStatusActive,
					Models: []bedrocktypes.InferenceProfileModel{
						{ModelArn: aws.String("arn:aws:bedrock:us-east-1::foundation-model/amazon.nova-micro-v1:0")},
					},
					CreatedAt: &now,
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
	if !supportsTextOutput(bedrocktypes.FoundationModelSummary{
		OutputModalities: []bedrocktypes.ModelModality{bedrocktypes.ModelModalityText},
	}) {
		t.Fatalf("expected text output support true")
	}
	if supportsTextOutput(bedrocktypes.FoundationModelSummary{
		OutputModalities: []bedrocktypes.ModelModality{bedrocktypes.ModelModalityImage},
	}) {
		t.Fatalf("expected text output support false")
	}
	if !hasDisallowedOutputModalities(bedrocktypes.FoundationModelSummary{
		OutputModalities: []bedrocktypes.ModelModality{bedrocktypes.ModelModalityImage},
	}) {
		t.Fatalf("expected disallowed output modality true")
	}
	if hasDisallowedOutputModalities(bedrocktypes.FoundationModelSummary{
		OutputModalities: []bedrocktypes.ModelModality{bedrocktypes.ModelModalityText},
	}) {
		t.Fatalf("expected disallowed output modality false")
	}
	if got := modelIDFromFoundationARN("arn:aws:bedrock:us-east-1::foundation-model/meta.llama3-1-8b-instruct-v1:0"); got != "meta.llama3-1-8b-instruct-v1:0" {
		t.Fatalf("unexpected model id parse: %q", got)
	}
	if got := modelIDFromFoundationARN("arn:aws:bedrock:us-east-1::inference-profile/us.meta.llama3"); got != "" {
		t.Fatalf("expected empty parse for non-foundation arn, got %q", got)
	}
	if !profileSupportsTextOnlyOutputs(bedrocktypes.InferenceProfileSummary{
		Models: []bedrocktypes.InferenceProfileModel{
			{ModelArn: aws.String("arn:aws:bedrock:us-east-1::foundation-model/meta.llama3-1-8b-instruct-v1:0")},
		},
	}, map[string]bool{"meta.llama3-1-8b-instruct-v1:0": true}) {
		t.Fatalf("expected profile eligibility true")
	}
	if profileSupportsTextOnlyOutputs(bedrocktypes.InferenceProfileSummary{
		Models: []bedrocktypes.InferenceProfileModel{
			{ModelArn: aws.String("arn:aws:bedrock:us-east-1::foundation-model/meta.llama3-1-8b-instruct-v1:0")},
		},
	}, map[string]bool{"meta.llama3-1-8b-instruct-v1:0": false}) {
		t.Fatalf("expected profile eligibility false")
	}
}

var _ bedrockAPI = (*fakeBedrockAPI)(nil)
var _ bedrockRuntimeAPI = (*fakeRuntimeAPI)(nil)

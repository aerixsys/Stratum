package bedrock

import (
	"encoding/json"
	"testing"

	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/stratum/gateway/internal/schema"
)

func TestTranslateRequest_BasicMessages(t *testing.T) {
	req := &schema.ChatRequest{
		Model: "anthropic.claude-3-sonnet",
		Messages: []schema.Message{
			{Role: "user", Content: json.RawMessage(`"Hello"`)},
		},
	}
	input, err := TranslateRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if input.ModelID != "anthropic.claude-3-sonnet" {
		t.Errorf("expected model anthropic.claude-3-sonnet, got %s", input.ModelID)
	}
	if len(input.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(input.Messages))
	}
	if input.Messages[0].Role != brtypes.ConversationRoleUser {
		t.Errorf("expected user role, got %s", input.Messages[0].Role)
	}
}

func TestTranslateRequest_SystemMessage(t *testing.T) {
	req := &schema.ChatRequest{
		Model: "anthropic.claude-3-sonnet",
		Messages: []schema.Message{
			{Role: "system", Content: json.RawMessage(`"You are helpful"`)},
			{Role: "user", Content: json.RawMessage(`"Hi"`)},
		},
	}
	input, err := TranslateRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(input.System) != 1 {
		t.Fatalf("expected 1 system block, got %d", len(input.System))
	}
	if len(input.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(input.Messages))
	}
}

func TestTranslateRequest_ReasoningExcludeDoesNotStripSampling(t *testing.T) {
	temp := float32(0.7)
	topP := float32(0.9)
	exclude := true
	req := &schema.ChatRequest{
		Model:       "anthropic.claude-sonnet-4-5-20250929-v1:0",
		Messages:    []schema.Message{{Role: "user", Content: json.RawMessage(`"Think"`)}},
		Temperature: &temp,
		TopP:        &topP,
		Reasoning: &schema.Reasoning{
			Exclude: &exclude,
		},
	}
	input, err := TranslateRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !input.ReasoningExclude {
		t.Fatal("expected reasoning exclude=true")
	}
	if input.InferenceConfig == nil || input.InferenceConfig.Temperature == nil {
		t.Fatal("expected temperature to remain set")
	}
	// Claude 4.5 quirk still applies: with temp+topP, topP is dropped.
	if input.InferenceConfig.TopP != nil {
		t.Fatal("expected topP to be dropped by Claude 4.5 quirk")
	}
	if input.AdditionalModelRequestFields != nil {
		t.Fatal("did not expect adapter-generated additional_model_request_fields")
	}
}

func TestTranslateRequest_Claude45SamplingQuirk(t *testing.T) {
	temp := float32(0.7)
	topP := float32(0.9)
	req := &schema.ChatRequest{
		Model:       "anthropic.claude-sonnet-4-5-20250929-v1:0",
		Messages:    []schema.Message{{Role: "user", Content: json.RawMessage(`"Hi"`)}},
		Temperature: &temp,
		TopP:        &topP,
	}
	input, err := TranslateRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Claude Sonnet 4.5: can't have both temp and topP → keep temp, drop topP
	if input.InferenceConfig == nil {
		t.Fatal("InferenceConfig should be set")
	}
	if input.InferenceConfig.Temperature == nil {
		t.Error("temperature should be preserved")
	}
	if input.InferenceConfig.TopP != nil {
		t.Error("topP should be dropped for Claude Sonnet 4.5")
	}
}

func TestTranslateRequest_ToolChoice(t *testing.T) {
	tests := []struct {
		name       string
		toolChoice string
		wantNil    bool // tool config should be nil
	}{
		{"auto", `"auto"`, false},
		{"required", `"required"`, false},
		{"none", `"none"`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &schema.ChatRequest{
				Model:      "anthropic.claude-3-sonnet",
				Messages:   []schema.Message{{Role: "user", Content: json.RawMessage(`"Hi"`)}},
				Tools:      []schema.Tool{{Type: "function", Function: schema.ToolFunction{Name: "test", Description: "test"}}},
				ToolChoice: json.RawMessage(tt.toolChoice),
			}
			input, err := TranslateRequest(req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantNil && input.ToolConfig != nil {
				t.Error("expected nil ToolConfig for 'none'")
			}
			if !tt.wantNil && input.ToolConfig == nil {
				t.Error("expected non-nil ToolConfig")
			}
		})
	}
}

func TestTranslateRequest_SpecificToolChoice(t *testing.T) {
	req := &schema.ChatRequest{
		Model:      "anthropic.claude-3-sonnet",
		Messages:   []schema.Message{{Role: "user", Content: json.RawMessage(`"Hi"`)}},
		Tools:      []schema.Tool{{Type: "function", Function: schema.ToolFunction{Name: "get_weather", Description: "weather"}}},
		ToolChoice: json.RawMessage(`{"type":"function","function":{"name":"get_weather"}}`),
	}
	input, err := TranslateRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if input.ToolConfig == nil {
		t.Fatal("expected non-nil ToolConfig")
	}
	if input.ToolConfig.ToolChoice == nil {
		t.Fatal("expected non-nil ToolChoice")
	}
	if _, ok := input.ToolConfig.ToolChoice.(*brtypes.ToolChoiceMemberTool); !ok {
		t.Error("expected ToolChoiceMemberTool")
	}
}

func TestTranslateRequest_PromptCachingDisabledByDefault(t *testing.T) {
	req := &schema.ChatRequest{
		Model: "anthropic.claude-3-7-sonnet-20250219-v1:0",
		Messages: []schema.Message{
			{Role: "system", Content: json.RawMessage(`"You are helpful"`)},
			{Role: "user", Content: json.RawMessage(`"Hi"`)},
		},
	}
	input, err := TranslateRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, msg := range input.Messages {
		for _, block := range msg.Content {
			if _, ok := block.(*brtypes.ContentBlockMemberCachePoint); ok {
				t.Fatal("did not expect cache point without per-request prompt_caching")
			}
		}
	}
}

func TestTranslateRequest_PromptCachingEnabledPerRequest(t *testing.T) {
	req := &schema.ChatRequest{
		Model:     "anthropic.claude-3-7-sonnet-20250219-v1:0",
		Messages:  []schema.Message{{Role: "system", Content: json.RawMessage(`"You are helpful"`)}, {Role: "user", Content: json.RawMessage(`"Hi"`)}},
		ExtraBody: json.RawMessage(`{"prompt_caching":{"enabled":true}}`),
	}
	input, err := TranslateRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(input.System) != 2 {
		t.Fatalf("expected system cache point when prompt_caching is enabled, got %d blocks", len(input.System))
	}
	lastMsg := input.Messages[len(input.Messages)-1]
	if lastMsg.Role != brtypes.ConversationRoleUser {
		t.Fatal("last message should be user")
	}
	foundCache := false
	for _, block := range lastMsg.Content {
		if _, ok := block.(*brtypes.ContentBlockMemberCachePoint); ok {
			foundCache = true
		}
	}
	if !foundCache {
		t.Fatal("expected cache point in last user message")
	}
}

func TestTranslateRequest_PromptCachingDisabledPerRequest(t *testing.T) {
	falseVal := false
	extra, _ := json.Marshal(map[string]interface{}{"prompt_caching": falseVal})
	req := &schema.ChatRequest{
		Model:     "anthropic.claude-3-7-sonnet-20250219-v1:0",
		Messages:  []schema.Message{{Role: "user", Content: json.RawMessage(`"Hi"`)}},
		ExtraBody: extra,
	}
	input, err := TranslateRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, msg := range input.Messages {
		for _, block := range msg.Content {
			if _, ok := block.(*brtypes.ContentBlockMemberCachePoint); ok {
				t.Error("should not have cache point when per-request prompt_caching is false")
			}
		}
	}
}

func TestTranslateRequest_PromptCachingTTL(t *testing.T) {
	req := &schema.ChatRequest{
		Model: "anthropic.claude-haiku-4-5-20251001-v1:0",
		Messages: []schema.Message{
			{Role: "system", Content: json.RawMessage(`"You are helpful"`)},
			{Role: "user", Content: json.RawMessage(`"Hi"`)},
		},
		ExtraBody: json.RawMessage(`{"prompt_caching":{"enabled":true,"ttl":"1h"}}`),
	}
	input, err := TranslateRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(input.System) != 2 {
		t.Fatalf("expected system cache point, got %d blocks", len(input.System))
	}
	cp, ok := input.System[1].(*brtypes.SystemContentBlockMemberCachePoint)
	if !ok {
		t.Fatalf("expected cache point block")
	}
	if cp.Value.Ttl != brtypes.CacheTTLOneHour {
		t.Fatalf("expected ttl=1h, got %s", cp.Value.Ttl)
	}
}

func TestTranslateRequest_ReasoningExclude(t *testing.T) {
	exclude := true
	req := &schema.ChatRequest{
		Model:    "anthropic.claude-3-7-sonnet-20250219-v1:0",
		Messages: []schema.Message{{Role: "user", Content: json.RawMessage(`"Hi"`)}},
		Reasoning: &schema.Reasoning{
			Exclude: &exclude,
		},
	}
	input, err := TranslateRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !input.ReasoningExclude {
		t.Fatalf("expected reasoning exclude=true")
	}
}

func TestTranslateRequest_AdditionalModelRequestFieldsPassthrough(t *testing.T) {
	req := &schema.ChatRequest{
		Model:    "amazon.nova-micro-v1:0",
		Messages: []schema.Message{{Role: "user", Content: json.RawMessage(`"Hi"`)}},
		ExtraBody: json.RawMessage(`{
			"additional_model_request_fields": {
				"reasoning_effort":"high",
				"reasoningConfig":{"type":"enabled","maxReasoningEffort":"medium"},
				"custom":"value"
			}
		}`),
	}
	input, err := TranslateRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if input.AdditionalModelRequestFields == nil {
		t.Fatalf("expected additional_model_request_fields")
	}
	data, err := input.AdditionalModelRequestFields.MarshalSmithyDocument()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded["reasoning_effort"] != "high" {
		t.Fatalf("expected reasoning_effort=high passthrough, got %+v", decoded)
	}
	if decoded["custom"] != "value" {
		t.Fatalf("expected custom=value passthrough, got %+v", decoded)
	}
}

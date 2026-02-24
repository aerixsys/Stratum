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
	input, err := TranslateRequest(req, TranslateConfig{})
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
	input, err := TranslateRequest(req, TranslateConfig{})
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

func TestTranslateRequest_ThinkingStripsTemperature(t *testing.T) {
	temp := float32(0.7)
	topP := float32(0.9)
	req := &schema.ChatRequest{
		Model:           "anthropic.claude-3-7-sonnet-20250219-v1:0",
		Messages:        []schema.Message{{Role: "user", Content: json.RawMessage(`"Think"`)}},
		Temperature:     &temp,
		TopP:            &topP,
		ReasoningEffort: "high",
	}
	input, err := TranslateRequest(req, TranslateConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Thinking enabled → temperature and topP should be stripped
	if input.InferenceConfig != nil && input.InferenceConfig.Temperature != nil {
		t.Error("temperature should be nil when thinking is enabled")
	}
	if input.InferenceConfig != nil && input.InferenceConfig.TopP != nil {
		t.Error("topP should be nil when thinking is enabled")
	}
	if input.ThinkingConfig == nil {
		t.Fatal("ThinkingConfig should be set")
	}
	if input.ThinkingConfig.BudgetToken != 16384 {
		t.Errorf("expected budget 16384 for high, got %d", input.ThinkingConfig.BudgetToken)
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
	input, err := TranslateRequest(req, TranslateConfig{})
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
			input, err := TranslateRequest(req, TranslateConfig{})
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
	input, err := TranslateRequest(req, TranslateConfig{})
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

func TestTranslateRequest_PromptCaching(t *testing.T) {
	req := &schema.ChatRequest{
		Model: "anthropic.claude-3-7-sonnet-20250219-v1:0",
		Messages: []schema.Message{
			{Role: "system", Content: json.RawMessage(`"You are helpful"`)},
			{Role: "user", Content: json.RawMessage(`"Hi"`)},
		},
	}
	input, err := TranslateRequest(req, TranslateConfig{EnablePromptCaching: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// System should have 2 blocks: text + cachePoint
	if len(input.System) != 2 {
		t.Errorf("expected 2 system blocks (text + cache), got %d", len(input.System))
	}
	// Last user message should have cache point appended
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
		t.Error("expected cache point in last user message")
	}
}

func TestTranslateRequest_PerRequestCachingOverride(t *testing.T) {
	falseVal := false
	extra, _ := json.Marshal(map[string]interface{}{"prompt_caching": falseVal})
	req := &schema.ChatRequest{
		Model:     "anthropic.claude-3-7-sonnet-20250219-v1:0",
		Messages:  []schema.Message{{Role: "user", Content: json.RawMessage(`"Hi"`)}},
		ExtraBody: extra,
	}
	// Config says caching enabled, but per-request override says false
	input, err := TranslateRequest(req, TranslateConfig{EnablePromptCaching: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should NOT have cache points
	for _, msg := range input.Messages {
		for _, block := range msg.Content {
			if _, ok := block.(*brtypes.ContentBlockMemberCachePoint); ok {
				t.Error("should not have cache point when per-request override is false")
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
	input, err := TranslateRequest(req, TranslateConfig{})
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

func TestTranslateRequest_InvalidAdditionalFieldPath(t *testing.T) {
	req := &schema.ChatRequest{
		Model:    "anthropic.claude-3-sonnet",
		Messages: []schema.Message{{Role: "user", Content: json.RawMessage(`"Hi"`)}},
		ExtraBody: json.RawMessage(`{
			"additional_model_response_field_paths":["stop_sequence"]
		}`),
	}
	_, err := TranslateRequest(req, TranslateConfig{})
	if err == nil {
		t.Fatal("expected error for invalid response field path")
	}
}

func TestTranslateRequest_ExtraBedrockControls(t *testing.T) {
	req := &schema.ChatRequest{
		Model:    "anthropic.claude-3-sonnet",
		Messages: []schema.Message{{Role: "user", Content: json.RawMessage(`"Hi"`)}},
		ExtraBody: json.RawMessage(`{
			"guardrail_config":{"guardrail_identifier":"gr-1","guardrail_version":"1","trace":"enabled","stream_processing_mode":"sync"},
			"request_metadata":{"tenant":"acme"},
			"additional_model_request_fields":{"custom":"value"},
			"additional_model_response_field_paths":["/stop_sequence"],
			"performance_config":{"latency":"optimized"},
			"service_tier":"priority"
		}`),
	}
	input, err := TranslateRequest(req, TranslateConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if input.GuardrailConfig == nil {
		t.Fatalf("expected guardrail config")
	}
	if input.GuardrailConfig.Identifier != "gr-1" || input.GuardrailConfig.Version != "1" {
		t.Fatalf("unexpected guardrail config: %+v", input.GuardrailConfig)
	}
	if input.RequestMetadata["tenant"] != "acme" {
		t.Fatalf("expected request metadata tenant=acme")
	}
	if input.PerformanceLatency != "optimized" {
		t.Fatalf("expected performance latency optimized")
	}
	if input.ServiceTier != "priority" {
		t.Fatalf("expected priority service tier")
	}
	if len(input.AdditionalModelResponseFieldPaths) != 1 || input.AdditionalModelResponseFieldPaths[0] != "/stop_sequence" {
		t.Fatalf("unexpected additional_model_response_field_paths")
	}
}

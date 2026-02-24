package bedrock

import (
	"encoding/json"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
)

func TestConvertConverseOutput_TextOnly(t *testing.T) {
	out := &bedrockruntime.ConverseOutput{
		Output: &brtypes.ConverseOutputMemberMessage{
			Value: brtypes.Message{
				Role: brtypes.ConversationRoleAssistant,
				Content: []brtypes.ContentBlock{
					&brtypes.ContentBlockMemberText{Value: "Hello world"},
				},
			},
		},
		StopReason: brtypes.StopReasonEndTurn,
		Usage: &brtypes.TokenUsage{
			InputTokens:  aws.Int32(10),
			OutputTokens: aws.Int32(5),
			TotalTokens:  aws.Int32(15),
		},
	}

	resp := convertConverseOutput(out, "test-model")

	if resp.Object != "chat.completion" {
		t.Errorf("expected chat.completion, got %s", resp.Object)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(resp.Choices))
	}
	if resp.Choices[0].Message.Content == nil || *resp.Choices[0].Message.Content != "Hello world" {
		t.Error("expected content 'Hello world'")
	}
	if resp.Choices[0].Message.Reasoning != nil {
		t.Error("expected nil reasoning for text-only response")
	}
	if resp.Usage == nil {
		t.Fatal("expected usage")
	}
	if resp.Usage.PromptTokens != 10 {
		t.Errorf("expected 10 prompt tokens, got %d", resp.Usage.PromptTokens)
	}
}

func TestConvertConverseOutput_WithReasoning(t *testing.T) {
	thinkText := "Let me think about this..."
	sig := "abc123signature"
	out := &bedrockruntime.ConverseOutput{
		Output: &brtypes.ConverseOutputMemberMessage{
			Value: brtypes.Message{
				Role: brtypes.ConversationRoleAssistant,
				Content: []brtypes.ContentBlock{
					&brtypes.ContentBlockMemberReasoningContent{
						Value: &brtypes.ReasoningContentBlockMemberReasoningText{
							Value: brtypes.ReasoningTextBlock{
								Text:      &thinkText,
								Signature: &sig,
							},
						},
					},
					&brtypes.ContentBlockMemberText{Value: "The answer is 42"},
				},
			},
		},
		StopReason: brtypes.StopReasonEndTurn,
		Usage: &brtypes.TokenUsage{
			InputTokens:  aws.Int32(20),
			OutputTokens: aws.Int32(50),
			TotalTokens:  aws.Int32(70),
		},
	}

	resp := convertConverseOutput(out, "test-model")

	if resp.Choices[0].Message.Reasoning == nil {
		t.Fatal("expected reasoning content")
	}
	if *resp.Choices[0].Message.Reasoning != "Let me think about this..." {
		t.Errorf("unexpected reasoning: %s", *resp.Choices[0].Message.Reasoning)
	}
	if resp.Choices[0].Message.ReasoningSignature == nil {
		t.Fatal("expected reasoning signature")
	}
	if *resp.Choices[0].Message.ReasoningSignature != "abc123signature" {
		t.Errorf("unexpected signature: %s", *resp.Choices[0].Message.ReasoningSignature)
	}
	if *resp.Choices[0].Message.Content != "The answer is 42" {
		t.Errorf("unexpected content: %s", *resp.Choices[0].Message.Content)
	}
}

func TestConvertConverseOutput_WithToolCalls(t *testing.T) {
	out := &bedrockruntime.ConverseOutput{
		Output: &brtypes.ConverseOutputMemberMessage{
			Value: brtypes.Message{
				Role: brtypes.ConversationRoleAssistant,
				Content: []brtypes.ContentBlock{
					&brtypes.ContentBlockMemberToolUse{
						Value: brtypes.ToolUseBlock{
							ToolUseId: aws.String("call_123"),
							Name:      aws.String("get_weather"),
							Input:     nil,
						},
					},
				},
			},
		},
		StopReason: brtypes.StopReasonToolUse,
		Usage: &brtypes.TokenUsage{
			InputTokens:  aws.Int32(15),
			OutputTokens: aws.Int32(10),
			TotalTokens:  aws.Int32(25),
		},
	}

	resp := convertConverseOutput(out, "test-model")

	if len(resp.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.Choices[0].Message.ToolCalls))
	}
	tc := resp.Choices[0].Message.ToolCalls[0]
	if tc.ID != "call_123" {
		t.Errorf("expected tool call ID call_123, got %s", tc.ID)
	}
	if tc.Function.Name != "get_weather" {
		t.Errorf("expected function name get_weather, got %s", tc.Function.Name)
	}
	if *resp.Choices[0].FinishReason != "tool_calls" {
		t.Errorf("expected finish_reason tool_calls, got %s", *resp.Choices[0].FinishReason)
	}
}

func TestConvertConverseOutput_CacheMetrics(t *testing.T) {
	out := &bedrockruntime.ConverseOutput{
		Output: &brtypes.ConverseOutputMemberMessage{
			Value: brtypes.Message{
				Role: brtypes.ConversationRoleAssistant,
				Content: []brtypes.ContentBlock{
					&brtypes.ContentBlockMemberText{Value: "cached response"},
				},
			},
		},
		StopReason: brtypes.StopReasonEndTurn,
		Usage: &brtypes.TokenUsage{
			InputTokens:           aws.Int32(100),
			OutputTokens:          aws.Int32(20),
			TotalTokens:           aws.Int32(120),
			CacheReadInputTokens:  aws.Int32(80),
			CacheWriteInputTokens: aws.Int32(50),
		},
	}

	resp := convertConverseOutput(out, "test-model")

	if resp.Usage.PromptTokensDetails == nil {
		t.Fatal("expected prompt token details")
	}
	if resp.Usage.PromptTokensDetails.CacheReadTokens != 80 {
		t.Errorf("expected 80 cache read tokens, got %d", resp.Usage.PromptTokensDetails.CacheReadTokens)
	}
	if resp.Usage.PromptTokensDetails.CacheWriteTokens != 50 {
		t.Errorf("expected 50 cache write tokens, got %d", resp.Usage.PromptTokensDetails.CacheWriteTokens)
	}
	if resp.Usage.PromptTokensDetails.CachedTokens != 80 {
		t.Errorf("expected 80 cached tokens (alias), got %d", resp.Usage.PromptTokensDetails.CachedTokens)
	}
}

func TestMapStopReason(t *testing.T) {
	tests := []struct {
		input    brtypes.StopReason
		expected string
	}{
		{brtypes.StopReasonEndTurn, "stop"},
		{brtypes.StopReasonToolUse, "tool_calls"},
		{brtypes.StopReasonMaxTokens, "length"},
		{brtypes.StopReasonStopSequence, "stop"},
		{brtypes.StopReasonContentFiltered, "content_filter"},
		{brtypes.StopReason("unknown"), "stop"},
	}
	for _, tt := range tests {
		t.Run(string(tt.input), func(t *testing.T) {
			got := MapStopReason(tt.input)
			if got != tt.expected {
				t.Errorf("MapStopReason(%s) = %s, want %s", tt.input, got, tt.expected)
			}
		})
	}
}

func TestConvertConverseOutput_AdditionalFields(t *testing.T) {
	out := &bedrockruntime.ConverseOutput{
		Output: &brtypes.ConverseOutputMemberMessage{
			Value: brtypes.Message{
				Role:    brtypes.ConversationRoleAssistant,
				Content: []brtypes.ContentBlock{&brtypes.ContentBlockMemberText{Value: "ok"}},
			},
		},
		StopReason:                    brtypes.StopReasonEndTurn,
		Usage:                         &brtypes.TokenUsage{InputTokens: aws.Int32(1), OutputTokens: aws.Int32(1), TotalTokens: aws.Int32(2)},
		AdditionalModelResponseFields: document.NewLazyDocument(map[string]interface{}{"stop_sequence": "\n\n"}),
	}

	resp := convertConverseOutput(out, "test-model")
	if len(resp.AdditionalModelResponseFields) == 0 {
		t.Fatal("expected additional_model_response_fields")
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(resp.AdditionalModelResponseFields, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded["stop_sequence"] != "\n\n" {
		t.Fatalf("unexpected additional fields: %+v", decoded)
	}
}

func TestMergeAdditionalRequestFields(t *testing.T) {
	input := &ConverseInput{
		AdditionalModelRequestFields: document.NewLazyDocument(map[string]interface{}{"foo": "bar"}),
		ThinkingConfig:               &ThinkingConfig{Enabled: true, BudgetToken: 1024},
	}
	doc := mergeAdditionalRequestFields(input)
	if doc == nil {
		t.Fatal("expected merged document")
	}
	data, err := doc.MarshalSmithyDocument()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded["foo"] != "bar" {
		t.Fatalf("expected foo=bar, got %+v", decoded)
	}
	if _, ok := decoded["thinking"]; !ok {
		t.Fatalf("expected thinking key in merged document")
	}
}

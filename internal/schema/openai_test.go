package schema

import (
	"encoding/json"
	"testing"
)

func TestMessage_ContentString(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected string
	}{
		{"plain string", `"Hello world"`, "Hello world"},
		{"empty", `""`, ""},
		{"array returns empty", `[{"type":"text","text":"hi"}]`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := Message{Content: json.RawMessage(tt.content)}
			got := msg.ContentString()
			if got != tt.expected {
				t.Errorf("ContentString() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestMessage_ContentParts(t *testing.T) {
	t.Run("array of parts", func(t *testing.T) {
		msg := Message{Content: json.RawMessage(`[{"type":"text","text":"hello"},{"type":"image_url","image_url":{"url":"http://example.com/img.png"}}]`)}
		parts := msg.ContentParts()
		if len(parts) != 2 {
			t.Fatalf("expected 2 parts, got %d", len(parts))
		}
		if parts[0].Type != "text" || parts[0].Text != "hello" {
			t.Errorf("unexpected first part: %+v", parts[0])
		}
		if parts[1].Type != "image_url" || parts[1].ImageURL == nil {
			t.Errorf("unexpected second part: %+v", parts[1])
		}
	})

	t.Run("string wraps to text part", func(t *testing.T) {
		msg := Message{Content: json.RawMessage(`"just text"`)}
		parts := msg.ContentParts()
		if len(parts) != 1 {
			t.Fatalf("expected 1 part, got %d", len(parts))
		}
		if parts[0].Type != "text" || parts[0].Text != "just text" {
			t.Errorf("unexpected part: %+v", parts[0])
		}
	})
}

func TestChatRequest_ParseToolChoice(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"auto", `"auto"`, "auto"},
		{"required", `"required"`, "required"},
		{"none", `"none"`, "none"},
		{"specific tool", `{"type":"function","function":{"name":"get_weather"}}`, "tool:get_weather"},
		{"empty", ``, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := ChatRequest{ToolChoice: json.RawMessage(tt.input)}
			got := req.ParseToolChoice()
			if got != tt.expected {
				t.Errorf("ParseToolChoice() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestChatRequest_ParseExtraBody(t *testing.T) {
	t.Run("with prompt_caching false", func(t *testing.T) {
		req := ChatRequest{ExtraBody: json.RawMessage(`{"prompt_caching":false}`)}
		opts := req.ParseExtraBody()
		if opts.PromptCaching == nil || *opts.PromptCaching != false {
			t.Error("expected prompt_caching=false")
		}
	})

	t.Run("empty extra body", func(t *testing.T) {
		req := ChatRequest{}
		opts := req.ParseExtraBody()
		if opts.PromptCaching != nil {
			t.Error("expected nil prompt_caching")
		}
	})

	t.Run("extended fields parse", func(t *testing.T) {
		req := ChatRequest{ExtraBody: json.RawMessage(`{
			"prompt_caching":{"enabled":true,"system":true,"messages":false,"tools":true,"ttl":"1h"},
			"guardrail_config":{"guardrail_identifier":"gr-1","guardrail_version":"1","trace":"enabled","stream_processing_mode":"async"},
			"request_metadata":{"tenant":"acme"},
			"additional_model_request_fields":{"foo":"bar"},
			"additional_model_response_field_paths":["/stop_sequence"],
			"performance_config":{"latency":"optimized"},
			"service_tier":"priority"
		}`)}
		opts := req.ParseExtraBody()
		if opts.PromptCaching == nil || *opts.PromptCaching != true {
			t.Fatalf("expected prompt_caching enabled=true")
		}
		if opts.PromptCachingMessages == nil || *opts.PromptCachingMessages != false {
			t.Fatalf("expected prompt_caching.messages=false")
		}
		if opts.PromptCachingTTL != "1h" {
			t.Fatalf("expected ttl=1h, got %q", opts.PromptCachingTTL)
		}
		if opts.GuardrailIdentifier != "gr-1" || opts.GuardrailVersion != "1" {
			t.Fatalf("unexpected guardrail config: %+v", opts)
		}
		if opts.RequestMetadata["tenant"] != "acme" {
			t.Fatalf("expected request metadata tenant=acme")
		}
		if len(opts.AdditionalModelResponseFieldPaths) != 1 || opts.AdditionalModelResponseFieldPaths[0] != "/stop_sequence" {
			t.Fatalf("unexpected additional_model_response_field_paths: %v", opts.AdditionalModelResponseFieldPaths)
		}
		if opts.PerformanceLatency != "optimized" {
			t.Fatalf("expected performance latency optimized, got %q", opts.PerformanceLatency)
		}
		if opts.ServiceTier != "priority" {
			t.Fatalf("expected service_tier priority, got %q", opts.ServiceTier)
		}
	})
}

func TestResponseMessage_JSON(t *testing.T) {
	reasoning := "thinking..."
	sig := "sig123"
	content := "answer"
	msg := ResponseMessage{
		Role:               "assistant",
		Content:            &content,
		Reasoning:          &reasoning,
		ReasoningSignature: &sig,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if decoded["reasoning"] != "thinking..." {
		t.Errorf("expected reasoning in JSON, got %v", decoded["reasoning"])
	}
	if decoded["reasoning_signature"] != "sig123" {
		t.Errorf("expected reasoning_signature in JSON, got %v", decoded["reasoning_signature"])
	}
}

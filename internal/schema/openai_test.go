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

	t.Run("core fields parse", func(t *testing.T) {
		req := ChatRequest{ExtraBody: json.RawMessage(`{
			"prompt_caching":{"enabled":true,"system":true,"messages":false,"tools":true,"ttl":"1h"},
			"additional_model_request_fields":{"foo":"bar"}
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
		var fields map[string]string
		if err := json.Unmarshal(opts.AdditionalModelRequestFields, &fields); err != nil {
			t.Fatalf("failed to parse additional_model_request_fields: %v", err)
		}
		if fields["foo"] != "bar" {
			t.Fatalf("unexpected additional_model_request_fields: %+v", fields)
		}
	})
}

func TestChatRequest_ValidateExtraBodyCoreOnly(t *testing.T) {
	t.Run("allows core fields", func(t *testing.T) {
		req := ChatRequest{ExtraBody: json.RawMessage(`{
			"prompt_caching":{"enabled":true},
			"additional_model_request_fields":{"x":"y"}
		}`)}
		if err := req.ValidateExtraBodyCoreOnly(); err != nil {
			t.Fatalf("unexpected validation error: %v", err)
		}
	})

	t.Run("rejects unsupported fields", func(t *testing.T) {
		req := ChatRequest{ExtraBody: json.RawMessage(`{
			"guardrail_config":{"guardrail_identifier":"gr-1"},
			"service_tier":"priority"
		}`)}
		err := req.ValidateExtraBodyCoreOnly()
		if err == nil {
			t.Fatal("expected validation error")
		}
		if err.Error() != "unsupported extra_body fields (guardrail_config, service_tier); only prompt_caching and additional_model_request_fields are supported" {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestChatRequest_ValidateMessagesStrict(t *testing.T) {
	t.Run("valid user string content", func(t *testing.T) {
		req := ChatRequest{
			Messages: []Message{
				{Role: "user", Content: json.RawMessage(`"hello"`)},
			},
		}
		if err := req.ValidateMessagesStrict(); err != nil {
			t.Fatalf("unexpected validation error: %v", err)
		}
	})

	t.Run("rejects unknown role", func(t *testing.T) {
		req := ChatRequest{
			Messages: []Message{
				{Role: "moderator", Content: json.RawMessage(`"hello"`)},
			},
		}
		err := req.ValidateMessagesStrict()
		if err == nil || err.Error() != `messages[0].role "moderator" is not supported` {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects tool message without tool_call_id", func(t *testing.T) {
		req := ChatRequest{
			Messages: []Message{
				{Role: "tool", Content: json.RawMessage(`"output"`)},
			},
		}
		err := req.ValidateMessagesStrict()
		if err == nil || err.Error() != "messages[0].tool_call_id is required for tool role" {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects user content with invalid type", func(t *testing.T) {
		req := ChatRequest{
			Messages: []Message{
				{Role: "user", Content: json.RawMessage(`123`)},
			},
		}
		err := req.ValidateMessagesStrict()
		if err == nil || err.Error() != "messages[0].content must be a string or an array of content parts" {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects user content part with unknown type", func(t *testing.T) {
		req := ChatRequest{
			Messages: []Message{
				{Role: "user", Content: json.RawMessage(`[{"type":"audio","text":"x"}]`)},
			},
		}
		err := req.ValidateMessagesStrict()
		if err == nil || err.Error() != `messages[0].content[0].type "audio" is not supported` {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects user image_url content without url", func(t *testing.T) {
		req := ChatRequest{
			Messages: []Message{
				{Role: "user", Content: json.RawMessage(`[{"type":"image_url","image_url":{}}]`)},
			},
		}
		err := req.ValidateMessagesStrict()
		if err == nil || err.Error() != "messages[0].content[0].image_url.url is required" {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestReasoning_Unmarshal_TracksUnsupportedControls(t *testing.T) {
	var req ChatRequest
	err := json.Unmarshal([]byte(`{
		"model":"m1",
		"messages":[{"role":"user","content":"hi"}],
		"reasoning":{"exclude":true,"enabled":true,"effort":"high"}
	}`), &req)
	if err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}
	if req.Reasoning == nil {
		t.Fatal("expected reasoning object")
	}
	if req.Reasoning.Exclude == nil || !*req.Reasoning.Exclude {
		t.Fatal("expected exclude=true")
	}
	if !req.Reasoning.HasUnsupportedControls() {
		t.Fatal("expected unsupported reasoning controls to be tracked")
	}
	unsupported := req.Reasoning.UnsupportedControls()
	if len(unsupported) != 2 || unsupported[0] != "effort" || unsupported[1] != "enabled" {
		t.Fatalf("unexpected unsupported controls: %v", unsupported)
	}
}

func TestReasoning_Unmarshal_RejectsNonBooleanExclude(t *testing.T) {
	var req ChatRequest
	err := json.Unmarshal([]byte(`{
		"model":"m1",
		"messages":[{"role":"user","content":"hi"}],
		"reasoning":{"exclude":"yes"}
	}`), &req)
	if err == nil {
		t.Fatal("expected unmarshal error for non-boolean reasoning.exclude")
	}
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

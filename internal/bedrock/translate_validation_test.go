package bedrock

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/stratum/gateway/internal/schema"
)

func TestTranslateRequest_ValidationErrors(t *testing.T) {
	tests := []struct {
		name       string
		model      string
		extraBody  string
		wantErrSub string
	}{
		{
			name:       "additional request fields must be object",
			model:      "anthropic.claude-3-sonnet",
			extraBody:  `{"additional_model_request_fields":["bad"]}`,
			wantErrSub: "must be a JSON object",
		},
		{
			name:       "prompt cache ttl invalid",
			model:      "anthropic.claude-3-sonnet",
			extraBody:  `{"prompt_caching":{"enabled":true,"ttl":"2h"}}`,
			wantErrSub: "unsupported prompt_caching ttl",
		},
		{
			name:       "prompt cache ttl one hour unsupported",
			model:      "anthropic.claude-3-7-sonnet-20250219-v1:0",
			extraBody:  `{"prompt_caching":{"enabled":true,"ttl":"1h"}}`,
			wantErrSub: "ttl 1h is not supported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &schema.ChatRequest{
				Model:     tt.model,
				Messages:  []schema.Message{{Role: "user", Content: json.RawMessage(`"hello"`)}},
				ExtraBody: json.RawMessage(tt.extraBody),
			}
			_, err := TranslateRequest(req)
			if err == nil || !strings.Contains(err.Error(), tt.wantErrSub) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErrSub, err)
			}
		})
	}
}

func TestParseUserContent_Structured(t *testing.T) {
	img := base64.StdEncoding.EncodeToString([]byte{1, 2, 3})
	msg := schema.Message{
		Role: "user",
		Content: json.RawMessage(`[
			{"type":"text","text":"hello"},
			{"type":"image_url","image_url":{"url":"data:image/png;base64,` + img + `"}}
		]`),
	}

	blocks, err := parseUserContent(msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
	if _, ok := blocks[0].(*brtypes.ContentBlockMemberText); !ok {
		t.Fatalf("expected first block text")
	}
	if _, ok := blocks[1].(*brtypes.ContentBlockMemberImage); !ok {
		t.Fatalf("expected second block image")
	}
}

func TestParseUserContent_EmptyImageObject(t *testing.T) {
	msg := schema.Message{
		Role:    "user",
		Content: json.RawMessage(`[{"type":"image_url"}]`),
	}
	blocks, err := parseUserContent(msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(blocks) != 0 {
		t.Fatalf("expected no blocks for empty image url, got %d", len(blocks))
	}
}

func TestImageFormatMapping(t *testing.T) {
	if imageFormat("image/webp") != brtypes.ImageFormatWebp {
		t.Fatalf("expected webp")
	}
	if imageFormat("image/gif") != brtypes.ImageFormatGif {
		t.Fatalf("expected gif")
	}
	if imageFormat("image/jpeg") != brtypes.ImageFormatJpeg {
		t.Fatalf("expected jpeg")
	}
}

func TestTranslateRequest_MessageRoleCoverage(t *testing.T) {
	req := &schema.ChatRequest{
		Model: "anthropic.claude-3-sonnet",
		Messages: []schema.Message{
			{Role: "developer", Content: json.RawMessage(`"follow policy"`)},
			{Role: "user", Content: json.RawMessage(`"hello"`)},
			{
				Role:    "assistant",
				Content: json.RawMessage(`"calling tool"`),
				ToolCalls: []schema.ToolCall{
					{
						ID:   "call_1",
						Type: "function",
						Function: schema.ToolCallFunction{
							Name:      "lookup",
							Arguments: `{"q":"x"}`,
						},
					},
				},
			},
			{Role: "tool", ToolCallID: "call_1", Content: json.RawMessage(`"result"`)},
		},
	}
	in, err := TranslateRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(in.System) == 0 {
		t.Fatalf("expected developer/system content to map into system blocks")
	}
	if len(in.Messages) == 0 {
		t.Fatalf("expected translated messages")
	}
}

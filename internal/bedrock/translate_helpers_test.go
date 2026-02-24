package bedrock

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/stratum/gateway/internal/schema"
)

func TestParseAssistantContent_WithReasoningAndToolUse(t *testing.T) {
	reasoning := "thinking"
	signature := "sig-123"
	msg := schema.Message{
		Role:               "assistant",
		Content:            json.RawMessage(`"answer"`),
		Reasoning:          &reasoning,
		ReasoningSignature: &signature,
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
	}

	blocks := parseAssistantContent(msg)
	if len(blocks) != 3 {
		t.Fatalf("expected 3 blocks, got %d", len(blocks))
	}
	if _, ok := blocks[0].(*brtypes.ContentBlockMemberReasoningContent); !ok {
		t.Fatalf("expected first block to be reasoning")
	}
	if _, ok := blocks[1].(*brtypes.ContentBlockMemberText); !ok {
		t.Fatalf("expected second block to be text")
	}
	if tu, ok := blocks[2].(*brtypes.ContentBlockMemberToolUse); !ok {
		t.Fatalf("expected third block to be tool use")
	} else if tu.Value.Name == nil || *tu.Value.Name != "lookup" {
		t.Fatalf("unexpected tool name: %+v", tu.Value.Name)
	}
}

func TestParseToolResult(t *testing.T) {
	msg := schema.Message{
		Role:       "tool",
		ToolCallID: "call_1",
		Content:    json.RawMessage(`"tool output"`),
	}
	blocks := parseToolResult(msg)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if _, ok := blocks[0].(*brtypes.ContentBlockMemberToolResult); !ok {
		t.Fatalf("expected tool result block")
	}
}

func TestMergeConsecutiveMessages_PrependsUserWhenFirstNotUser(t *testing.T) {
	msgs := []brtypes.Message{
		{
			Role: brtypes.ConversationRoleAssistant,
			Content: []brtypes.ContentBlock{
				&brtypes.ContentBlockMemberText{Value: "a1"},
			},
		},
		{
			Role: brtypes.ConversationRoleAssistant,
			Content: []brtypes.ContentBlock{
				&brtypes.ContentBlockMemberText{Value: "a2"},
			},
		},
		{
			Role: brtypes.ConversationRoleUser,
			Content: []brtypes.ContentBlock{
				&brtypes.ContentBlockMemberText{Value: "u1"},
			},
		},
		{
			Role: brtypes.ConversationRoleUser,
			Content: []brtypes.ContentBlock{
				&brtypes.ContentBlockMemberText{Value: "u2"},
			},
		},
	}

	merged := mergeConsecutiveMessages(msgs)
	if len(merged) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(merged))
	}
	if merged[0].Role != brtypes.ConversationRoleUser {
		t.Fatalf("expected prepended user message first, got %s", merged[0].Role)
	}
}

func TestParseImageURL_HTTPGuards(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
		contentLen  string
		maxBytes    int64
		wantErrSub  string
	}{
		{
			name:        "reject non image content type",
			contentType: "text/plain",
			body:        "hello",
			maxBytes:    1024,
			wantErrSub:  "unsupported content type",
		},
		{
			name:        "reject oversized content-length header",
			contentType: "image/png",
			body:        "ok",
			contentLen:  "128",
			maxBytes:    16,
			wantErrSub:  "image too large",
		},
		{
			name:        "reject oversized response",
			contentType: "image/png",
			body:        strings.Repeat("a", 128),
			maxBytes:    16,
			wantErrSub:  "image too large",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tt.contentType)
				if tt.contentLen != "" {
					w.Header().Set("Content-Length", tt.contentLen)
				}
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			_, err := parseImageURL(srv.URL, TranslateConfig{
				AllowPrivateImageFetch: true,
				ImageMaxBytes:          tt.maxBytes,
				ImageFetchTimeout:      2 * time.Second,
			})
			if err == nil || !strings.Contains(err.Error(), tt.wantErrSub) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErrSub, err)
			}
		})
	}
}

func TestParseImageURL_TooManyRedirects(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, srv.URL, http.StatusFound)
	}))
	defer srv.Close()

	_, err := parseImageURL(srv.URL, TranslateConfig{
		AllowPrivateImageFetch: true,
		ImageMaxBytes:          1024,
		ImageFetchTimeout:      2 * time.Second,
	})
	if err == nil || !strings.Contains(err.Error(), "too many redirects") {
		t.Fatalf("expected too many redirects error, got %v", err)
	}
}

func TestParseDataURL_RejectsNonImageMediaType(t *testing.T) {
	_, _, err := parseDataURL("data:text/plain;base64,SGVsbG8=", 1024)
	if err == nil || !strings.Contains(err.Error(), "unsupported data URL media type") {
		t.Fatalf("expected media type validation error, got %v", err)
	}
}

func TestParseImageURL_HTTPImageSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte{1, 2, 3, 4})
	}))
	defer srv.Close()

	block, err := parseImageURL(srv.URL, TranslateConfig{
		AllowPrivateImageFetch: true,
		ImageMaxBytes:          1024,
		ImageFetchTimeout:      2 * time.Second,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	img, ok := block.(*brtypes.ContentBlockMemberImage)
	if !ok {
		t.Fatalf("expected image block")
	}
	if img.Value.Format != brtypes.ImageFormatPng {
		t.Fatalf("expected png format, got %s", img.Value.Format)
	}
}

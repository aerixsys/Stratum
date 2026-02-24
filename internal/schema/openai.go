package schema

import (
	"encoding/json"
	"strings"
)

// -- Chat Completion Request --

type ChatRequest struct {
	Model           string          `json:"model"`
	Messages        []Message       `json:"messages"`
	Temperature     *float32        `json:"temperature,omitempty"`
	TopP            *float32        `json:"top_p,omitempty"`
	MaxTokens       *int32          `json:"max_tokens,omitempty"`
	Stop            []string        `json:"stop,omitempty"`
	Stream          bool            `json:"stream,omitempty"`
	StreamOptions   *StreamOptions  `json:"stream_options,omitempty"`
	Tools           []Tool          `json:"tools,omitempty"`
	ToolChoice      json.RawMessage `json:"tool_choice,omitempty"`
	ReasoningEffort string          `json:"reasoning_effort,omitempty"`
	ExtraBody       json.RawMessage `json:"extra_body,omitempty"`
}

// StreamOptions controls streaming behavior.
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

// ParseToolChoice interprets the tool_choice field.
// Returns: "auto", "required", "none", or "tool:<name>".
func (r *ChatRequest) ParseToolChoice() string {
	if len(r.ToolChoice) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(r.ToolChoice, &s); err == nil {
		return s // "auto", "required", "none"
	}
	var obj struct {
		Type     string `json:"type"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if err := json.Unmarshal(r.ToolChoice, &obj); err == nil && obj.Function.Name != "" {
		return "tool:" + obj.Function.Name
	}
	return ""
}

// ParseExtraBody extracts known extra_body fields.
type ExtraBodyOptions struct {
	PromptCaching *bool

	PromptCachingSystem   *bool
	PromptCachingMessages *bool
	PromptCachingTools    *bool
	PromptCachingTTL      string

	GuardrailIdentifier string
	GuardrailVersion    string
	GuardrailTrace      string
	GuardrailStreamMode string

	RequestMetadata                   map[string]string
	AdditionalModelRequestFields      json.RawMessage
	AdditionalModelResponseFieldPaths []string

	PerformanceLatency string
	ServiceTier        string
}

func (r *ChatRequest) ParseExtraBody() ExtraBodyOptions {
	var opts ExtraBodyOptions
	if len(r.ExtraBody) == 0 {
		return opts
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(r.ExtraBody, &raw); err != nil {
		return opts
	}

	if pcRaw, ok := raw["prompt_caching"]; ok && len(pcRaw) > 0 {
		var enabled bool
		if err := json.Unmarshal(pcRaw, &enabled); err == nil {
			opts.PromptCaching = &enabled
			opts.PromptCachingSystem = &enabled
			opts.PromptCachingMessages = &enabled
			opts.PromptCachingTools = &enabled
		} else {
			var pc struct {
				Enabled  *bool  `json:"enabled,omitempty"`
				System   *bool  `json:"system,omitempty"`
				Messages *bool  `json:"messages,omitempty"`
				Tools    *bool  `json:"tools,omitempty"`
				TTL      string `json:"ttl,omitempty"`
			}
			if err := json.Unmarshal(pcRaw, &pc); err == nil {
				opts.PromptCaching = pc.Enabled
				opts.PromptCachingSystem = pc.System
				opts.PromptCachingMessages = pc.Messages
				opts.PromptCachingTools = pc.Tools
				opts.PromptCachingTTL = strings.TrimSpace(pc.TTL)

				if pc.Enabled != nil {
					if opts.PromptCachingSystem == nil {
						opts.PromptCachingSystem = pc.Enabled
					}
					if opts.PromptCachingMessages == nil {
						opts.PromptCachingMessages = pc.Enabled
					}
					if opts.PromptCachingTools == nil {
						opts.PromptCachingTools = pc.Enabled
					}
				}
			}
		}
	}

	if grRaw, ok := raw["guardrail_config"]; ok && len(grRaw) > 0 {
		var m map[string]interface{}
		if err := json.Unmarshal(grRaw, &m); err == nil {
			opts.GuardrailIdentifier = firstString(m,
				"guardrail_identifier", "guardrailIdentifier", "identifier")
			opts.GuardrailVersion = firstString(m,
				"guardrail_version", "guardrailVersion", "version")
			opts.GuardrailTrace = strings.ToLower(firstString(m,
				"trace", "guardrail_trace", "guardrailTrace"))
			opts.GuardrailStreamMode = strings.ToLower(firstString(m,
				"stream_processing_mode", "streamProcessingMode"))
		}
	}

	if rmRaw, ok := raw["request_metadata"]; ok && len(rmRaw) > 0 {
		_ = json.Unmarshal(rmRaw, &opts.RequestMetadata)
	}

	if amrfRaw, ok := raw["additional_model_request_fields"]; ok && len(amrfRaw) > 0 {
		opts.AdditionalModelRequestFields = amrfRaw
	}
	if amrpfRaw, ok := raw["additional_model_response_field_paths"]; ok && len(amrpfRaw) > 0 {
		_ = json.Unmarshal(amrpfRaw, &opts.AdditionalModelResponseFieldPaths)
	}

	if perfRaw, ok := raw["performance_config"]; ok && len(perfRaw) > 0 {
		var perf struct {
			Latency string `json:"latency"`
		}
		if err := json.Unmarshal(perfRaw, &perf); err == nil {
			opts.PerformanceLatency = strings.ToLower(strings.TrimSpace(perf.Latency))
		}
	}

	if serviceTierRaw, ok := raw["service_tier"]; ok && len(serviceTierRaw) > 0 {
		_ = json.Unmarshal(serviceTierRaw, &opts.ServiceTier)
		opts.ServiceTier = strings.ToLower(strings.TrimSpace(opts.ServiceTier))
	}

	return opts
}

func firstString(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			s, ok := v.(string)
			if ok {
				s = strings.TrimSpace(s)
				if s != "" {
					return s
				}
			}
		}
	}
	return ""
}

type Message struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"` // string or []ContentPart
	Name       string          `json:"name,omitempty"`
	ToolCalls  []ToolCall      `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	// Reasoning fields for multi-turn thinking block passthrough
	Reasoning          *string `json:"reasoning,omitempty"`
	ReasoningSignature *string `json:"reasoning_signature,omitempty"`
}

// ContentString extracts a plain text string from Content.
// Returns empty string if Content is an array.
func (m *Message) ContentString() string {
	if len(m.Content) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(m.Content, &s); err == nil {
		return s
	}
	return ""
}

// ContentParts extracts structured content parts from Content.
func (m *Message) ContentParts() []ContentPart {
	if len(m.Content) == 0 {
		return nil
	}
	var parts []ContentPart
	if err := json.Unmarshal(m.Content, &parts); err == nil {
		return parts
	}
	// If it's a string, wrap in single text part
	s := m.ContentString()
	if s != "" {
		return []ContentPart{{Type: "text", Text: s}}
	}
	return nil
}

type ContentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

type ImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type ToolCall struct {
	Index    *int             `json:"index,omitempty"`
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolCallFunction `json:"function"`
}

type ToolCallFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments"`
}

// -- Chat Completion Response --

type ChatResponse struct {
	ID                            string          `json:"id"`
	Object                        string          `json:"object"`
	Created                       int64           `json:"created"`
	Model                         string          `json:"model"`
	SystemFingerprint             string          `json:"system_fingerprint"`
	Choices                       []Choice        `json:"choices"`
	Usage                         *Usage          `json:"usage,omitempty"`
	AdditionalModelResponseFields json.RawMessage `json:"additional_model_response_fields,omitempty"`
}

type Choice struct {
	Index        int              `json:"index"`
	Message      *ResponseMessage `json:"message,omitempty"`
	Delta        *ResponseMessage `json:"delta,omitempty"`
	FinishReason *string          `json:"finish_reason"`
	Logprobs     *json.RawMessage `json:"logprobs"`
}

type ResponseMessage struct {
	Role      string     `json:"role,omitempty"`
	Content   *string    `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	// Reasoning content from extended thinking
	Reasoning          *string `json:"reasoning,omitempty"`
	ReasoningSignature *string `json:"reasoning_signature,omitempty"`
}

type Usage struct {
	PromptTokens            int                      `json:"prompt_tokens"`
	CompletionTokens        int                      `json:"completion_tokens"`
	TotalTokens             int                      `json:"total_tokens"`
	PromptTokensDetails     *PromptTokenDetails      `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails *CompletionTokensDetails `json:"completion_tokens_details,omitempty"`
}

type PromptTokenDetails struct {
	CachedTokens     int `json:"cached_tokens,omitempty"`
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"`
	CacheReadTokens  int `json:"cache_read_tokens,omitempty"`
}

type CompletionTokensDetails struct {
	ReasoningTokens int `json:"reasoning_tokens,omitempty"`
}

// -- Embeddings --

type EmbeddingRequest struct {
	Model          string          `json:"model"`
	Input          json.RawMessage `json:"input"` // string or []string
	EncodingFormat string          `json:"encoding_format,omitempty"`
}

// InputStrings extracts the input as a string slice.
func (r *EmbeddingRequest) InputStrings() []string {
	var s string
	if err := json.Unmarshal(r.Input, &s); err == nil {
		return []string{s}
	}
	var arr []string
	if err := json.Unmarshal(r.Input, &arr); err == nil {
		return arr
	}
	return nil
}

type EmbeddingResponse struct {
	Object string          `json:"object"`
	Data   []EmbeddingData `json:"data"`
	Model  string          `json:"model"`
	Usage  *EmbeddingUsage `json:"usage,omitempty"`
}

type EmbeddingData struct {
	Object    string    `json:"object"`
	Embedding []float64 `json:"embedding"`
	Index     int       `json:"index"`
}

type EmbeddingUsage struct {
	PromptTokens int `json:"prompt_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// -- Models --

type ModelList struct {
	Object string  `json:"object"`
	Data   []Model `json:"data"`
}

type Model struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

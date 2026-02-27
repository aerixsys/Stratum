package schema

import (
	"encoding/json"
	"fmt"
	"sort"
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
	Reasoning       *Reasoning      `json:"reasoning,omitempty"`
	ReasoningEffort string          `json:"reasoning_effort,omitempty"`
	ExtraBody       json.RawMessage `json:"extra_body,omitempty"`
}

var allowedExtraBodyFields = map[string]struct{}{
	"prompt_caching":                  {},
	"additional_model_request_fields": {},
}

type Reasoning struct {
	Exclude *bool `json:"exclude,omitempty"`

	unsupportedKeys []string `json:"-"`
}

func (r *Reasoning) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	r.Exclude = nil
	r.unsupportedKeys = r.unsupportedKeys[:0]

	for k, v := range raw {
		switch k {
		case "exclude":
			var exclude bool
			if err := json.Unmarshal(v, &exclude); err != nil {
				return fmt.Errorf("reasoning.exclude must be a boolean")
			}
			r.Exclude = &exclude
		default:
			r.unsupportedKeys = append(r.unsupportedKeys, k)
		}
	}
	sort.Strings(r.unsupportedKeys)
	return nil
}

func (r *Reasoning) HasUnsupportedControls() bool {
	return r != nil && len(r.unsupportedKeys) > 0
}

func (r *Reasoning) UnsupportedControls() []string {
	if r == nil || len(r.unsupportedKeys) == 0 {
		return nil
	}
	out := make([]string, len(r.unsupportedKeys))
	copy(out, r.unsupportedKeys)
	return out
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

// ValidateExtraBodyCoreOnly enforces minimal extra_body surface.
func (r *ChatRequest) ValidateExtraBodyCoreOnly() error {
	if len(r.ExtraBody) == 0 {
		return nil
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(r.ExtraBody, &raw); err != nil {
		return fmt.Errorf("extra_body must be a JSON object")
	}

	var unsupported []string
	for key := range raw {
		if _, ok := allowedExtraBodyFields[key]; !ok {
			unsupported = append(unsupported, key)
		}
	}
	if len(unsupported) == 0 {
		return nil
	}
	sort.Strings(unsupported)
	return fmt.Errorf(
		"unsupported extra_body fields (%s); only prompt_caching and additional_model_request_fields are supported",
		strings.Join(unsupported, ", "),
	)
}

// ParseExtraBody extracts supported extra_body fields.
type ExtraBodyOptions struct {
	PromptCaching *bool

	PromptCachingSystem   *bool
	PromptCachingMessages *bool
	PromptCachingTools    *bool
	PromptCachingTTL      string

	AdditionalModelRequestFields json.RawMessage
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

	if amrfRaw, ok := raw["additional_model_request_fields"]; ok && len(amrfRaw) > 0 {
		opts.AdditionalModelRequestFields = amrfRaw
	}

	return opts
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
	PromptTokens        int                 `json:"prompt_tokens"`
	CompletionTokens    int                 `json:"completion_tokens"`
	TotalTokens         int                 `json:"total_tokens"`
	PromptTokensDetails *PromptTokenDetails `json:"prompt_tokens_details,omitempty"`
}

type PromptTokenDetails struct {
	CachedTokens     int `json:"cached_tokens,omitempty"`
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"`
	CacheReadTokens  int `json:"cache_read_tokens,omitempty"`
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

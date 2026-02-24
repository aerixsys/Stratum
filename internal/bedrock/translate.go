package bedrock

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/stratum/gateway/internal/schema"
)

// ── Request Translation (OpenAI → Bedrock) ──

// TranslateRequest converts an OpenAI ChatRequest into Bedrock Converse input args.
func TranslateRequest(req *schema.ChatRequest, cfg TranslateConfig) (*ConverseInput, error) {
	extraOpts := req.ParseExtraBody()
	input := &ConverseInput{
		ModelID:      req.Model,
		IncludeUsage: req.StreamOptions != nil && req.StreamOptions.IncludeUsage,
	}

	thinkingEnabled := req.ReasoningEffort != "" && supportsThinking(req.Model)

	// Parse messages
	var systemBlocks []brtypes.SystemContentBlock
	var messages []brtypes.Message

	for _, msg := range req.Messages {
		switch msg.Role {
		case "system", "developer":
			text := msg.ContentString()
			if text != "" {
				systemBlocks = append(systemBlocks, &brtypes.SystemContentBlockMemberText{
					Value: text,
				})
			}

		case "user":
			content, err := parseUserContent(msg, cfg)
			if err != nil {
				return nil, fmt.Errorf("user message: %w", err)
			}
			messages = append(messages, brtypes.Message{
				Role:    brtypes.ConversationRoleUser,
				Content: content,
			})

		case "assistant":
			content := parseAssistantContent(msg)
			messages = append(messages, brtypes.Message{
				Role:    brtypes.ConversationRoleAssistant,
				Content: content,
			})

		case "tool":
			content := parseToolResult(msg)
			messages = append(messages, brtypes.Message{
				Role:    brtypes.ConversationRoleUser,
				Content: content,
			})
		}
	}

	// Merge consecutive same-role messages (Bedrock requirement)
	messages = mergeConsecutiveMessages(messages)

	// Determine if prompt caching should be enabled (config + per-request override)
	cachingEnabled := cfg.EnablePromptCaching
	if extraOpts.PromptCaching != nil {
		cachingEnabled = *extraOpts.PromptCaching
	}

	systemCaching := cachingEnabled
	messagesCaching := cachingEnabled
	toolsCaching := cachingEnabled
	if extraOpts.PromptCachingSystem != nil {
		systemCaching = *extraOpts.PromptCachingSystem
	}
	if extraOpts.PromptCachingMessages != nil {
		messagesCaching = *extraOpts.PromptCachingMessages
	}
	if extraOpts.PromptCachingTools != nil {
		toolsCaching = *extraOpts.PromptCachingTools
	}
	cacheTTL := strings.ToLower(strings.TrimSpace(extraOpts.PromptCachingTTL))
	if cacheTTL != "" && cacheTTL != string(brtypes.CacheTTLFiveMinutes) && cacheTTL != string(brtypes.CacheTTLOneHour) {
		return nil, fmt.Errorf("unsupported prompt_caching ttl %q (allowed: 5m, 1h)", cacheTTL)
	}
	if cacheTTL == string(brtypes.CacheTTLOneHour) && !supportsExtendedTTL(req.Model) {
		return nil, fmt.Errorf("prompt_caching ttl 1h is not supported by model %s", req.Model)
	}

	// Add prompt caching markers
	if cachingEnabled && supportsPromptCaching(req.Model) {
		systemBlocks = addCachePoints(systemBlocks, messages, req.Model, systemCaching, messagesCaching, cacheTTL)
	}

	input.System = systemBlocks
	input.Messages = messages

	// Inference configuration — apply model quirks
	infConfig := &brtypes.InferenceConfiguration{}
	hasInfConfig := false

	if req.MaxTokens != nil {
		infConfig.MaxTokens = req.MaxTokens
		hasInfConfig = true
	}

	// Thinking is incompatible with temperature, topP, topK
	if !thinkingEnabled {
		wantTemp := req.Temperature != nil
		wantTopP := req.TopP != nil

		// Claude Sonnet 4.5 / Haiku 4.5: cannot accept both temperature AND topP
		if wantTemp && wantTopP && isClaude45Sampling(req.Model) {
			// Keep temperature, drop topP
			wantTopP = false
		}

		if wantTemp {
			infConfig.Temperature = req.Temperature
			hasInfConfig = true
		}
		if wantTopP {
			infConfig.TopP = req.TopP
			hasInfConfig = true
		}
	}

	if len(req.Stop) > 0 {
		infConfig.StopSequences = req.Stop
		hasInfConfig = true
	}
	if hasInfConfig {
		input.InferenceConfig = infConfig
	}

	// Tool configuration with tool_choice support
	if len(req.Tools) > 0 {
		toolCfg := translateTools(req.Tools)

		// Add cache point to tools if caching enabled
		if toolsCaching && supportsToolCaching(req.Model) {
			cachePoint := brtypes.CachePointBlock{Type: brtypes.CachePointTypeDefault}
			if cacheTTL != "" {
				cachePoint.Ttl = brtypes.CacheTTL(cacheTTL)
			}
			toolCfg.Tools = append(toolCfg.Tools, &brtypes.ToolMemberCachePoint{
				Value: cachePoint,
			})
		}

		// Map OpenAI tool_choice → Bedrock ToolChoice
		choice := req.ParseToolChoice()
		switch {
		case choice == "auto":
			toolCfg.ToolChoice = &brtypes.ToolChoiceMemberAuto{Value: brtypes.AutoToolChoice{}}
		case choice == "required":
			toolCfg.ToolChoice = &brtypes.ToolChoiceMemberAny{Value: brtypes.AnyToolChoice{}}
		case choice == "none":
			// Don't send tool config at all
			toolCfg = nil
		case strings.HasPrefix(choice, "tool:"):
			name := strings.TrimPrefix(choice, "tool:")
			toolCfg.ToolChoice = &brtypes.ToolChoiceMemberTool{
				Value: brtypes.SpecificToolChoice{Name: aws.String(name)},
			}
		}

		input.ToolConfig = toolCfg
	}

	// Reasoning / Extended thinking via additionalModelRequestFields
	if thinkingEnabled {
		budget := reasoningBudget(req.ReasoningEffort)
		input.ThinkingConfig = &ThinkingConfig{
			Enabled:     true,
			BudgetToken: budget,
		}
	}

	if len(extraOpts.AdditionalModelRequestFields) > 0 {
		var reqFields interface{}
		if err := json.Unmarshal(extraOpts.AdditionalModelRequestFields, &reqFields); err != nil {
			return nil, fmt.Errorf("extra_body.additional_model_request_fields must be valid JSON: %w", err)
		}
		if _, ok := reqFields.(map[string]interface{}); !ok {
			return nil, fmt.Errorf("extra_body.additional_model_request_fields must be a JSON object")
		}
		input.AdditionalModelRequestFields = document.NewLazyDocument(reqFields)
	}
	if len(extraOpts.AdditionalModelResponseFieldPaths) > 0 {
		for _, p := range extraOpts.AdditionalModelResponseFieldPaths {
			if !strings.HasPrefix(p, "/") {
				return nil, fmt.Errorf("invalid additional_model_response_field_paths entry %q: must start with /", p)
			}
		}
		input.AdditionalModelResponseFieldPaths = extraOpts.AdditionalModelResponseFieldPaths
	}
	if len(extraOpts.RequestMetadata) > 0 {
		input.RequestMetadata = extraOpts.RequestMetadata
	}
	if extraOpts.PerformanceLatency != "" {
		switch extraOpts.PerformanceLatency {
		case string(brtypes.PerformanceConfigLatencyOptimized), string(brtypes.PerformanceConfigLatencyStandard):
			input.PerformanceLatency = extraOpts.PerformanceLatency
		default:
			return nil, fmt.Errorf("unsupported performance_config.latency %q", extraOpts.PerformanceLatency)
		}
	}
	if extraOpts.ServiceTier != "" {
		switch extraOpts.ServiceTier {
		case string(brtypes.ServiceTierTypeDefault),
			string(brtypes.ServiceTierTypePriority),
			string(brtypes.ServiceTierTypeFlex),
			string(brtypes.ServiceTierTypeReserved):
			input.ServiceTier = extraOpts.ServiceTier
		default:
			return nil, fmt.Errorf("unsupported service_tier %q", extraOpts.ServiceTier)
		}
	}
	if extraOpts.GuardrailIdentifier != "" || extraOpts.GuardrailVersion != "" {
		if extraOpts.GuardrailIdentifier == "" || extraOpts.GuardrailVersion == "" {
			return nil, fmt.Errorf("guardrail_config requires both identifier and version")
		}
		input.GuardrailConfig = &GuardrailConfig{
			Identifier:       extraOpts.GuardrailIdentifier,
			Version:          extraOpts.GuardrailVersion,
			Trace:            extraOpts.GuardrailTrace,
			StreamProcessing: extraOpts.GuardrailStreamMode,
		}
	}

	return input, nil
}

// ConverseInput holds translated Bedrock request args.
type ConverseInput struct {
	ModelID                           string
	Messages                          []brtypes.Message
	System                            []brtypes.SystemContentBlock
	InferenceConfig                   *brtypes.InferenceConfiguration
	ToolConfig                        *brtypes.ToolConfiguration
	ThinkingConfig                    *ThinkingConfig
	GuardrailConfig                   *GuardrailConfig
	RequestMetadata                   map[string]string
	AdditionalModelRequestFields      document.Interface
	AdditionalModelResponseFieldPaths []string
	PerformanceLatency                string
	ServiceTier                       string
	IncludeUsage                      bool
}

// ThinkingConfig holds extended thinking parameters.
type ThinkingConfig struct {
	Enabled     bool
	BudgetToken int32
}

// GuardrailConfig holds guardrail request options.
type GuardrailConfig struct {
	Identifier       string
	Version          string
	Trace            string
	StreamProcessing string
}

type TranslateConfig struct {
	EnablePromptCaching    bool
	AllowPrivateImageFetch bool
	ImageMaxBytes          int64
	ImageFetchTimeout      time.Duration
}

// ── User Content Parsing ──

func parseUserContent(msg schema.Message, cfg TranslateConfig) ([]brtypes.ContentBlock, error) {
	parts := msg.ContentParts()
	if len(parts) == 0 {
		return []brtypes.ContentBlock{
			&brtypes.ContentBlockMemberText{Value: ""},
		}, nil
	}

	var blocks []brtypes.ContentBlock
	for _, part := range parts {
		switch part.Type {
		case "text":
			blocks = append(blocks, &brtypes.ContentBlockMemberText{Value: part.Text})
		case "image_url":
			if part.ImageURL == nil {
				continue
			}
			imgBlock, err := parseImageURL(part.ImageURL.URL, cfg)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, imgBlock)
		}
	}
	return blocks, nil
}

func parseImageURL(urlStr string, cfg TranslateConfig) (brtypes.ContentBlock, error) {
	maxBytes := cfg.ImageMaxBytes
	if maxBytes <= 0 {
		maxBytes = 5 * 1024 * 1024
	}
	timeout := cfg.ImageFetchTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	// Handle base64 data URLs
	if strings.HasPrefix(urlStr, "data:") {
		mediaType, data, err := parseDataURL(urlStr, maxBytes)
		if err != nil {
			return nil, err
		}
		format := imageFormat(mediaType)
		return &brtypes.ContentBlockMemberImage{
			Value: brtypes.ImageBlock{
				Format: format,
				Source: &brtypes.ImageSourceMemberBytes{
					Value: data,
				},
			},
		}, nil
	}

	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return nil, fmt.Errorf("invalid image URL: %w", err)
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return nil, fmt.Errorf("unsupported image URL scheme %q", parsedURL.Scheme)
	}
	if parsedURL.Hostname() == "" {
		return nil, fmt.Errorf("image URL host is required")
	}
	if !cfg.AllowPrivateImageFetch {
		if err := validateRemoteImageHost(parsedURL.Hostname()); err != nil {
			return nil, err
		}
	}

	client := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("fetch image: too many redirects")
			}
			if req.URL == nil || (req.URL.Scheme != "http" && req.URL.Scheme != "https") {
				return fmt.Errorf("fetch image: redirect to unsupported scheme")
			}
			if !cfg.AllowPrivateImageFetch {
				if err := validateRemoteImageHost(req.URL.Hostname()); err != nil {
					return fmt.Errorf("fetch image: redirect blocked: %w", err)
				}
			}
			return nil
		},
	}
	req, err := http.NewRequest(http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, fmt.Errorf("build image request: %w", err)
	}
	req.Header.Set("User-Agent", "stratum-gateway/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch image: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("fetch image: upstream returned status %d", resp.StatusCode)
	}
	if resp.ContentLength > maxBytes && resp.ContentLength > 0 {
		return nil, fmt.Errorf("image too large: max %d bytes", maxBytes)
	}

	limited := io.LimitReader(resp.Body, maxBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read image: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("image too large: max %d bytes", maxBytes)
	}

	ct := resp.Header.Get("Content-Type")
	if ct != "" && !strings.HasPrefix(strings.ToLower(ct), "image/") {
		return nil, fmt.Errorf("unsupported content type %q", ct)
	}
	format := imageFormat(ct)
	return &brtypes.ContentBlockMemberImage{
		Value: brtypes.ImageBlock{
			Format: format,
			Source: &brtypes.ImageSourceMemberBytes{
				Value: data,
			},
		},
	}, nil
}

func parseDataURL(dataURL string, maxBytes int64) (mediaType string, data []byte, err error) {
	// data:image/png;base64,iVBOR...
	parts := strings.SplitN(dataURL, ",", 2)
	if len(parts) != 2 {
		return "", nil, fmt.Errorf("invalid data URL")
	}
	meta := parts[0] // data:image/png;base64
	meta = strings.TrimPrefix(meta, "data:")
	metaParts := strings.Split(meta, ";")
	mediaType = metaParts[0]
	if mediaType == "" {
		return "", nil, fmt.Errorf("invalid data URL media type")
	}
	if !strings.HasPrefix(strings.ToLower(mediaType), "image/") {
		return "", nil, fmt.Errorf("unsupported data URL media type %q", mediaType)
	}
	if maxBytes > 0 {
		// Rough pre-check to reduce memory spikes on very large data URLs.
		maxBase64Len := int((maxBytes*4)/3) + 8
		if len(parts[1]) > maxBase64Len {
			return "", nil, fmt.Errorf("image too large: max %d bytes", maxBytes)
		}
	}

	decoded, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return "", nil, fmt.Errorf("base64 decode: %w", err)
	}
	if maxBytes > 0 && int64(len(decoded)) > maxBytes {
		return "", nil, fmt.Errorf("image too large: max %d bytes", maxBytes)
	}
	return mediaType, decoded, nil
}

func validateRemoteImageHost(host string) error {
	if host == "" {
		return fmt.Errorf("image URL host is required")
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("resolve image host: %w", err)
	}
	for _, ip := range ips {
		if isPrivateOrLocalIP(ip) {
			return fmt.Errorf("image URL host resolves to private or local address")
		}
	}
	return nil
}

func isPrivateOrLocalIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		if v4[0] == 10 {
			return true
		}
		if v4[0] == 172 && v4[1] >= 16 && v4[1] <= 31 {
			return true
		}
		if v4[0] == 192 && v4[1] == 168 {
			return true
		}
		if v4[0] == 169 && v4[1] == 254 {
			return true
		}
	}
	if len(ip) == net.IPv6len {
		// Unique local and link-local prefixes.
		if (ip[0]&0xfe) == 0xfc || (ip[0] == 0xfe && (ip[1]&0xc0) == 0x80) {
			return true
		}
	}
	return false
}

func imageFormat(contentType string) brtypes.ImageFormat {
	ct := strings.ToLower(contentType)
	switch {
	case strings.Contains(ct, "png"):
		return brtypes.ImageFormatPng
	case strings.Contains(ct, "gif"):
		return brtypes.ImageFormatGif
	case strings.Contains(ct, "webp"):
		return brtypes.ImageFormatWebp
	default:
		return brtypes.ImageFormatJpeg
	}
}

// ── Assistant Content Parsing ──

func parseAssistantContent(msg schema.Message) []brtypes.ContentBlock {
	var blocks []brtypes.ContentBlock

	// Pass back reasoning/thinking blocks for multi-turn continuity
	if msg.Reasoning != nil && *msg.Reasoning != "" {
		reasoningBlock := brtypes.ReasoningTextBlock{
			Text: msg.Reasoning,
		}
		if msg.ReasoningSignature != nil && *msg.ReasoningSignature != "" {
			reasoningBlock.Signature = msg.ReasoningSignature
		}
		blocks = append(blocks, &brtypes.ContentBlockMemberReasoningContent{
			Value: &brtypes.ReasoningContentBlockMemberReasoningText{
				Value: reasoningBlock,
			},
		})
	}

	text := msg.ContentString()
	if text != "" {
		blocks = append(blocks, &brtypes.ContentBlockMemberText{Value: text})
	}

	// Tool calls
	for _, tc := range msg.ToolCalls {
		var inputMap map[string]interface{}
		_ = json.Unmarshal([]byte(tc.Function.Arguments), &inputMap)
		if inputMap == nil {
			inputMap = make(map[string]interface{})
		}
		blocks = append(blocks, &brtypes.ContentBlockMemberToolUse{
			Value: brtypes.ToolUseBlock{
				ToolUseId: aws.String(tc.ID),
				Name:      aws.String(tc.Function.Name),
				Input:     document.NewLazyDocument(inputMap),
			},
		})
	}

	if len(blocks) == 0 {
		blocks = append(blocks, &brtypes.ContentBlockMemberText{Value: ""})
	}
	return blocks
}

// ── Tool Result Parsing ──

func parseToolResult(msg schema.Message) []brtypes.ContentBlock {
	text := msg.ContentString()
	return []brtypes.ContentBlock{
		&brtypes.ContentBlockMemberToolResult{
			Value: brtypes.ToolResultBlock{
				ToolUseId: aws.String(msg.ToolCallID),
				Content: []brtypes.ToolResultContentBlock{
					&brtypes.ToolResultContentBlockMemberText{Value: text},
				},
			},
		},
	}
}

// ── Tool Configuration Translation ──

func translateTools(tools []schema.Tool) *brtypes.ToolConfiguration {
	var specs []brtypes.Tool
	for _, t := range tools {
		if t.Type != "function" {
			continue
		}
		var inputSchema map[string]interface{}
		if t.Function.Parameters != nil {
			_ = json.Unmarshal(t.Function.Parameters, &inputSchema)
		}
		if inputSchema == nil {
			inputSchema = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
		}
		specs = append(specs, &brtypes.ToolMemberToolSpec{
			Value: brtypes.ToolSpecification{
				Name:        aws.String(t.Function.Name),
				Description: aws.String(t.Function.Description),
				InputSchema: &brtypes.ToolInputSchemaMemberJson{Value: document.NewLazyDocument(inputSchema)},
			},
		})
	}
	return &brtypes.ToolConfiguration{Tools: specs}
}

// ── Message Merging (Bedrock requires alternating roles) ──

func mergeConsecutiveMessages(msgs []brtypes.Message) []brtypes.Message {
	if len(msgs) == 0 {
		return msgs
	}

	var merged []brtypes.Message
	current := msgs[0]

	for i := 1; i < len(msgs); i++ {
		if msgs[i].Role == current.Role {
			current.Content = append(current.Content, msgs[i].Content...)
		} else {
			merged = append(merged, current)
			current = msgs[i]
		}
	}
	merged = append(merged, current)

	// Ensure first message is user role
	if len(merged) > 0 && merged[0].Role != brtypes.ConversationRoleUser {
		merged = append([]brtypes.Message{{
			Role:    brtypes.ConversationRoleUser,
			Content: []brtypes.ContentBlock{&brtypes.ContentBlockMemberText{Value: "Continue"}},
		}}, merged...)
	}

	return merged
}

// ── Prompt Caching ──

func supportsPromptCaching(modelID string) bool {
	m := strings.ToLower(modelID)
	return strings.Contains(m, "claude") || strings.Contains(m, "nova")
}

func addCachePoints(system []brtypes.SystemContentBlock, messages []brtypes.Message, modelID string, systemEnabled, messagesEnabled bool, ttl string) []brtypes.SystemContentBlock {
	_ = modelID
	cachePoint := brtypes.CachePointBlock{Type: brtypes.CachePointTypeDefault}
	if ttl != "" {
		cachePoint.Ttl = brtypes.CacheTTL(ttl)
	}

	// Add cache point after the last system content block
	if systemEnabled && len(system) > 0 {
		system = append(system, &brtypes.SystemContentBlockMemberCachePoint{
			Value: cachePoint,
		})
	}

	// Add cache point after the last user message's content
	if messagesEnabled {
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i].Role == brtypes.ConversationRoleUser {
				messages[i].Content = append(messages[i].Content, &brtypes.ContentBlockMemberCachePoint{
					Value: cachePoint,
				})
				break
			}
		}
	}

	return system
}

// ── Response Translation (Bedrock → OpenAI) ──

// MapStopReason converts Bedrock stop reason to OpenAI finish_reason.
func MapStopReason(reason brtypes.StopReason) string {
	switch reason {
	case brtypes.StopReasonEndTurn:
		return "stop"
	case brtypes.StopReasonToolUse:
		return "tool_calls"
	case brtypes.StopReasonMaxTokens:
		return "length"
	case brtypes.StopReasonStopSequence:
		return "stop"
	case brtypes.StopReasonContentFiltered:
		return "content_filter"
	default:
		return "stop"
	}
}

// ── Reasoning Budget ──

func reasoningBudget(effort string) int32 {
	switch strings.ToLower(effort) {
	case "low":
		return 1024
	case "medium":
		return 4096
	case "high":
		return 16384
	default:
		return 4096
	}
}

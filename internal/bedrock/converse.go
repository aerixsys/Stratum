package bedrock

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/google/uuid"
	"github.com/stratum/gateway/internal/schema"
)

const reasoningTokenEstimateMethod = "char_ratio_v1"

// Converse calls the synchronous Bedrock Converse API and returns an OpenAI ChatResponse.
func (c *Client) Converse(ctx context.Context, input *ConverseInput) (*schema.ChatResponse, error) {
	converseInput := &bedrockruntime.ConverseInput{
		ModelId:  aws.String(input.ModelID),
		Messages: input.Messages,
	}
	if len(input.System) > 0 {
		converseInput.System = input.System
	}
	if input.InferenceConfig != nil {
		converseInput.InferenceConfig = input.InferenceConfig
	}
	if input.ToolConfig != nil {
		converseInput.ToolConfig = input.ToolConfig
	}
	applyConverseFields(converseInput, input)

	out, err := c.BedrockRuntime.Converse(ctx, converseInput)
	if err != nil {
		return nil, fmt.Errorf("Bedrock Converse: %w", err)
	}

	return convertConverseOutput(out, input.ModelID), nil
}

func convertConverseOutput(out *bedrockruntime.ConverseOutput, modelID string) *schema.ChatResponse {
	msgID := "chatcmpl-" + shortUUID()
	now := time.Now().Unix()

	var content string
	var reasoning string
	var reasoningTextLen int
	var reasoningSignature string
	var toolCalls []schema.ToolCall

	if out.Output != nil {
		if msgOutput, ok := out.Output.(*brtypes.ConverseOutputMemberMessage); ok {
			for _, block := range msgOutput.Value.Content {
				switch b := block.(type) {
				case *brtypes.ContentBlockMemberText:
					content += b.Value
				case *brtypes.ContentBlockMemberToolUse:
					argsBytes, _ := json.Marshal(b.Value.Input)
					toolCalls = append(toolCalls, schema.ToolCall{
						ID:   aws.ToString(b.Value.ToolUseId),
						Type: "function",
						Function: schema.ToolCallFunction{
							Name:      aws.ToString(b.Value.Name),
							Arguments: string(argsBytes),
						},
					})
				case *brtypes.ContentBlockMemberReasoningContent:
					switch rc := b.Value.(type) {
					case *brtypes.ReasoningContentBlockMemberReasoningText:
						if rc.Value.Text != nil {
							reasoning += *rc.Value.Text
							reasoningTextLen += len(*rc.Value.Text)
						}
						if rc.Value.Signature != nil {
							reasoningSignature = *rc.Value.Signature
						}
					case *brtypes.ReasoningContentBlockMemberRedactedContent:
						// Encrypted thinking — note it but can't display
						if len(rc.Value) > 0 {
							reasoning += "[redacted thinking content]"
						}
					}
				}
			}
		}
	}

	finishReason := MapStopReason(out.StopReason)

	var usage *schema.Usage
	if out.Usage != nil {
		usage = &schema.Usage{
			PromptTokens:     int(aws.ToInt32(out.Usage.InputTokens)),
			CompletionTokens: int(aws.ToInt32(out.Usage.OutputTokens)),
			TotalTokens:      int(aws.ToInt32(out.Usage.TotalTokens)),
		}

		// Cache read/write metrics
		promptDetails := &schema.PromptTokenDetails{}
		hasCacheDetails := false

		if out.Usage.CacheReadInputTokens != nil {
			promptDetails.CacheReadTokens = int(aws.ToInt32(out.Usage.CacheReadInputTokens))
			promptDetails.CachedTokens = promptDetails.CacheReadTokens
			hasCacheDetails = true
		}
		if out.Usage.CacheWriteInputTokens != nil {
			promptDetails.CacheWriteTokens = int(aws.ToInt32(out.Usage.CacheWriteInputTokens))
			hasCacheDetails = true
		}
		if hasCacheDetails {
			usage.PromptTokensDetails = promptDetails
		}

		// Estimate reasoning tokens from character ratio.
		// Bedrock outputTokens includes both reasoning + output combined.
		// We use only real reasoning text length (not redacted placeholders).
		if reasoningTextLen > 0 {
			estimated := estimateReasoningTokens(usage.CompletionTokens, reasoningTextLen, len(content))
			usage.CompletionTokensDetails = estimatedCompletionTokenDetails(estimated)
		}
	}

	var contentPtr *string
	if content != "" {
		contentPtr = &content
	}
	var reasoningPtr *string
	if reasoning != "" {
		reasoningPtr = &reasoning
	}
	var sigPtr *string
	if reasoningSignature != "" {
		sigPtr = &reasoningSignature
	}

	msg := &schema.ResponseMessage{
		Role:               "assistant",
		Content:            contentPtr,
		ToolCalls:          toolCalls,
		Reasoning:          reasoningPtr,
		ReasoningSignature: sigPtr,
	}

	resp := &schema.ChatResponse{
		ID:                msgID,
		Object:            "chat.completion",
		Created:           now,
		Model:             modelID,
		SystemFingerprint: "fp",
		Choices: []schema.Choice{
			{
				Index:        0,
				Message:      msg,
				FinishReason: &finishReason,
			},
		},
		Usage: usage,
	}
	if out.AdditionalModelResponseFields != nil {
		resp.AdditionalModelResponseFields = smithyDocumentToRaw(out.AdditionalModelResponseFields)
	}
	return resp
}

// ConverseStream calls the streaming Bedrock ConverseStream API.
// It returns a channel that emits SSE-formatted byte slices.
// Errors are sent as SSE error events in the stream.
func (c *Client) ConverseStream(ctx context.Context, input *ConverseInput) <-chan []byte {
	dataCh := make(chan []byte, 64)

	go func() {
		defer close(dataCh)

		streamInput := &bedrockruntime.ConverseStreamInput{
			ModelId:  aws.String(input.ModelID),
			Messages: input.Messages,
		}
		if len(input.System) > 0 {
			streamInput.System = input.System
		}
		if input.InferenceConfig != nil {
			streamInput.InferenceConfig = input.InferenceConfig
		}
		if input.ToolConfig != nil {
			streamInput.ToolConfig = input.ToolConfig
		}
		applyConverseStreamFields(streamInput, input)

		out, err := c.BedrockRuntime.ConverseStream(ctx, streamInput)
		if err != nil {
			emitStreamErrorChunk(func(b []byte) { dataCh <- b })
			emitDone(func(b []byte) { dataCh <- b })
			return
		}

		msgID := "chatcmpl-" + shortUUID()
		now := time.Now().Unix()
		stream := out.GetStream()
		if stream == nil {
			emitStreamErrorChunk(func(b []byte) { dataCh <- b })
			emitDone(func(b []byte) { dataCh <- b })
			return
		}
		defer stream.Close()

		emitConverseSSEStream(
			input,
			msgID,
			now,
			stream.Events(),
			stream.Err,
			func(b []byte) { dataCh <- b },
		)
	}()

	return dataCh
}

type streamSSEState struct {
	currentToolCallIndex int
	finishReason         string
	finishChunkSent      bool
	pendingUsage         *schema.Usage
	reasoningTextLen     int
	contentLen           int
}

func emitConverseSSEStream(
	input *ConverseInput,
	msgID string,
	now int64,
	events <-chan brtypes.ConverseStreamOutput,
	streamErr func() error,
	emit func([]byte),
) {
	emitRoleChunk(msgID, now, input.ModelID, emit)

	state := &streamSSEState{}
	for event := range events {
		handleConverseStreamEvent(input, msgID, now, event, state, emit)
	}

	if streamErr != nil {
		if err := streamErr(); err != nil {
			log.Printf("[stream] Error: %v", err)
			if !state.finishChunkSent {
				emitStreamErrorChunk(emit)
				emitDone(emit)
				return
			}
		}
	}

	if !state.finishChunkSent {
		finishReason := state.finishReason
		if finishReason == "" {
			finishReason = "stop"
		}
		emitSSEValue(schema.ChatResponse{
			ID:      msgID,
			Object:  "chat.completion.chunk",
			Created: now,
			Model:   input.ModelID,
			Choices: []schema.Choice{{
				Index:        0,
				Delta:        &schema.ResponseMessage{},
				FinishReason: &finishReason,
			}},
		}, emit)
	}

	if state.pendingUsage != nil && input.IncludeUsage {
		emitSSEValue(schema.ChatResponse{
			ID:      msgID,
			Object:  "chat.completion.chunk",
			Created: now,
			Model:   input.ModelID,
			Choices: []schema.Choice{},
			Usage:   state.pendingUsage,
		}, emit)
	}

	emitDone(emit)
}

func handleConverseStreamEvent(
	input *ConverseInput,
	msgID string,
	now int64,
	event brtypes.ConverseStreamOutput,
	state *streamSSEState,
	emit func([]byte),
) {
	switch e := event.(type) {
	case *brtypes.ConverseStreamOutputMemberContentBlockDelta:
		delta := e.Value.Delta
		switch d := delta.(type) {
		case *brtypes.ContentBlockDeltaMemberText:
			state.contentLen += len(d.Value)
			emitSSEValue(schema.ChatResponse{
				ID:      msgID,
				Object:  "chat.completion.chunk",
				Created: now,
				Model:   input.ModelID,
				Choices: []schema.Choice{{
					Index: 0,
					Delta: &schema.ResponseMessage{Content: aws.String(d.Value)},
				}},
			}, emit)
		case *brtypes.ContentBlockDeltaMemberReasoningContent:
			switch rd := d.Value.(type) {
			case *brtypes.ReasoningContentBlockDeltaMemberText:
				state.reasoningTextLen += len(rd.Value)
				emitSSEValue(schema.ChatResponse{
					ID:      msgID,
					Object:  "chat.completion.chunk",
					Created: now,
					Model:   input.ModelID,
					Choices: []schema.Choice{{
						Index: 0,
						Delta: &schema.ResponseMessage{Reasoning: aws.String(rd.Value)},
					}},
				}, emit)
			case *brtypes.ReasoningContentBlockDeltaMemberSignature:
				emitSSEValue(schema.ChatResponse{
					ID:      msgID,
					Object:  "chat.completion.chunk",
					Created: now,
					Model:   input.ModelID,
					Choices: []schema.Choice{{
						Index: 0,
						Delta: &schema.ResponseMessage{ReasoningSignature: aws.String(rd.Value)},
					}},
				}, emit)
			case *brtypes.ReasoningContentBlockDeltaMemberRedactedContent:
				_ = rd
			}
		case *brtypes.ContentBlockDeltaMemberToolUse:
			idx := state.currentToolCallIndex
			emitSSEValue(schema.ChatResponse{
				ID:      msgID,
				Object:  "chat.completion.chunk",
				Created: now,
				Model:   input.ModelID,
				Choices: []schema.Choice{{
					Index: 0,
					Delta: &schema.ResponseMessage{
						ToolCalls: []schema.ToolCall{{
							Index: &idx,
							Type:  "function",
							Function: schema.ToolCallFunction{
								Arguments: aws.ToString(d.Value.Input),
							},
						}},
					},
				}},
			}, emit)
		}
	case *brtypes.ConverseStreamOutputMemberContentBlockStart:
		start := e.Value.Start
		if tu, ok := start.(*brtypes.ContentBlockStartMemberToolUse); ok {
			idx := state.currentToolCallIndex
			emitSSEValue(schema.ChatResponse{
				ID:      msgID,
				Object:  "chat.completion.chunk",
				Created: now,
				Model:   input.ModelID,
				Choices: []schema.Choice{{
					Index: 0,
					Delta: &schema.ResponseMessage{
						ToolCalls: []schema.ToolCall{{
							Index: &idx,
							ID:    aws.ToString(tu.Value.ToolUseId),
							Type:  "function",
							Function: schema.ToolCallFunction{
								Name: aws.ToString(tu.Value.Name),
							},
						}},
					},
				}},
			}, emit)
			state.currentToolCallIndex++
		}
	case *brtypes.ConverseStreamOutputMemberMessageStop:
		state.finishReason = MapStopReason(e.Value.StopReason)
		emitSSEValue(schema.ChatResponse{
			ID:      msgID,
			Object:  "chat.completion.chunk",
			Created: now,
			Model:   input.ModelID,
			Choices: []schema.Choice{{
				Index:        0,
				Delta:        &schema.ResponseMessage{},
				FinishReason: &state.finishReason,
			}},
		}, emit)
		state.finishChunkSent = true
	case *brtypes.ConverseStreamOutputMemberMetadata:
		if !input.IncludeUsage || e.Value.Usage == nil {
			return
		}
		usage := buildUsageFromTokenUsage(e.Value.Usage)
		// Estimate reasoning tokens from accumulated text lengths
		if state.reasoningTextLen > 0 && usage != nil {
			estimated := estimateReasoningTokens(usage.CompletionTokens, state.reasoningTextLen, state.contentLen)
			usage.CompletionTokensDetails = estimatedCompletionTokenDetails(estimated)
		}
		state.pendingUsage = usage
	}
}

func emitRoleChunk(msgID string, now int64, modelID string, emit func([]byte)) {
	emitSSEValue(schema.ChatResponse{
		ID:      msgID,
		Object:  "chat.completion.chunk",
		Created: now,
		Model:   modelID,
		Choices: []schema.Choice{
			{
				Index: 0,
				Delta: &schema.ResponseMessage{Role: "assistant"},
			},
		},
	}, emit)
}

func emitSSEValue(v interface{}, emit func([]byte)) {
	data, err := marshalSSE(v)
	if err != nil {
		return
	}
	emit(data)
}

func emitStreamErrorChunk(emit func([]byte)) {
	emitSSEValue(map[string]any{
		"error": map[string]string{
			"message": "Upstream model invocation failed",
		},
	}, emit)
}

func emitDone(emit func([]byte)) {
	emit([]byte("data: [DONE]\n\n"))
}

func buildUsageFromTokenUsage(usage *brtypes.TokenUsage) *schema.Usage {
	if usage == nil {
		return nil
	}
	out := &schema.Usage{
		PromptTokens:     int(aws.ToInt32(usage.InputTokens)),
		CompletionTokens: int(aws.ToInt32(usage.OutputTokens)),
		TotalTokens:      int(aws.ToInt32(usage.TotalTokens)),
	}

	promptDetails := &schema.PromptTokenDetails{}
	hasCacheDetails := false
	if usage.CacheReadInputTokens != nil {
		promptDetails.CacheReadTokens = int(aws.ToInt32(usage.CacheReadInputTokens))
		promptDetails.CachedTokens = promptDetails.CacheReadTokens
		hasCacheDetails = true
	}
	if usage.CacheWriteInputTokens != nil {
		promptDetails.CacheWriteTokens = int(aws.ToInt32(usage.CacheWriteInputTokens))
		hasCacheDetails = true
	}
	if hasCacheDetails {
		out.PromptTokensDetails = promptDetails
	}

	return out
}

func marshalSSE(v interface{}) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return append(append([]byte("data: "), data...), '\n', '\n'), nil
}

func applyConverseFields(dst *bedrockruntime.ConverseInput, input *ConverseInput) {
	if dst == nil || input == nil {
		return
	}
	if fields := mergeAdditionalRequestFields(input); fields != nil {
		dst.AdditionalModelRequestFields = fields
	}
	if len(input.AdditionalModelResponseFieldPaths) > 0 {
		dst.AdditionalModelResponseFieldPaths = input.AdditionalModelResponseFieldPaths
	}
	if len(input.RequestMetadata) > 0 {
		dst.RequestMetadata = input.RequestMetadata
	}
	if input.PerformanceLatency != "" {
		dst.PerformanceConfig = &brtypes.PerformanceConfiguration{
			Latency: brtypes.PerformanceConfigLatency(input.PerformanceLatency),
		}
	}
	if input.ServiceTier != "" {
		dst.ServiceTier = &brtypes.ServiceTier{
			Type: brtypes.ServiceTierType(input.ServiceTier),
		}
	}
	if input.GuardrailConfig != nil {
		guardrail := &brtypes.GuardrailConfiguration{
			GuardrailIdentifier: aws.String(input.GuardrailConfig.Identifier),
			GuardrailVersion:    aws.String(input.GuardrailConfig.Version),
		}
		if input.GuardrailConfig.Trace != "" {
			guardrail.Trace = brtypes.GuardrailTrace(input.GuardrailConfig.Trace)
		}
		dst.GuardrailConfig = guardrail
	}
}

func applyConverseStreamFields(dst *bedrockruntime.ConverseStreamInput, input *ConverseInput) {
	if dst == nil || input == nil {
		return
	}
	if fields := mergeAdditionalRequestFields(input); fields != nil {
		dst.AdditionalModelRequestFields = fields
	}
	if len(input.AdditionalModelResponseFieldPaths) > 0 {
		dst.AdditionalModelResponseFieldPaths = input.AdditionalModelResponseFieldPaths
	}
	if len(input.RequestMetadata) > 0 {
		dst.RequestMetadata = input.RequestMetadata
	}
	if input.PerformanceLatency != "" {
		dst.PerformanceConfig = &brtypes.PerformanceConfiguration{
			Latency: brtypes.PerformanceConfigLatency(input.PerformanceLatency),
		}
	}
	if input.ServiceTier != "" {
		dst.ServiceTier = &brtypes.ServiceTier{
			Type: brtypes.ServiceTierType(input.ServiceTier),
		}
	}
	if input.GuardrailConfig != nil {
		guardrail := &brtypes.GuardrailStreamConfiguration{
			GuardrailIdentifier: aws.String(input.GuardrailConfig.Identifier),
			GuardrailVersion:    aws.String(input.GuardrailConfig.Version),
		}
		if input.GuardrailConfig.Trace != "" {
			guardrail.Trace = brtypes.GuardrailTrace(input.GuardrailConfig.Trace)
		}
		if input.GuardrailConfig.StreamProcessing != "" {
			guardrail.StreamProcessingMode = brtypes.GuardrailStreamProcessingMode(input.GuardrailConfig.StreamProcessing)
		}
		dst.GuardrailConfig = guardrail
	}
}

func mergeAdditionalRequestFields(input *ConverseInput) document.Interface {
	if input == nil {
		return nil
	}

	var merged map[string]interface{}
	if input.AdditionalModelRequestFields != nil {
		b, err := input.AdditionalModelRequestFields.MarshalSmithyDocument()
		if err == nil {
			var m map[string]interface{}
			if err := json.Unmarshal(b, &m); err == nil {
				merged = m
			}
		}
	}

	if input.ThinkingConfig != nil && input.ThinkingConfig.Enabled {
		if merged == nil {
			merged = map[string]interface{}{}
		}
		merged["thinking"] = map[string]interface{}{
			"type":          "enabled",
			"budget_tokens": input.ThinkingConfig.BudgetToken,
		}
	}

	if len(merged) == 0 {
		return nil
	}
	return document.NewLazyDocument(merged)
}

func smithyDocumentToRaw(doc document.Interface) json.RawMessage {
	if doc == nil {
		return nil
	}
	b, err := doc.MarshalSmithyDocument()
	if err != nil {
		return nil
	}
	if !json.Valid(b) {
		return nil
	}
	return b
}

func shortUUID() string {
	u := uuid.New()
	return u.String()[:8]
}

func estimatedCompletionTokenDetails(estimated int) *schema.CompletionTokensDetails {
	if estimated <= 0 {
		return nil
	}
	return &schema.CompletionTokensDetails{
		ReasoningTokens:          estimated,
		ReasoningTokensEstimated: true,
		ReasoningTokensMethod:    reasoningTokenEstimateMethod,
	}
}

// estimateReasoningTokens splits outputTokens proportionally between reasoning
// and content based on their character lengths. Bedrock's outputTokens includes
// both reasoning and regular output combined — this gives us a good estimate
// without needing a tokenizer library.
func estimateReasoningTokens(outputTokens, reasoningLen, contentLen int) int {
	if outputTokens <= 0 || reasoningLen <= 0 {
		return 0
	}
	totalLen := reasoningLen + contentLen
	if totalLen <= 0 {
		return 0
	}
	estimated := int(float64(outputTokens) * float64(reasoningLen) / float64(totalLen))
	if estimated > outputTokens {
		estimated = outputTokens
	}
	return estimated
}

package bedrock

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/google/uuid"
	"github.com/stratum/gateway/internal/schema"
)

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

	return convertConverseOutputWithOptions(out, input.ModelID, input.ReasoningExclude), nil
}

func convertConverseOutput(out *bedrockruntime.ConverseOutput, modelID string) *schema.ChatResponse {
	return convertConverseOutputWithOptions(out, modelID, false)
}

func convertConverseOutputWithOptions(out *bedrockruntime.ConverseOutput, modelID string, reasoningExclude bool) *schema.ChatResponse {
	msgID := "chatcmpl-" + shortUUID()
	now := time.Now().Unix()

	var content string
	var reasoning string
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

	}

	var contentPtr *string
	if content != "" {
		contentPtr = &content
	}
	var reasoningPtr *string
	if reasoning != "" && !reasoningExclude {
		reasoningPtr = &reasoning
	}
	var sigPtr *string
	if reasoningSignature != "" && !reasoningExclude {
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
		emit := func(b []byte) bool {
			return sendStreamChunk(ctx, dataCh, b)
		}

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
			if !emitStreamErrorChunk(emit) {
				return
			}
			_ = emitDone(emit)
			return
		}

		msgID := "chatcmpl-" + shortUUID()
		now := time.Now().Unix()
		stream := out.GetStream()
		if stream == nil {
			if !emitStreamErrorChunk(emit) {
				return
			}
			_ = emitDone(emit)
			return
		}
		defer stream.Close()

		emitConverseSSEStream(
			input,
			msgID,
			now,
			stream.Events(),
			stream.Err,
			emit,
		)
	}()

	return dataCh
}

type streamSSEState struct {
	currentToolCallIndex int
	finishReason         string
	finishChunkSent      bool
	pendingUsage         *schema.Usage
}

func emitConverseSSEStream(
	input *ConverseInput,
	msgID string,
	now int64,
	events <-chan brtypes.ConverseStreamOutput,
	streamErr func() error,
	emit func([]byte) bool,
) {
	if !emitRoleChunk(msgID, now, input.ModelID, emit) {
		return
	}

	state := &streamSSEState{}
	for event := range events {
		if !handleConverseStreamEvent(input, msgID, now, event, state, emit) {
			return
		}
	}

	if streamErr != nil {
		if err := streamErr(); err != nil {
			if !state.finishChunkSent {
				if !emitStreamErrorChunk(emit) {
					return
				}
				_ = emitDone(emit)
				return
			}
		}
	}

	if !state.finishChunkSent {
		finishReason := state.finishReason
		if finishReason == "" {
			finishReason = "stop"
		}
		if !emitSSEValue(schema.ChatResponse{
			ID:      msgID,
			Object:  "chat.completion.chunk",
			Created: now,
			Model:   input.ModelID,
			Choices: []schema.Choice{{
				Index:        0,
				Delta:        &schema.ResponseMessage{},
				FinishReason: &finishReason,
			}},
		}, emit) {
			return
		}
	}

	if state.pendingUsage != nil && input.IncludeUsage {
		if !emitSSEValue(schema.ChatResponse{
			ID:      msgID,
			Object:  "chat.completion.chunk",
			Created: now,
			Model:   input.ModelID,
			Choices: []schema.Choice{},
			Usage:   state.pendingUsage,
		}, emit) {
			return
		}
	}

	_ = emitDone(emit)
}

func handleConverseStreamEvent(
	input *ConverseInput,
	msgID string,
	now int64,
	event brtypes.ConverseStreamOutput,
	state *streamSSEState,
	emit func([]byte) bool,
) bool {
	switch e := event.(type) {
	case *brtypes.ConverseStreamOutputMemberContentBlockDelta:
		delta := e.Value.Delta
		switch d := delta.(type) {
		case *brtypes.ContentBlockDeltaMemberText:
			if !emitSSEValue(schema.ChatResponse{
				ID:      msgID,
				Object:  "chat.completion.chunk",
				Created: now,
				Model:   input.ModelID,
				Choices: []schema.Choice{{
					Index: 0,
					Delta: &schema.ResponseMessage{Content: aws.String(d.Value)},
				}},
			}, emit) {
				return false
			}
		case *brtypes.ContentBlockDeltaMemberReasoningContent:
			switch rd := d.Value.(type) {
			case *brtypes.ReasoningContentBlockDeltaMemberText:
				if !input.ReasoningExclude {
					if !emitSSEValue(schema.ChatResponse{
						ID:      msgID,
						Object:  "chat.completion.chunk",
						Created: now,
						Model:   input.ModelID,
						Choices: []schema.Choice{{
							Index: 0,
							Delta: &schema.ResponseMessage{Reasoning: aws.String(rd.Value)},
						}},
					}, emit) {
						return false
					}
				}
			case *brtypes.ReasoningContentBlockDeltaMemberSignature:
				if !input.ReasoningExclude {
					if !emitSSEValue(schema.ChatResponse{
						ID:      msgID,
						Object:  "chat.completion.chunk",
						Created: now,
						Model:   input.ModelID,
						Choices: []schema.Choice{{
							Index: 0,
							Delta: &schema.ResponseMessage{ReasoningSignature: aws.String(rd.Value)},
						}},
					}, emit) {
						return false
					}
				}
			case *brtypes.ReasoningContentBlockDeltaMemberRedactedContent:
				_ = rd
			}
		case *brtypes.ContentBlockDeltaMemberToolUse:
			idx := state.currentToolCallIndex
			if !emitSSEValue(schema.ChatResponse{
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
			}, emit) {
				return false
			}
		}
	case *brtypes.ConverseStreamOutputMemberContentBlockStart:
		start := e.Value.Start
		if tu, ok := start.(*brtypes.ContentBlockStartMemberToolUse); ok {
			idx := state.currentToolCallIndex
			if !emitSSEValue(schema.ChatResponse{
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
			}, emit) {
				return false
			}
			state.currentToolCallIndex++
		}
	case *brtypes.ConverseStreamOutputMemberMessageStop:
		state.finishReason = MapStopReason(e.Value.StopReason)
		if !emitSSEValue(schema.ChatResponse{
			ID:      msgID,
			Object:  "chat.completion.chunk",
			Created: now,
			Model:   input.ModelID,
			Choices: []schema.Choice{{
				Index:        0,
				Delta:        &schema.ResponseMessage{},
				FinishReason: &state.finishReason,
			}},
		}, emit) {
			return false
		}
		state.finishChunkSent = true
	case *brtypes.ConverseStreamOutputMemberMetadata:
		if !input.IncludeUsage || e.Value.Usage == nil {
			return true
		}
		state.pendingUsage = buildUsageFromTokenUsage(e.Value.Usage)
	}
	return true
}

func emitRoleChunk(msgID string, now int64, modelID string, emit func([]byte) bool) bool {
	return emitSSEValue(schema.ChatResponse{
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

func emitSSEValue(v interface{}, emit func([]byte) bool) bool {
	data, err := marshalSSE(v)
	if err != nil {
		return false
	}
	return emit(data)
}

func emitStreamErrorChunk(emit func([]byte) bool) bool {
	return emitSSEValue(map[string]any{
		"error": map[string]string{
			"message": "Upstream model invocation failed",
		},
	}, emit)
}

func emitDone(emit func([]byte) bool) bool {
	return emit([]byte("data: [DONE]\n\n"))
}

func sendStreamChunk(ctx context.Context, out chan<- []byte, chunk []byte) bool {
	if ctx.Err() != nil {
		return false
	}
	select {
	case <-ctx.Done():
		return false
	case out <- chunk:
		return true
	}
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
}

func applyConverseStreamFields(dst *bedrockruntime.ConverseStreamInput, input *ConverseInput) {
	if dst == nil || input == nil {
		return
	}
	if fields := mergeAdditionalRequestFields(input); fields != nil {
		dst.AdditionalModelRequestFields = fields
	}
}

func mergeAdditionalRequestFields(input *ConverseInput) document.Interface {
	if input == nil {
		return nil
	}
	if input.AdditionalModelRequestFields == nil {
		return nil
	}
	b, err := input.AdditionalModelRequestFields.MarshalSmithyDocument()
	if err != nil {
		return nil
	}
	var merged map[string]interface{}
	if err := json.Unmarshal(b, &merged); err != nil {
		return nil
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

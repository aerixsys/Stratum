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
			errSSE, _ := marshalSSE(map[string]any{
				"error": map[string]string{
					"message": "Upstream model invocation failed",
				},
			})
			dataCh <- errSSE
			dataCh <- []byte("data: [DONE]\n\n")
			return
		}

		msgID := "chatcmpl-" + shortUUID()
		now := time.Now().Unix()
		stream := out.GetStream()
		defer stream.Close()

		// Send initial role chunk
		roleChunk := schema.ChatResponse{
			ID:      msgID,
			Object:  "chat.completion.chunk",
			Created: now,
			Model:   input.ModelID,
			Choices: []schema.Choice{
				{
					Index: 0,
					Delta: &schema.ResponseMessage{Role: "assistant"},
				},
			},
		}
		if data, err := marshalSSE(roleChunk); err == nil {
			dataCh <- data
		}

		var currentToolCallIndex int
		var finishReason string
		finishChunkSent := false

		for event := range stream.Events() {
			switch e := event.(type) {
			case *brtypes.ConverseStreamOutputMemberContentBlockDelta:
				delta := e.Value.Delta
				switch d := delta.(type) {
				case *brtypes.ContentBlockDeltaMemberText:
					chunk := schema.ChatResponse{
						ID:      msgID,
						Object:  "chat.completion.chunk",
						Created: now,
						Model:   input.ModelID,
						Choices: []schema.Choice{{
							Index: 0,
							Delta: &schema.ResponseMessage{Content: aws.String(d.Value)},
						}},
					}
					if data, err := marshalSSE(chunk); err == nil {
						dataCh <- data
					}

				case *brtypes.ContentBlockDeltaMemberReasoningContent:
					// Handle reasoning/thinking deltas
					switch rd := d.Value.(type) {
					case *brtypes.ReasoningContentBlockDeltaMemberText:
						chunk := schema.ChatResponse{
							ID:      msgID,
							Object:  "chat.completion.chunk",
							Created: now,
							Model:   input.ModelID,
							Choices: []schema.Choice{{
								Index: 0,
								Delta: &schema.ResponseMessage{Reasoning: aws.String(rd.Value)},
							}},
						}
						if data, err := marshalSSE(chunk); err == nil {
							dataCh <- data
						}
					case *brtypes.ReasoningContentBlockDeltaMemberSignature:
						chunk := schema.ChatResponse{
							ID:      msgID,
							Object:  "chat.completion.chunk",
							Created: now,
							Model:   input.ModelID,
							Choices: []schema.Choice{{
								Index: 0,
								Delta: &schema.ResponseMessage{ReasoningSignature: aws.String(rd.Value)},
							}},
						}
						if data, err := marshalSSE(chunk); err == nil {
							dataCh <- data
						}
					case *brtypes.ReasoningContentBlockDeltaMemberRedactedContent:
						// Encrypted thinking — skip or note
						_ = rd
					}

				case *brtypes.ContentBlockDeltaMemberToolUse:
					idx := currentToolCallIndex
					chunk := schema.ChatResponse{
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
					}
					if data, err := marshalSSE(chunk); err == nil {
						dataCh <- data
					}
				}

			case *brtypes.ConverseStreamOutputMemberContentBlockStart:
				start := e.Value.Start
				if tu, ok := start.(*brtypes.ContentBlockStartMemberToolUse); ok {
					idx := currentToolCallIndex
					chunk := schema.ChatResponse{
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
					}
					if data, err := marshalSSE(chunk); err == nil {
						dataCh <- data
					}
					currentToolCallIndex++
				}

			case *brtypes.ConverseStreamOutputMemberMessageStop:
				finishReason = MapStopReason(e.Value.StopReason)
				chunk := schema.ChatResponse{
					ID:      msgID,
					Object:  "chat.completion.chunk",
					Created: now,
					Model:   input.ModelID,
					Choices: []schema.Choice{{
						Index:        0,
						Delta:        &schema.ResponseMessage{},
						FinishReason: &finishReason,
					}},
				}
				if data, err := marshalSSE(chunk); err == nil {
					dataCh <- data
					finishChunkSent = true
				}

			case *brtypes.ConverseStreamOutputMemberMetadata:
				if !input.IncludeUsage || e.Value.Usage == nil {
					continue
				}
				usage := &schema.Usage{
					PromptTokens:     int(aws.ToInt32(e.Value.Usage.InputTokens)),
					CompletionTokens: int(aws.ToInt32(e.Value.Usage.OutputTokens)),
					TotalTokens:      int(aws.ToInt32(e.Value.Usage.TotalTokens)),
				}
				promptDetails := &schema.PromptTokenDetails{}
				hasCacheDetails := false
				if e.Value.Usage.CacheReadInputTokens != nil {
					promptDetails.CacheReadTokens = int(aws.ToInt32(e.Value.Usage.CacheReadInputTokens))
					promptDetails.CachedTokens = promptDetails.CacheReadTokens
					hasCacheDetails = true
				}
				if e.Value.Usage.CacheWriteInputTokens != nil {
					promptDetails.CacheWriteTokens = int(aws.ToInt32(e.Value.Usage.CacheWriteInputTokens))
					hasCacheDetails = true
				}
				if hasCacheDetails {
					usage.PromptTokensDetails = promptDetails
				}
				usageChunk := schema.ChatResponse{
					ID:      msgID,
					Object:  "chat.completion.chunk",
					Created: now,
					Model:   input.ModelID,
					Choices: []schema.Choice{},
					Usage:   usage,
				}
				if data, err := marshalSSE(usageChunk); err == nil {
					dataCh <- data
				}
			}
		}

		if err := stream.Err(); err != nil {
			log.Printf("[stream] Error: %v", err)
		}
		if !finishChunkSent {
			if finishReason == "" {
				finishReason = "stop"
			}
			chunk := schema.ChatResponse{
				ID:      msgID,
				Object:  "chat.completion.chunk",
				Created: now,
				Model:   input.ModelID,
				Choices: []schema.Choice{{
					Index:        0,
					Delta:        &schema.ResponseMessage{},
					FinishReason: &finishReason,
				}},
			}
			if data, err := marshalSSE(chunk); err == nil {
				dataCh <- data
			}
		}

		// Send [DONE]
		dataCh <- []byte("data: [DONE]\n\n")
	}()

	return dataCh
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

// buildThinkingFields creates the additionalModelRequestFields document
// for extended thinking configuration.
func buildThinkingFields(cfg *ThinkingConfig) document.Interface {
	return document.NewLazyDocument(map[string]interface{}{
		"thinking": map[string]interface{}{
			"type":          "enabled",
			"budget_tokens": cfg.BudgetToken,
		},
	})
}

func shortUUID() string {
	u := uuid.New()
	return u.String()[:8]
}

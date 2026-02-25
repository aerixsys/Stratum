package bedrock

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
)

func TestEmitConverseSSEStream_OrderAndSchema(t *testing.T) {
	input := &ConverseInput{
		ModelID:      "anthropic.claude-sonnet-4-5-20250929-v1:0",
		IncludeUsage: true,
	}

	events := make(chan brtypes.ConverseStreamOutput, 8)
	events <- &brtypes.ConverseStreamOutputMemberContentBlockDelta{
		Value: brtypes.ContentBlockDeltaEvent{
			ContentBlockIndex: aws.Int32(0),
			Delta: &brtypes.ContentBlockDeltaMemberText{
				Value: "hello",
			},
		},
	}
	events <- &brtypes.ConverseStreamOutputMemberContentBlockStart{
		Value: brtypes.ContentBlockStartEvent{
			ContentBlockIndex: aws.Int32(1),
			Start: &brtypes.ContentBlockStartMemberToolUse{
				Value: brtypes.ToolUseBlockStart{
					ToolUseId: aws.String("call_1"),
					Name:      aws.String("get_weather"),
				},
			},
		},
	}
	events <- &brtypes.ConverseStreamOutputMemberContentBlockDelta{
		Value: brtypes.ContentBlockDeltaEvent{
			ContentBlockIndex: aws.Int32(1),
			Delta: &brtypes.ContentBlockDeltaMemberToolUse{
				Value: brtypes.ToolUseBlockDelta{Input: aws.String("{\"city\":\"nyc\"}")},
			},
		},
	}
	events <- &brtypes.ConverseStreamOutputMemberContentBlockDelta{
		Value: brtypes.ContentBlockDeltaEvent{
			ContentBlockIndex: aws.Int32(2),
			Delta: &brtypes.ContentBlockDeltaMemberReasoningContent{
				Value: &brtypes.ReasoningContentBlockDeltaMemberText{Value: "thinking"},
			},
		},
	}
	events <- &brtypes.ConverseStreamOutputMemberContentBlockDelta{
		Value: brtypes.ContentBlockDeltaEvent{
			ContentBlockIndex: aws.Int32(2),
			Delta: &brtypes.ContentBlockDeltaMemberReasoningContent{
				Value: &brtypes.ReasoningContentBlockDeltaMemberSignature{Value: "sig-1"},
			},
		},
	}
	// Emit metadata before message stop to assert we still place usage after finish chunk.
	events <- &brtypes.ConverseStreamOutputMemberMetadata{
		Value: brtypes.ConverseStreamMetadataEvent{
			Metrics: &brtypes.ConverseStreamMetrics{LatencyMs: aws.Int64(12)},
			Usage: &brtypes.TokenUsage{
				InputTokens:           aws.Int32(10),
				OutputTokens:          aws.Int32(5),
				TotalTokens:           aws.Int32(15),
				CacheReadInputTokens:  aws.Int32(7),
				CacheWriteInputTokens: aws.Int32(3),
			},
		},
	}
	events <- &brtypes.ConverseStreamOutputMemberMessageStop{
		Value: brtypes.MessageStopEvent{StopReason: brtypes.StopReasonEndTurn},
	}
	close(events)

	chunks := collectSSEChunks(input, events, func() error { return nil })
	if len(chunks) != 9 {
		t.Fatalf("expected 9 chunks, got %d", len(chunks))
	}

	role := decodeChunkAsMap(t, chunks[0])
	if got := mapString(role, "object"); got != "chat.completion.chunk" {
		t.Fatalf("unexpected role chunk object: %v", got)
	}
	choices := role["choices"].([]interface{})
	delta := choices[0].(map[string]interface{})["delta"].(map[string]interface{})
	if got := mapString(delta, "role"); got != "assistant" {
		t.Fatalf("expected assistant role delta, got %q", got)
	}

	text := decodeChunkAsMap(t, chunks[1])
	textDelta := text["choices"].([]interface{})[0].(map[string]interface{})["delta"].(map[string]interface{})
	if got := mapString(textDelta, "content"); got != "hello" {
		t.Fatalf("unexpected text delta: %q", got)
	}

	toolStart := decodeChunkAsMap(t, chunks[2])
	toolStartTC := firstToolCall(toolStart)
	if mapString(toolStartTC, "id") != "call_1" {
		t.Fatalf("unexpected tool id: %+v", toolStartTC)
	}
	if mapString(toolStartTC["function"].(map[string]interface{}), "name") != "get_weather" {
		t.Fatalf("unexpected tool name: %+v", toolStartTC)
	}

	toolDelta := decodeChunkAsMap(t, chunks[3])
	toolDeltaTC := firstToolCall(toolDelta)
	if mapString(toolDeltaTC["function"].(map[string]interface{}), "arguments") != "{\"city\":\"nyc\"}" {
		t.Fatalf("unexpected tool arguments chunk: %+v", toolDeltaTC)
	}

	reasoning := decodeChunkAsMap(t, chunks[4])
	reasoningDelta := reasoning["choices"].([]interface{})[0].(map[string]interface{})["delta"].(map[string]interface{})
	if mapString(reasoningDelta, "reasoning") != "thinking" {
		t.Fatalf("unexpected reasoning delta: %+v", reasoningDelta)
	}

	signature := decodeChunkAsMap(t, chunks[5])
	signatureDelta := signature["choices"].([]interface{})[0].(map[string]interface{})["delta"].(map[string]interface{})
	if mapString(signatureDelta, "reasoning_signature") != "sig-1" {
		t.Fatalf("unexpected reasoning signature delta: %+v", signatureDelta)
	}

	finish := decodeChunkAsMap(t, chunks[6])
	finishChoice := finish["choices"].([]interface{})[0].(map[string]interface{})
	if mapString(finishChoice, "finish_reason") != "stop" {
		t.Fatalf("unexpected finish reason chunk: %+v", finishChoice)
	}

	usage := decodeChunkAsMap(t, chunks[7])
	if _, ok := usage["usage"]; !ok {
		t.Fatalf("expected usage chunk after finish")
	}
	usageMap := usage["usage"].(map[string]interface{})
	if got := int(usageMap["prompt_tokens"].(float64)); got != 10 {
		t.Fatalf("unexpected prompt_tokens in usage: %d", got)
	}
	completionDetails, ok := usageMap["completion_tokens_details"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected completion_tokens_details in usage chunk, got %+v", usageMap)
	}
	if got := int(completionDetails["reasoning_tokens"].(float64)); got <= 0 {
		t.Fatalf("expected positive reasoning_tokens estimate in stream usage, got %d", got)
	}
	if got := completionDetails["reasoning_tokens_estimated"]; got != true {
		t.Fatalf("expected reasoning_tokens_estimated=true, got %+v", got)
	}
	if got := mapString(completionDetails, "reasoning_tokens_method"); got != reasoningTokenEstimateMethod {
		t.Fatalf("expected reasoning_tokens_method=%q, got %q", reasoningTokenEstimateMethod, got)
	}

	if string(chunks[8]) != "data: [DONE]\n\n" {
		t.Fatalf("expected final [DONE], got %q", string(chunks[8]))
	}
}

func TestEmitConverseSSEStream_MidStreamErrorEmitsValidErrorJSON(t *testing.T) {
	input := &ConverseInput{ModelID: "amazon.nova-micro-v1:0"}
	events := make(chan brtypes.ConverseStreamOutput, 1)
	events <- &brtypes.ConverseStreamOutputMemberContentBlockDelta{
		Value: brtypes.ContentBlockDeltaEvent{
			ContentBlockIndex: aws.Int32(0),
			Delta: &brtypes.ContentBlockDeltaMemberText{
				Value: "partial",
			},
		},
	}
	close(events)

	chunks := collectSSEChunks(input, events, func() error { return errors.New("socket closed") })
	if len(chunks) != 4 {
		t.Fatalf("expected role+delta+error+[DONE] chunks, got %d", len(chunks))
	}
	if _, ok := decodeChunkAsMap(t, chunks[2])["error"]; !ok {
		t.Fatalf("expected error JSON chunk, got %q", string(chunks[2]))
	}
	if string(chunks[3]) != "data: [DONE]\n\n" {
		t.Fatalf("expected [DONE], got %q", string(chunks[3]))
	}
}

func TestEmitConverseSSEStream_IncludeUsageFalseSkipsUsageChunk(t *testing.T) {
	input := &ConverseInput{ModelID: "amazon.nova-micro-v1:0", IncludeUsage: false}
	events := make(chan brtypes.ConverseStreamOutput, 2)
	events <- &brtypes.ConverseStreamOutputMemberMetadata{
		Value: brtypes.ConverseStreamMetadataEvent{
			Metrics: &brtypes.ConverseStreamMetrics{LatencyMs: aws.Int64(1)},
			Usage: &brtypes.TokenUsage{
				InputTokens:  aws.Int32(2),
				OutputTokens: aws.Int32(3),
				TotalTokens:  aws.Int32(5),
			},
		},
	}
	events <- &brtypes.ConverseStreamOutputMemberMessageStop{
		Value: brtypes.MessageStopEvent{StopReason: brtypes.StopReasonEndTurn},
	}
	close(events)

	chunks := collectSSEChunks(input, events, func() error { return nil })
	if len(chunks) != 3 {
		t.Fatalf("expected role+finish+[DONE], got %d", len(chunks))
	}
	if string(chunks[2]) != "data: [DONE]\n\n" {
		t.Fatalf("expected [DONE], got %q", string(chunks[2]))
	}
}

func TestEmitConverseSSEStream_SynthesizesFinishWhenMissing(t *testing.T) {
	input := &ConverseInput{ModelID: "amazon.nova-micro-v1:0"}
	events := make(chan brtypes.ConverseStreamOutput, 1)
	events <- &brtypes.ConverseStreamOutputMemberContentBlockDelta{
		Value: brtypes.ContentBlockDeltaEvent{
			ContentBlockIndex: aws.Int32(0),
			Delta:             &brtypes.ContentBlockDeltaMemberText{Value: "hi"},
		},
	}
	close(events)

	chunks := collectSSEChunks(input, events, func() error { return nil })
	if len(chunks) != 4 {
		t.Fatalf("expected role+delta+finish+[DONE], got %d", len(chunks))
	}
	finish := decodeChunkAsMap(t, chunks[2])
	finishChoice := finish["choices"].([]interface{})[0].(map[string]interface{})
	if mapString(finishChoice, "finish_reason") != "stop" {
		t.Fatalf("expected synthesized finish=stop, got %+v", finishChoice)
	}
}

func collectSSEChunks(
	input *ConverseInput,
	events <-chan brtypes.ConverseStreamOutput,
	streamErr func() error,
) [][]byte {
	var chunks [][]byte
	emitConverseSSEStream(input, "chatcmpl-test", 1700000000, events, streamErr, func(b []byte) {
		copied := make([]byte, len(b))
		copy(copied, b)
		chunks = append(chunks, copied)
	})
	return chunks
}

func decodeChunkAsMap(t *testing.T, chunk []byte) map[string]interface{} {
	t.Helper()
	s := string(chunk)
	if !strings.HasPrefix(s, "data: ") || !strings.HasSuffix(s, "\n\n") {
		t.Fatalf("invalid SSE chunk framing: %q", s)
	}
	payload := strings.TrimSuffix(strings.TrimPrefix(s, "data: "), "\n\n")
	var decoded map[string]interface{}
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		t.Fatalf("invalid JSON payload %q: %v", payload, err)
	}
	return decoded
}

func firstToolCall(m map[string]interface{}) map[string]interface{} {
	choices := m["choices"].([]interface{})
	delta := choices[0].(map[string]interface{})["delta"].(map[string]interface{})
	toolCalls := delta["tool_calls"].([]interface{})
	return toolCalls[0].(map[string]interface{})
}

func mapString(m map[string]interface{}, key string) string {
	v, _ := m[key]
	s, _ := v.(string)
	return s
}

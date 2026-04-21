package authropic

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

func TestMessageOutboundTransformRequestUsesCompatForRawResponses(t *testing.T) {
	stream := false
	outbound := &MessageOutbound{}

	req, err := outbound.TransformRequest(context.Background(), &model.InternalLLMRequest{
		Model:        "claude-sonnet-4-20250514",
		Stream:       &stream,
		RawAPIFormat: model.APIFormatOpenAIResponse,
		RawRequest:   []byte(`{"model":"gpt-5","input":[{"role":"system","content":"你是助手"},{"role":"user","content":"hello"}],"stream":true}`),
	}, "https://example.com", "test-key")
	if err != nil {
		t.Fatalf("TransformRequest returned error: %v", err)
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("failed to read request body: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("failed to decode compat anthropic body: %v", err)
	}

	if got["model"] != "claude-sonnet-4-20250514" {
		t.Fatalf("model should be rewritten by compat path, got %v", got["model"])
	}
	if streamValue, ok := got["stream"]; ok && streamValue != false {
		t.Fatalf("stream should be omitted or false, got %v", streamValue)
	}
	if got["system"] != "你是助手" {
		t.Fatalf("system prompt should be preserved, got %#v", got["system"])
	}

	messages, ok := got["messages"].([]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("expected one anthropic message, got %T len=%d", got["messages"], len(messages))
	}
	msg, _ := messages[0].(map[string]any)
	if msg["role"] != "user" {
		t.Fatalf("expected anthropic user message, got %#v", msg["role"])
	}
}

func TestMessageOutboundTransformRequestUsesCompatForRawChatCompletions(t *testing.T) {
	stream := true
	outbound := &MessageOutbound{}

	req, err := outbound.TransformRequest(context.Background(), &model.InternalLLMRequest{
		Model:        "claude-sonnet-4-20250514",
		Stream:       &stream,
		RawAPIFormat: model.APIFormatOpenAIChatCompletion,
		RawRequest:   []byte(`{"model":"gpt-4.1","messages":[{"role":"system","content":"你是助手"},{"role":"user","content":"hello"}],"stream":false}`),
	}, "https://example.com", "test-key")
	if err != nil {
		t.Fatalf("TransformRequest returned error: %v", err)
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("failed to read request body: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("failed to decode compat anthropic body: %v", err)
	}

	if got["model"] != "claude-sonnet-4-20250514" {
		t.Fatalf("model should be rewritten by compat path, got %v", got["model"])
	}
	if got["stream"] != true {
		t.Fatalf("stream should follow internal request, got %v", got["stream"])
	}
	if got["system"] != "你是助手" {
		t.Fatalf("system prompt should be preserved, got %#v", got["system"])
	}
}

func TestMessageOutboundTransformStreamCompatToolUse(t *testing.T) {
	outbound := &MessageOutbound{}

	messageStart := []byte(`{"type":"message_start","message":{"id":"msg_123","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-20250514","usage":{"input_tokens":7,"cache_creation_input_tokens":2}}}`)
	startResp, err := outbound.TransformStream(nil, messageStart)
	if err != nil {
		t.Fatalf("TransformStream message_start error: %v", err)
	}
	if startResp == nil || len(startResp.Choices) != 1 || startResp.Choices[0].Delta == nil || startResp.Choices[0].Delta.Role != "assistant" {
		t.Fatalf("expected assistant role chunk on message_start, got %#v", startResp)
	}
	if startResp.Usage == nil || startResp.Usage.PromptTokens != 7 || startResp.Usage.CacheCreationInputTokens != 2 {
		t.Fatalf("expected prompt usage on message_start, got %#v", startResp.Usage)
	}

	toolStart := []byte(`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_123","name":"exec_command","input":{}}}`)
	toolResp, err := outbound.TransformStream(nil, toolStart)
	if err != nil {
		t.Fatalf("TransformStream content_block_start error: %v", err)
	}
	if toolResp == nil || len(toolResp.Choices) != 1 || len(toolResp.Choices[0].Delta.ToolCalls) != 1 {
		t.Fatalf("expected tool call chunk, got %#v", toolResp)
	}
	if toolResp.Choices[0].Delta.ToolCalls[0].Function.Name != "exec_command" {
		t.Fatalf("expected tool name from compat chunk, got %#v", toolResp.Choices[0].Delta.ToolCalls[0])
	}

	toolDelta := []byte(`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"cmd\":\"pwd\"}"}}`)
	deltaResp, err := outbound.TransformStream(nil, toolDelta)
	if err != nil {
		t.Fatalf("TransformStream content_block_delta error: %v", err)
	}
	if deltaResp == nil || len(deltaResp.Choices) != 1 || len(deltaResp.Choices[0].Delta.ToolCalls) != 1 {
		t.Fatalf("expected tool call arguments delta, got %#v", deltaResp)
	}
	if deltaResp.Choices[0].Delta.ToolCalls[0].Function.Arguments != `{"cmd":"pwd"}` {
		t.Fatalf("expected tool call arguments delta, got %#v", deltaResp.Choices[0].Delta.ToolCalls[0])
	}

	messageDelta := []byte(`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":3,"cache_read_input_tokens":5}}`)
	deltaOnlyResp, err := outbound.TransformStream(nil, messageDelta)
	if err != nil {
		t.Fatalf("TransformStream message_delta error: %v", err)
	}
	if deltaOnlyResp != nil {
		t.Fatalf("expected message_delta to be buffered into final usage chunk, got %#v", deltaOnlyResp)
	}

	messageStop := []byte(`{"type":"message_stop"}`)
	stopResp, err := outbound.TransformStream(nil, messageStop)
	if err != nil {
		t.Fatalf("TransformStream message_stop error: %v", err)
	}
	if stopResp == nil || len(stopResp.Choices) != 1 {
		t.Fatalf("expected final completion chunk, got %#v", stopResp)
	}
	if stopResp.Choices[0].FinishReason == nil || *stopResp.Choices[0].FinishReason != "tool_calls" {
		t.Fatalf("expected tool_calls finish reason, got %#v", stopResp.Choices[0].FinishReason)
	}
	if stopResp.Usage == nil {
		t.Fatalf("expected final usage on message_stop")
	}
	if stopResp.Usage.PromptTokens != 7 || stopResp.Usage.CompletionTokens != 3 || stopResp.Usage.TotalTokens != 17 {
		t.Fatalf("unexpected final usage: %#v", stopResp.Usage)
	}
	if stopResp.Usage.PromptTokensDetails == nil || stopResp.Usage.PromptTokensDetails.CachedTokens != 5 {
		t.Fatalf("expected cached prompt tokens to propagate, got %#v", stopResp.Usage.PromptTokensDetails)
	}
	if !stopResp.Usage.AnthropicUsage || stopResp.Usage.CacheCreationInputTokens != 2 {
		t.Fatalf("expected anthropic-specific usage details, got %#v", stopResp.Usage)
	}
}

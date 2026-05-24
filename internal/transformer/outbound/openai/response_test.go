package openai

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

func TestMarshalResponsesRequestPreservesRawResponsesPayload(t *testing.T) {
	raw := []byte(`{"model":"gpt-5","include":["reasoning.encrypted_content"],"reasoning":{"effort":"high","max_tokens":2048},"text":{"format":{"type":"json_schema","name":"answer","schema":{"type":"object"}},"verbosity":"high"},"metadata":{"trace_id":"abc"},"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}]}`)

	body, err := marshalResponsesRequest(&model.InternalLLMRequest{
		Model:        "gpt-5-mini",
		RawAPIFormat: model.APIFormatOpenAIResponse,
		RawRequest:   raw,
	})
	if err != nil {
		t.Fatalf("marshalResponsesRequest returned error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("failed to decode preserved body: %v", err)
	}

	if got["model"] != "gpt-5-mini" {
		t.Fatalf("model not rewritten, got %v", got["model"])
	}
	if _, ok := got["include"]; !ok {
		t.Fatalf("include should be preserved")
	}
	reasoning, ok := got["reasoning"].(map[string]any)
	if !ok {
		t.Fatalf("reasoning should be preserved as object, got %T", got["reasoning"])
	}
	if reasoning["max_tokens"] != float64(2048) {
		t.Fatalf("reasoning.max_tokens should be preserved, got %v", reasoning["max_tokens"])
	}
	text, ok := got["text"].(map[string]any)
	if !ok {
		t.Fatalf("text should be preserved as object, got %T", got["text"])
	}
	format, ok := text["format"].(map[string]any)
	if !ok {
		t.Fatalf("text.format should be preserved as object, got %T", text["format"])
	}
	if format["name"] != "answer" {
		t.Fatalf("text.format.name should be preserved, got %v", format["name"])
	}
	if _, ok := format["schema"]; !ok {
		t.Fatalf("text.format.schema should be preserved")
	}
	input, ok := got["input"].([]any)
	if !ok || len(input) != 1 {
		t.Fatalf("input shape should be preserved, got %T len=%d", got["input"], len(input))
	}
	firstItem, ok := input[0].(map[string]any)
	if !ok {
		t.Fatalf("input[0] should remain an object, got %T", input[0])
	}
	if firstItem["type"] != "message" {
		t.Fatalf("input[0].type should be preserved, got %v", firstItem["type"])
	}
}

func TestConvertToLLMResponseFromResponsesPreservesReasoningContent(t *testing.T) {
	status := "completed"
	resp := &ResponsesResponse{
		ID:     "resp_reasoning",
		Model:  "gpt-5",
		Status: &status,
		Output: []ResponsesItem{
			{
				Type: "reasoning",
				Summary: []ResponsesReasoningSummary{
					{Type: "summary_text", Text: "先分析约束。"},
				},
			},
			{
				Type: "message",
				Role: "assistant",
				Content: &ResponsesInput{
					Items: []ResponsesItem{
						{Type: "output_text", Text: strPtr("最终答案")},
					},
				},
			},
		},
	}

	got := convertToLLMResponseFromResponses(resp)
	if got == nil || len(got.Choices) != 1 || got.Choices[0].Message == nil {
		t.Fatalf("expected one converted choice, got %#v", got)
	}
	if got.Choices[0].Message.ReasoningContent == nil || *got.Choices[0].Message.ReasoningContent != "先分析约束。" {
		t.Fatalf("expected reasoning content to survive conversion, got %#v", got.Choices[0].Message.ReasoningContent)
	}
	if got.Choices[0].Message.Content.Content == nil || *got.Choices[0].Message.Content.Content != "最终答案" {
		t.Fatalf("expected text content to survive conversion, got %#v", got.Choices[0].Message.Content.Content)
	}
}

func TestMarshalResponsesRequestFallsBackToTransformedBody(t *testing.T) {
	body, err := marshalResponsesRequest(&model.InternalLLMRequest{
		Model: "gpt-5-mini",
		Messages: []model.Message{{
			Role: "user",
			Content: model.MessageContent{
				Content: strPtr("hello"),
			},
		}},
	})
	if err != nil {
		t.Fatalf("marshalResponsesRequest returned error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("failed to decode transformed body: %v", err)
	}

	if got["model"] != "gpt-5-mini" {
		t.Fatalf("unexpected model: %v", got["model"])
	}
	if got["input"] != "hello" {
		t.Fatalf("expected simple transformed input, got %v", got["input"])
	}
	if _, ok := got["include"]; ok {
		t.Fatalf("include should not appear in transformed fallback body")
	}
}

func TestMarshalResponsesRequestUsesCompatForRawChatCompletions(t *testing.T) {
	stream := true
	raw := []byte(`{"model":"gpt-4.1","messages":[{"role":"system","content":"你是助手"},{"role":"user","content":"hello"}],"stream":false}`)

	body, err := marshalResponsesRequest(&model.InternalLLMRequest{
		Model:        "gpt-5-mini",
		Stream:       &stream,
		RawAPIFormat: model.APIFormatOpenAIChatCompletion,
		RawRequest:   raw,
	})
	if err != nil {
		t.Fatalf("marshalResponsesRequest returned error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("failed to decode compat body: %v", err)
	}

	if got["model"] != "gpt-5-mini" {
		t.Fatalf("model should be rewritten by compat path, got %v", got["model"])
	}
	if got["stream"] != true {
		t.Fatalf("stream should follow internal request, got %v", got["stream"])
	}

	input, ok := got["input"].([]any)
	if !ok || len(input) != 2 {
		t.Fatalf("expected compat input array with 2 items, got %T len=%d", got["input"], len(input))
	}
	first, _ := input[0].(map[string]any)
	second, _ := input[1].(map[string]any)
	if first["role"] != "system" || second["role"] != "user" {
		t.Fatalf("unexpected compat roles: %#v %#v", first["role"], second["role"])
	}
}

func TestMarshalResponsesRequestUsesCompatForRawAnthropic(t *testing.T) {
	stream := true
	raw := []byte(`{"model":"claude-3-7-sonnet","system":"你是助手","messages":[{"role":"user","content":"hello"}],"stream":false}`)

	body, err := marshalResponsesRequest(&model.InternalLLMRequest{
		Model:        "gpt-5-mini",
		Stream:       &stream,
		RawAPIFormat: model.APIFormatAnthropicMessage,
		RawRequest:   raw,
	})
	if err != nil {
		t.Fatalf("marshalResponsesRequest returned error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("failed to decode compat body: %v", err)
	}

	if got["model"] != "gpt-5-mini" {
		t.Fatalf("model should be rewritten by compat path, got %v", got["model"])
	}
	if got["stream"] != true {
		t.Fatalf("stream should follow internal request, got %v", got["stream"])
	}

	input, ok := got["input"].([]any)
	if !ok || len(input) != 2 {
		t.Fatalf("expected compat input array with 2 items, got %T len=%d", got["input"], len(input))
	}
	first, _ := input[0].(map[string]any)
	second, _ := input[1].(map[string]any)
	if first["role"] != "system" || second["role"] != "user" {
		t.Fatalf("unexpected compat roles: %#v %#v", first["role"], second["role"])
	}
}

func TestResponseOutboundTransformResponsePreservesReasoningContent(t *testing.T) {
	upstreamBody := []byte(`{"object":"response","id":"resp_reasoning","model":"gpt-5","status":"completed","output":[{"type":"reasoning","summary":[{"type":"summary_text","text":"先分析约束。"}]},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"最终答案"}]}],"usage":{"input_tokens":10,"output_tokens":8,"total_tokens":18,"output_tokens_details":{"reasoning_tokens":3}}}`)

	resp, err := (&ResponseOutbound{}).TransformResponse(nil, &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(upstreamBody)),
	})
	if err != nil {
		t.Fatalf("TransformResponse returned error: %v", err)
	}
	if resp == nil || len(resp.Choices) != 1 || resp.Choices[0].Message == nil {
		t.Fatalf("expected one chat message response, got %#v", resp)
	}
	if resp.Choices[0].Message.ReasoningContent == nil || *resp.Choices[0].Message.ReasoningContent != "先分析约束。" {
		t.Fatalf("expected reasoning_content to be preserved, got %#v", resp.Choices[0].Message.ReasoningContent)
	}
	if resp.Choices[0].Message.Content.Content == nil || *resp.Choices[0].Message.Content.Content != "最终答案" {
		t.Fatalf("expected output text to be preserved, got %#v", resp.Choices[0].Message.Content.Content)
	}
}

func strPtr(s string) *string {
	return &s
}

func TestResponseOutboundTransformStreamSupportsFunctionCallDoneEvents(t *testing.T) {
	t.Run("added and done arguments fallback", func(t *testing.T) {
		outbound := &ResponseOutbound{}

		addedEvent := []byte(`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","call_id":"call_123","name":"exec_command"}}`)
		addedResp, err := outbound.TransformStream(nil, addedEvent)
		if err != nil {
			t.Fatalf("TransformStream added event error: %v", err)
		}
		if addedResp == nil || len(addedResp.Choices) != 1 || len(addedResp.Choices[0].Delta.ToolCalls) != 1 {
			t.Fatalf("expected tool call from output_item.added, got %#v", addedResp)
		}
		if addedResp.Choices[0].Delta.ToolCalls[0].Function.Name != "exec_command" {
			t.Fatalf("expected function name from added event, got %#v", addedResp.Choices[0].Delta.ToolCalls[0])
		}

		doneArgsEvent := []byte(`{"type":"response.function_call_arguments.done","output_index":0,"call_id":"call_123","arguments":"{\"cmd\":\"pwd\"}"}`)
		doneArgsResp, err := outbound.TransformStream(nil, doneArgsEvent)
		if err != nil {
			t.Fatalf("TransformStream done args event error: %v", err)
		}
		if doneArgsResp == nil || len(doneArgsResp.Choices) != 1 || len(doneArgsResp.Choices[0].Delta.ToolCalls) != 1 {
			t.Fatalf("expected tool call from function_call_arguments.done, got %#v", doneArgsResp)
		}
		if doneArgsResp.Choices[0].Delta.ToolCalls[0].Function.Arguments != `{"cmd":"pwd"}` {
			t.Fatalf("expected arguments from done event, got %#v", doneArgsResp.Choices[0].Delta.ToolCalls[0])
		}
	})

	t.Run("output item done fallback", func(t *testing.T) {
		outbound := &ResponseOutbound{}

		outputDoneEvent := []byte(`{"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","call_id":"call_456","name":"exec_command","arguments":"{\"cmd\":\"pwd\"}"}}`)
		outputDoneResp, err := outbound.TransformStream(nil, outputDoneEvent)
		if err != nil {
			t.Fatalf("TransformStream output item done error: %v", err)
		}
		if outputDoneResp == nil || len(outputDoneResp.Choices) != 1 || len(outputDoneResp.Choices[0].Delta.ToolCalls) != 1 {
			t.Fatalf("expected tool call from output_item.done, got %#v", outputDoneResp)
		}
		toolCall := outputDoneResp.Choices[0].Delta.ToolCalls[0]
		if toolCall.Function.Name != "exec_command" || toolCall.Function.Arguments != `{"cmd":"pwd"}` {
			t.Fatalf("expected complete tool call from output_item.done, got %#v", toolCall)
		}
	})
}

func TestResponseOutboundTransformStreamSupportsCompletedEventToolCalls(t *testing.T) {
	outbound := &ResponseOutbound{}

	event := []byte(`{"type":"response.completed","response":{"id":"resp_123","model":"gpt-5","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"我来处理。"}]},{"type":"function_call","call_id":"call_abc","name":"exec_command","arguments":"{\"cmd\":\"pwd\"}"}]}}`)
	resp, err := outbound.TransformStream(nil, event)
	if err != nil {
		t.Fatalf("TransformStream completed event error: %v", err)
	}
	if resp == nil || len(resp.Choices) != 1 {
		t.Fatalf("expected one choice, got %#v", resp)
	}
	if resp.Choices[0].FinishReason == nil || *resp.Choices[0].FinishReason != "tool_calls" {
		t.Fatalf("expected tool_calls finish reason, got %#v", resp.Choices[0].FinishReason)
	}
	if resp.Choices[0].Delta == nil || len(resp.Choices[0].Delta.ToolCalls) != 1 {
		t.Fatalf("expected tool call on completed event, got %#v", resp.Choices[0].Delta)
	}
	toolCall := resp.Choices[0].Delta.ToolCalls[0]
	if toolCall.ID != "call_abc" || toolCall.Function.Name != "exec_command" || toolCall.Function.Arguments != `{"cmd":"pwd"}` {
		t.Fatalf("unexpected tool call from completed event: %#v", toolCall)
	}
	if resp.ID != "resp_123" || resp.Model != "gpt-5" {
		t.Fatalf("expected response metadata to propagate, got id=%q model=%q", resp.ID, resp.Model)
	}
}

func TestResponseOutboundTransformStreamSkipsDuplicateToolCallsAfterArgumentDelta(t *testing.T) {
	outbound := &ResponseOutbound{}

	addedEvent := []byte(`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","call_id":"call_123","name":"exec_command"}}`)
	if _, err := outbound.TransformStream(nil, addedEvent); err != nil {
		t.Fatalf("TransformStream added event error: %v", err)
	}

	deltaEvent := []byte(`{"type":"response.function_call_arguments.delta","output_index":0,"call_id":"call_123","name":"exec_command","delta":"{\"cmd\":\"pwd\"}"}`)
	deltaResp, err := outbound.TransformStream(nil, deltaEvent)
	if err != nil {
		t.Fatalf("TransformStream delta event error: %v", err)
	}
	if deltaResp == nil || len(deltaResp.Choices) != 1 || len(deltaResp.Choices[0].Delta.ToolCalls) != 1 {
		t.Fatalf("expected tool call delta, got %#v", deltaResp)
	}
	if deltaResp.Choices[0].Delta.ToolCalls[0].Function.Arguments != `{"cmd":"pwd"}` {
		t.Fatalf("expected delta arguments, got %#v", deltaResp.Choices[0].Delta.ToolCalls[0])
	}

	doneArgsEvent := []byte(`{"type":"response.function_call_arguments.done","output_index":0,"call_id":"call_123","arguments":"{\"cmd\":\"pwd\"}"}`)
	doneArgsResp, err := outbound.TransformStream(nil, doneArgsEvent)
	if err != nil {
		t.Fatalf("TransformStream done args event error: %v", err)
	}
	if doneArgsResp != nil {
		t.Fatalf("expected done args event to be skipped after delta, got %#v", doneArgsResp)
	}

	outputDoneEvent := []byte(`{"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","call_id":"call_123","name":"exec_command","arguments":"{\"cmd\":\"pwd\"}"}}`)
	outputDoneResp, err := outbound.TransformStream(nil, outputDoneEvent)
	if err != nil {
		t.Fatalf("TransformStream output item done error: %v", err)
	}
	if outputDoneResp != nil {
		t.Fatalf("expected output item done event to be skipped after delta, got %#v", outputDoneResp)
	}

	completedEvent := []byte(`{"type":"response.completed","response":{"id":"resp_123","model":"gpt-5","status":"completed","output":[{"type":"function_call","call_id":"call_123","name":"exec_command","arguments":"{\"cmd\":\"pwd\"}"}]}}`)
	completedResp, err := outbound.TransformStream(nil, completedEvent)
	if err != nil {
		t.Fatalf("TransformStream completed event error: %v", err)
	}
	if completedResp == nil || len(completedResp.Choices) != 1 {
		t.Fatalf("expected completed response, got %#v", completedResp)
	}
	if completedResp.Choices[0].Delta != nil && len(completedResp.Choices[0].Delta.ToolCalls) > 0 {
		t.Fatalf("expected completed event not to repeat tool call after delta, got %#v", completedResp.Choices[0].Delta)
	}
	if completedResp.Choices[0].FinishReason == nil || *completedResp.Choices[0].FinishReason != "tool_calls" {
		t.Fatalf("expected completed event to preserve tool_calls finish reason, got %#v", completedResp.Choices[0].FinishReason)
	}
}

func TestResponseOutboundTransformStreamUsesCompletedEventAsToolCallFallback(t *testing.T) {
	outbound := &ResponseOutbound{}

	event := []byte(`{"type":"response.completed","response":{"id":"resp_456","model":"gpt-5","status":"completed","output":[{"type":"function_call","call_id":"call_fallback","name":"exec_command","arguments":"{\"cmd\":\"pwd\"}"}]}}`)
	resp, err := outbound.TransformStream(nil, event)
	if err != nil {
		t.Fatalf("TransformStream completed event error: %v", err)
	}
	if resp == nil || len(resp.Choices) != 1 {
		t.Fatalf("expected one choice, got %#v", resp)
	}
	if resp.Choices[0].FinishReason == nil || *resp.Choices[0].FinishReason != "tool_calls" {
		t.Fatalf("expected tool_calls finish reason for completed fallback, got %#v", resp.Choices[0].FinishReason)
	}
	if resp.Choices[0].Delta == nil || len(resp.Choices[0].Delta.ToolCalls) != 1 {
		t.Fatalf("expected tool call from completed fallback, got %#v", resp.Choices[0].Delta)
	}
	toolCall := resp.Choices[0].Delta.ToolCalls[0]
	if toolCall.ID != "call_fallback" || toolCall.Function.Name != "exec_command" || toolCall.Function.Arguments != `{"cmd":"pwd"}` {
		t.Fatalf("unexpected completed fallback tool call: %#v", toolCall)
	}
}

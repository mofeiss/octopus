package relay

import (
	"context"
	"encoding/json"
	"testing"

	anthropicInbound "github.com/bestruirui/octopus/internal/transformer/inbound/anthropic"
	openaiInbound "github.com/bestruirui/octopus/internal/transformer/inbound/openai"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
	authropicOutbound "github.com/bestruirui/octopus/internal/transformer/outbound/authropic"
	openaiOutbound "github.com/bestruirui/octopus/internal/transformer/outbound/openai"
)

func TestSnapshotHTTPRequestBodyDiffersAcrossOutboundFormats(t *testing.T) {
	ctx := context.Background()
	raw := []byte(`{"model":"gpt-4.1","messages":[{"role":"developer","content":"you are helpful"},{"role":"user","content":"hello"}],"stream":true}`)

	req, err := (&openaiInbound.ChatInbound{}).TransformRequest(ctx, raw)
	if err != nil {
		t.Fatalf("transform inbound request failed: %v", err)
	}
	req.RawRequest = append([]byte(nil), raw...)
	req.RawAPIFormat = transformerModel.APIFormatOpenAIChatCompletion

	internalJSON := marshalInternalRequestForTest(t, req)

	openAIReq, err := (&openaiOutbound.ChatOutbound{}).TransformRequest(ctx, req, "https://example.com/v1", "key")
	if err != nil {
		t.Fatalf("transform openai outbound request failed: %v", err)
	}
	openAIBody := snapshotHTTPRequestBody(openAIReq)
	if openAIBody == "" {
		t.Fatalf("expected openai outbound body")
	}
	if openAIBody == internalJSON {
		t.Fatalf("expected openai outbound body to differ from internal request when developer role exists\ninternal=%s\noutbound=%s", internalJSON, openAIBody)
	}

	req2, err := (&openaiInbound.ChatInbound{}).TransformRequest(ctx, raw)
	if err != nil {
		t.Fatalf("transform inbound request for anthropic failed: %v", err)
	}
	req2.RawRequest = append([]byte(nil), raw...)
	req2.RawAPIFormat = transformerModel.APIFormatOpenAIChatCompletion
	antReq, err := (&authropicOutbound.MessageOutbound{}).TransformRequest(ctx, req2, "https://example.com/v1", "key")
	if err != nil {
		t.Fatalf("transform anthropic outbound request failed: %v", err)
	}
	antBody := snapshotHTTPRequestBody(antReq)
	if antBody == "" {
		t.Fatalf("expected anthropic outbound body")
	}
	if antBody == internalJSON {
		t.Fatalf("expected anthropic outbound body to differ from internal request\ninternal=%s\noutbound=%s", internalJSON, antBody)
	}

	req3, err := (&anthropicInbound.MessagesInbound{}).TransformRequest(ctx, []byte(`{"model":"claude-3-7-sonnet","max_tokens":64,"messages":[{"role":"user","content":"hello"}]}`))
	if err != nil {
		t.Fatalf("transform anthropic inbound request failed: %v", err)
	}
	req3.RawAPIFormat = transformerModel.APIFormatAnthropicMessage
	respReq, err := (&openaiOutbound.ResponseOutbound{}).TransformRequest(ctx, req3, "https://example.com/v1", "key")
	if err != nil {
		t.Fatalf("transform responses outbound request failed: %v", err)
	}
	respBody := snapshotHTTPRequestBody(respReq)
	if respBody == "" {
		t.Fatalf("expected responses outbound body")
	}
	if respBody == marshalInternalRequestForTest(t, req3) {
		t.Fatalf("expected responses outbound body to differ from internal request")
	}
}

func marshalInternalRequestForTest(t *testing.T, req *transformerModel.InternalLLMRequest) string {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal internal request failed: %v", err)
	}
	return string(body)
}

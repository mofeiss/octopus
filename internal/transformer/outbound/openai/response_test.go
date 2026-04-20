package openai

import (
	"encoding/json"
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

func strPtr(s string) *string {
	return &s
}

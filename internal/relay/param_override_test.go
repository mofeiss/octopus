package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/inbound"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
)

func TestApplyParamOverrideToHTTPRequestDeepMergesAndOverrides(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "https://example.com/v1/responses", bytes.NewReader([]byte(`{
		"model": "gpt-5",
		"stream": true,
		"extra_body": {
			"foo": "bar"
		},
		"metadata": {
			"keep": "yes",
			"replace": "old"
		}
	}`)))
	if err != nil {
		t.Fatalf("new request failed: %v", err)
	}
	override := `{
		"stream": false,
		"extra_body": {
			"reasoning": { "effort": "xhigh" }
		},
		"metadata": {
			"replace": "new"
		}
	}`

	if err := applyParamOverrideToHTTPRequest(req, &override); err != nil {
		t.Fatalf("applyParamOverrideToHTTPRequest failed: %v", err)
	}

	got := decodeRequestBodyForTest(t, req)
	if got["model"] != "gpt-5" {
		t.Fatalf("expected model to be preserved, got %#v", got["model"])
	}
	if got["stream"] != false {
		t.Fatalf("expected stream to be overridden, got %#v", got["stream"])
	}

	extraBody, ok := got["extra_body"].(map[string]any)
	if !ok {
		t.Fatalf("expected extra_body object, got %#v", got["extra_body"])
	}
	if extraBody["foo"] != "bar" {
		t.Fatalf("expected existing extra_body.foo to be preserved, got %#v", extraBody["foo"])
	}
	reasoning, ok := extraBody["reasoning"].(map[string]any)
	if !ok || reasoning["effort"] != "xhigh" {
		t.Fatalf("expected extra_body.reasoning.effort xhigh, got %#v", extraBody["reasoning"])
	}

	metadata, ok := got["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("expected metadata object, got %#v", got["metadata"])
	}
	if metadata["keep"] != "yes" || metadata["replace"] != "new" {
		t.Fatalf("expected metadata to be deep-merged, got %#v", metadata)
	}

	if req.ContentLength <= 0 {
		t.Fatalf("expected content length to be refreshed, got %d", req.ContentLength)
	}
}

func TestApplyParamOverrideToHTTPRequestSkipsEmptyOverride(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "https://example.com/v1/responses", bytes.NewReader([]byte(`{"model":"gpt-5"}`)))
	if err != nil {
		t.Fatalf("new request failed: %v", err)
	}

	empty := "  "
	if err := applyParamOverrideToHTTPRequest(req, &empty); err != nil {
		t.Fatalf("empty override should be ignored, got %v", err)
	}

	got := decodeRequestBodyForTest(t, req)
	if got["model"] != "gpt-5" {
		t.Fatalf("expected original body to remain, got %#v", got)
	}
}

func TestApplyParamOverrideToHTTPRequestRejectsInvalidOverride(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "https://example.com/v1/responses", bytes.NewReader([]byte(`{"model":"gpt-5"}`)))
	if err != nil {
		t.Fatalf("new request failed: %v", err)
	}

	invalid := `["not", "an", "object"]`
	if err := applyParamOverrideToHTTPRequest(req, &invalid); err == nil {
		t.Fatalf("expected non-object override to fail")
	}
}

func TestApplyParamOverrideToPassthroughResponsesRequest(t *testing.T) {
	stream := true
	internalReq := &transformerModel.InternalLLMRequest{
		Model:        "gpt-5-new",
		Stream:       &stream,
		RawAPIFormat: transformerModel.APIFormatOpenAIResponse,
		RawRequest:   []byte(`{"model":"gpt-5-old","stream":true,"input":"hello"}`),
	}
	req, err := buildPassthroughRequest(context.Background(), internalReq, inbound.InboundTypeOpenAIResponse, "https://example.com/v1", "key")
	if err != nil {
		t.Fatalf("buildPassthroughRequest failed: %v", err)
	}
	override := `{"extra_body":{"reasoning":{"effort":"xhigh"}}}`

	if err := applyParamOverrideToHTTPRequest(req, &override); err != nil {
		t.Fatalf("applyParamOverrideToHTTPRequest failed: %v", err)
	}

	got := decodeRequestBodyForTest(t, req)
	if got["model"] != "gpt-5-new" {
		t.Fatalf("expected passthrough model rewrite to remain, got %#v", got["model"])
	}
	extraBody, ok := got["extra_body"].(map[string]any)
	if !ok {
		t.Fatalf("expected extra_body object, got %#v", got["extra_body"])
	}
	reasoning, ok := extraBody["reasoning"].(map[string]any)
	if !ok || reasoning["effort"] != "xhigh" {
		t.Fatalf("expected raw override to be appended, got %#v", extraBody["reasoning"])
	}
}

func decodeRequestBodyForTest(t *testing.T, req *http.Request) map[string]any {
	t.Helper()

	body := snapshotHTTPRequestBody(req)
	var got map[string]any
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("decode request body failed: %v\nbody=%s", err, body)
	}
	return got
}

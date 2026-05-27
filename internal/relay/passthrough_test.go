package relay

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/relay/balancer"
	"github.com/bestruirui/octopus/internal/transformer/inbound"
	openaiInbound "github.com/bestruirui/octopus/internal/transformer/inbound/openai"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	openaiOutbound "github.com/bestruirui/octopus/internal/transformer/outbound/openai"
	"github.com/gin-gonic/gin"
)

func TestShouldPassthroughSameProtocol(t *testing.T) {
	tests := []struct {
		name        string
		inboundType inbound.InboundType
		outbound    outbound.OutboundType
		want        bool
	}{
		{name: "chat", inboundType: inbound.InboundTypeOpenAIChat, outbound: outbound.OutboundTypeOpenAIChat, want: true},
		{name: "responses", inboundType: inbound.InboundTypeOpenAIResponse, outbound: outbound.OutboundTypeOpenAIResponse, want: true},
		{name: "anthropic", inboundType: inbound.InboundTypeAnthropic, outbound: outbound.OutboundTypeAnthropic, want: true},
		{name: "embedding", inboundType: inbound.InboundTypeOpenAIEmbedding, outbound: outbound.OutboundTypeOpenAIEmbedding, want: true},
		{name: "cross protocol", inboundType: inbound.InboundTypeOpenAIResponse, outbound: outbound.OutboundTypeOpenAIChat, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldPassthroughSameProtocol(tt.inboundType, tt.outbound); got != tt.want {
				t.Fatalf("shouldPassthroughSameProtocol() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRewritePassthroughRequestBodyOnlyChangesModel(t *testing.T) {
	raw := []byte(`{"model":"gpt-5-old","stream":true,"tool_choice":"auto","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}]}`)
	req := &transformerModel.InternalLLMRequest{
		Model:      "gpt-5-new",
		RawRequest: raw,
	}

	body, err := rewritePassthroughRequestBody(req)
	if err != nil {
		t.Fatalf("rewritePassthroughRequestBody error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode rewritten body error: %v", err)
	}
	if got["model"] != "gpt-5-new" {
		t.Fatalf("model not rewritten, got %v", got["model"])
	}
	if got["tool_choice"] != "auto" {
		t.Fatalf("tool_choice should be preserved, got %v", got["tool_choice"])
	}
	if got["stream"] != true {
		t.Fatalf("stream should be preserved, got %v", got["stream"])
	}
	input, ok := got["input"].([]any)
	if !ok || len(input) != 1 {
		t.Fatalf("input should be preserved, got %#v", got["input"])
	}
}

func TestPassthroughTargetURLPreservesQuery(t *testing.T) {
	query := url.Values{}
	query.Set("foo", "bar")
	query.Set("x", "1")

	target, err := passthroughTargetURL("https://example.com/v1", inbound.InboundTypeOpenAIResponse, query)
	if err != nil {
		t.Fatalf("passthroughTargetURL error: %v", err)
	}
	if target != "https://example.com/v1/responses?foo=bar&x=1" && target != "https://example.com/v1/responses?x=1&foo=bar" {
		t.Fatalf("unexpected passthrough target url: %s", target)
	}
}

func TestCapturePassthroughInternalStreamAggregatesContent(t *testing.T) {
	ra := &relayAttempt{}
	stop := "stop"
	empty := ""
	first := "你好，"
	second := "世界"

	ra.capturePassthroughInternalStream(&transformerModel.InternalLLMResponse{
		ID:      "resp_123",
		Object:  "chat.completion.chunk",
		Created: 1779446299,
		Model:   "gpt-5.5",
		Choices: []transformerModel.Choice{{
			Index: 0,
			Delta: &transformerModel.Message{Role: "assistant"},
		}},
	})
	ra.capturePassthroughInternalStream(&transformerModel.InternalLLMResponse{
		ID:     "resp_123",
		Object: "chat.completion.chunk",
		Model:  "gpt-5.5",
		Choices: []transformerModel.Choice{{
			Index: 0,
			Delta: &transformerModel.Message{Content: transformerModel.MessageContent{Content: &first}},
		}},
	})
	ra.capturePassthroughInternalStream(&transformerModel.InternalLLMResponse{
		ID:     "resp_123",
		Object: "chat.completion.chunk",
		Model:  "gpt-5.5",
		Choices: []transformerModel.Choice{{
			Index: 0,
			Delta: &transformerModel.Message{Content: transformerModel.MessageContent{Content: &second}},
		}},
	})
	ra.capturePassthroughInternalStream(&transformerModel.InternalLLMResponse{
		ID:     "resp_123",
		Object: "chat.completion.chunk",
		Model:  "gpt-5.5",
		Choices: []transformerModel.Choice{{
			Index:        0,
			Delta:        &transformerModel.Message{Content: transformerModel.MessageContent{Content: &empty}},
			FinishReason: &stop,
		}},
		Usage: &transformerModel.Usage{
			PromptTokens:     10,
			CompletionTokens: 2,
			TotalTokens:      12,
		},
	})

	got := ra.passthroughInternalResponse
	if got == nil || len(got.Choices) != 1 || got.Choices[0].Message == nil || got.Choices[0].Message.Content.Content == nil {
		t.Fatalf("expected aggregated response content, got %#v", got)
	}
	if *got.Choices[0].Message.Content.Content != "你好，世界" {
		t.Fatalf("expected full content to survive final empty chunk, got %q", *got.Choices[0].Message.Content.Content)
	}
	if got.Choices[0].FinishReason == nil || *got.Choices[0].FinishReason != "stop" {
		t.Fatalf("expected finish reason stop, got %#v", got.Choices[0].FinishReason)
	}
	if got.Usage == nil || got.Usage.TotalTokens != 12 {
		t.Fatalf("expected usage to be captured, got %#v", got.Usage)
	}
}

func TestHandlePassthroughStreamResponseFirstTokenTimeout(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	ginCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(ctx)

	bodyReader, bodyWriter := io.Pipe()
	defer bodyWriter.Close()
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       bodyReader,
	}
	ra := &relayAttempt{
		relayRequest: &relayRequest{
			c:         ginCtx,
			metrics:   &RelayMetrics{},
			inAdapter: &openaiInbound.ChatInbound{},
		},
		outAdapter:           &openaiOutbound.ChatOutbound{},
		firstTokenTimeOutSec: 1,
	}

	start := time.Now()
	err := ra.handlePassthroughStreamResponse(ctx, response)
	if err == nil || !strings.Contains(err.Error(), "first token timeout (1s)") {
		t.Fatalf("expected first token timeout, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 1500*time.Millisecond {
		t.Fatalf("timeout took too long: %v", elapsed)
	}
}

func TestEffectiveFirstTokenTimeOutDefaultsToThirtySeconds(t *testing.T) {
	if got := effectiveFirstTokenTimeOutSec(0); got != defaultFirstTokenTimeOutSec {
		t.Fatalf("expected default timeout %d, got %d", defaultFirstTokenTimeOutSec, got)
	}
	if got := effectiveFirstTokenTimeOutSec(9); got != 9 {
		t.Fatalf("expected configured timeout to win, got %d", got)
	}
}

func TestClientCanceledAttemptUsesDedicatedStatus(t *testing.T) {
	iter := balancer.NewIterator(model.Group{
		Mode: model.GroupModeFailover,
		Items: []model.GroupItem{{
			ChannelID: 1,
			ModelName: "gpt-test",
		}},
	}, 1, "gpt-test")
	if !iter.Next() {
		t.Fatal("expected iterator item")
	}
	span := iter.StartAttempt(1, 2, "client-canceled-channel")
	displayErr := clientCanceledDisplayError(context.Canceled)
	span.End(model.AttemptClientCanceled, 0, displayErr.Error())

	attempts := iter.Attempts()
	if len(attempts) != 1 {
		t.Fatalf("expected one attempt, got %d", len(attempts))
	}
	if attempts[0].Status != model.AttemptClientCanceled {
		t.Fatalf("expected client canceled status, got %s", attempts[0].Status)
	}
	if !strings.Contains(attempts[0].Msg, "客户端或前置网关取消请求") {
		t.Fatalf("expected bilingual cancel message, got %q", attempts[0].Msg)
	}
}

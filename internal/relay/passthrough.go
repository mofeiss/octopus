package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/transformer/inbound"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/tmaxmax/go-sse"
)

func shouldPassthroughSameProtocol(inboundType inbound.InboundType, outboundType outbound.OutboundType) bool {
	switch inboundType {
	case inbound.InboundTypeOpenAIChat:
		return outboundType == outbound.OutboundTypeOpenAIChat
	case inbound.InboundTypeOpenAIResponse:
		return outboundType == outbound.OutboundTypeOpenAIResponse
	case inbound.InboundTypeAnthropic:
		return outboundType == outbound.OutboundTypeAnthropic
	case inbound.InboundTypeOpenAIEmbedding:
		return outboundType == outbound.OutboundTypeOpenAIEmbedding
	default:
		return false
	}
}

func buildPassthroughRequest(ctx context.Context, request *transformerModel.InternalLLMRequest, inboundType inbound.InboundType, baseURL, key string) (*http.Request, error) {
	if request == nil {
		return nil, fmt.Errorf("request is nil")
	}

	body, err := rewritePassthroughRequestBody(request)
	if err != nil {
		return nil, err
	}

	targetURL, err := passthroughTargetURL(baseURL, inboundType, request.Query)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create passthrough request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if request.Stream != nil && *request.Stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}

	switch inboundType {
	case inbound.InboundTypeAnthropic:
		req.Header.Set("X-API-Key", key)
		req.Header.Set("Anthropic-Version", "2023-06-01")
	default:
		req.Header.Set("Authorization", "Bearer "+key)
	}

	return req, nil
}

func rewritePassthroughRequestBody(request *transformerModel.InternalLLMRequest) ([]byte, error) {
	if len(request.RawRequest) == 0 {
		return nil, fmt.Errorf("raw request is empty")
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(request.RawRequest, &raw); err != nil {
		return nil, fmt.Errorf("failed to decode raw passthrough request: %w", err)
	}

	modelValue, err := json.Marshal(request.Model)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal passthrough model: %w", err)
	}
	raw["model"] = modelValue

	body, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal passthrough body: %w", err)
	}

	return body, nil
}

func passthroughTargetURL(baseURL string, inboundType inbound.InboundType, query url.Values) (string, error) {
	path, err := passthroughPath(inboundType)
	if err != nil {
		return "", err
	}

	parsedURL, err := url.Parse(strings.TrimSuffix(baseURL, "/"))
	if err != nil {
		return "", fmt.Errorf("failed to parse passthrough base url: %w", err)
	}
	parsedURL.Path = parsedURL.Path + path
	if query != nil {
		parsedURL.RawQuery = query.Encode()
	}
	return parsedURL.String(), nil
}

func passthroughPath(inboundType inbound.InboundType) (string, error) {
	switch inboundType {
	case inbound.InboundTypeOpenAIChat:
		return "/chat/completions", nil
	case inbound.InboundTypeOpenAIResponse:
		return "/responses", nil
	case inbound.InboundTypeAnthropic:
		return "/messages", nil
	case inbound.InboundTypeOpenAIEmbedding:
		return "/embeddings", nil
	default:
		return "", fmt.Errorf("unsupported passthrough inbound type: %d", inboundType)
	}
}

func (ra *relayAttempt) handlePassthroughStreamResponse(ctx context.Context, response *http.Response) error {
	if ct := response.Header.Get("Content-Type"); ct != "" && !strings.Contains(strings.ToLower(ct), "text/event-stream") {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 16*1024))
		return fmt.Errorf("upstream returned non-SSE content-type %q for passthrough stream request: %s", ct, string(body))
	}

	ra.c.Header("Content-Type", "text/event-stream")
	ra.c.Header("Cache-Control", "no-cache")
	ra.c.Header("Connection", "keep-alive")
	ra.c.Header("X-Accel-Buffering", "no")

	firstToken := true
	readCfg := &sse.ReadConfig{MaxEventSize: maxSSEEventSize}
	for ev, err := range sse.Read(response.Body, readCfg) {
		if err != nil {
			return fmt.Errorf("failed to read passthrough stream event: %w", err)
		}

		if internalStream, convErr := ra.outAdapter.TransformStream(ctx, []byte(ev.Data)); convErr == nil && internalStream != nil {
			ra.capturePassthroughInternalStream(internalStream)
		} else if convErr != nil {
			log.Debugf("[fork] passthrough stream usage capture skipped: %v", convErr)
		}

		if firstToken {
			ra.metrics.SetFirstTokenTime(time.Now())
			firstToken = false
		}

		if ev.Type != "" {
			_, _ = ra.c.Writer.Write([]byte("event: " + ev.Type + "\n"))
		}
		_, _ = ra.c.Writer.Write([]byte("data: " + ev.Data + "\n\n"))
		ra.c.Writer.Flush()
	}

	_, _ = ra.c.Writer.Write([]byte("data: [DONE]\n\n"))
	ra.c.Writer.Flush()
	return nil
}

func (ra *relayAttempt) handlePassthroughResponse(ctx context.Context, response *http.Response) error {
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("failed to read passthrough response body: %w", err)
	}
	if len(body) == 0 {
		return fmt.Errorf("passthrough response body is empty")
	}

	if internalResp, convErr := ra.outAdapter.TransformResponse(ctx, &http.Response{
		StatusCode: response.StatusCode,
		Header:     response.Header.Clone(),
		Body:       io.NopCloser(bytes.NewReader(body)),
	}); convErr == nil {
		ra.passthroughInternalResponse = internalResp
	} else {
		log.Debugf("[fork] passthrough response usage capture skipped: %v", convErr)
	}

	contentType := response.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/json"
	}
	ra.c.Data(http.StatusOK, contentType, body)
	return nil
}

func (ra *relayAttempt) capturePassthroughInternalStream(resp *transformerModel.InternalLLMResponse) {
	if resp == nil || resp.Object == "[DONE]" {
		return
	}
	if ra.passthroughInternalResponse == nil {
		ra.passthroughInternalResponse = &transformerModel.InternalLLMResponse{}
	}
	if resp.ID != "" {
		ra.passthroughInternalResponse.ID = resp.ID
	}
	if resp.Model != "" {
		ra.passthroughInternalResponse.Model = resp.Model
	}
	if resp.Object != "" {
		ra.passthroughInternalResponse.Object = resp.Object
	}
	if resp.Created != 0 {
		ra.passthroughInternalResponse.Created = resp.Created
	}
	if resp.SystemFingerprint != "" {
		ra.passthroughInternalResponse.SystemFingerprint = resp.SystemFingerprint
	}
	if resp.ServiceTier != "" {
		ra.passthroughInternalResponse.ServiceTier = resp.ServiceTier
	}
	if resp.Usage != nil {
		ra.passthroughInternalResponse.Usage = resp.Usage
	}
	if len(resp.Choices) > 0 {
		ra.passthroughInternalResponse.Choices = resp.Choices
	}
}

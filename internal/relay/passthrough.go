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
			line := []byte("event: " + ev.Type + "\n")
			_, _ = ra.c.Writer.Write(line)
		}
		dataLine := []byte("data: " + ev.Data + "\n\n")
		_, _ = ra.c.Writer.Write(dataLine)
		ra.c.Writer.Flush()
	}

	doneLine := []byte("data: [DONE]\n\n")
	_, _ = ra.c.Writer.Write(doneLine)
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
	// [fork] passthrough returns the upstream body as-is, but keep both columns explicit in log detail.
	ra.metrics.SetResponseSnapshots(string(body), string(body))
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
	aggregate := ra.passthroughInternalResponse
	if resp.ID != "" {
		aggregate.ID = resp.ID
	}
	if resp.Model != "" {
		aggregate.Model = resp.Model
	}
	if resp.Object != "" {
		if resp.Object == "chat.completion.chunk" {
			if aggregate.Object == "" || aggregate.Object == "chat.completion.chunk" {
				aggregate.Object = "chat.completion"
			}
		} else {
			aggregate.Object = resp.Object
		}
	}
	if resp.Created != 0 {
		aggregate.Created = resp.Created
	}
	if resp.SystemFingerprint != "" {
		aggregate.SystemFingerprint = resp.SystemFingerprint
	}
	if resp.ServiceTier != "" {
		aggregate.ServiceTier = resp.ServiceTier
	}
	if resp.Usage != nil {
		aggregate.Usage = resp.Usage
	}
	for _, choice := range resp.Choices {
		aggregateChoice := passthroughAggregateChoice(aggregate, choice.Index)
		if choice.Message != nil {
			passthroughMergeMessage(aggregateChoice.Message, choice.Message)
		}
		if choice.Delta != nil {
			passthroughMergeMessage(aggregateChoice.Message, choice.Delta)
		}
		if choice.FinishReason != nil {
			aggregateChoice.FinishReason = choice.FinishReason
		}
		if choice.Logprobs != nil {
			if aggregateChoice.Logprobs == nil {
				aggregateChoice.Logprobs = &transformerModel.LogprobsContent{}
			}
			aggregateChoice.Logprobs.Content = append(aggregateChoice.Logprobs.Content, choice.Logprobs.Content...)
		}
	}
}

func passthroughAggregateChoice(resp *transformerModel.InternalLLMResponse, index int) *transformerModel.Choice {
	for i := range resp.Choices {
		if resp.Choices[i].Index == index {
			if resp.Choices[i].Message == nil {
				resp.Choices[i].Message = &transformerModel.Message{}
			}
			return &resp.Choices[i]
		}
	}

	resp.Choices = append(resp.Choices, transformerModel.Choice{
		Index:   index,
		Message: &transformerModel.Message{},
	})
	return &resp.Choices[len(resp.Choices)-1]
}

func passthroughMergeMessage(dst, src *transformerModel.Message) {
	if dst == nil || src == nil {
		return
	}
	if src.Role != "" {
		dst.Role = src.Role
	}
	if src.Content.Content != nil && *src.Content.Content != "" {
		if dst.Content.Content == nil {
			dst.Content.Content = new(string)
		}
		*dst.Content.Content += *src.Content.Content
	}
	if len(src.Content.MultipleContent) > 0 {
		dst.Content.MultipleContent = append(dst.Content.MultipleContent, src.Content.MultipleContent...)
	}
	if len(src.Images) > 0 {
		dst.Content.MultipleContent = append(dst.Content.MultipleContent, src.Images...)
	}
	if reasoning := src.GetReasoningContent(); reasoning != "" {
		if dst.ReasoningContent == nil {
			dst.ReasoningContent = new(string)
		}
		*dst.ReasoningContent += reasoning
	}
	for _, toolCall := range src.ToolCalls {
		dst.ToolCalls = passthroughMergeToolCall(dst.ToolCalls, toolCall)
	}
	if src.Refusal != "" {
		dst.Refusal = src.Refusal
	}
	if src.Audio != nil {
		dst.Audio = src.Audio
	}
}

func passthroughMergeToolCall(toolCalls []transformerModel.ToolCall, delta transformerModel.ToolCall) []transformerModel.ToolCall {
	for i := range toolCalls {
		if toolCalls[i].Index != delta.Index {
			continue
		}
		if delta.ID != "" {
			toolCalls[i].ID = delta.ID
		}
		if delta.Type != "" {
			toolCalls[i].Type = delta.Type
		}
		if delta.Function.Name != "" {
			toolCalls[i].Function.Name += delta.Function.Name
		}
		if delta.Function.Arguments != "" {
			toolCalls[i].Function.Arguments += delta.Function.Arguments
		}
		return toolCalls
	}
	return append(toolCalls, delta)
}

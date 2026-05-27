package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/helper"
	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/relay/balancer"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/transformer/inbound"
	"github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/gin-gonic/gin"
	"github.com/tmaxmax/go-sse"
)

// Handler 处理入站请求并转发到上游服务
func Handler(inboundType inbound.InboundType, c *gin.Context) {
	// 解析请求
	internalRequest, inAdapter, err := parseRequest(inboundType, c)
	if err != nil {
		return
	}
	supportedModels := c.GetString("supported_models")
	if supportedModels != "" {
		supportedModelsArray := strings.Split(supportedModels, ",")
		if !slices.Contains(supportedModelsArray, internalRequest.Model) {
			resp.Error(c, http.StatusBadRequest, "model not supported")
			return
		}
	}

	requestModel := internalRequest.Model
	apiKeyID := c.GetInt("api_key_id")

	// 获取通道分组
	group, err := op.GroupGetMap(requestModel, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusNotFound, "model not found")
		return
	}

	// 创建迭代器（策略排序 + 粘性优先）
	iter := balancer.NewIterator(group, apiKeyID, requestModel)
	if iter.Len() == 0 {
		resp.Error(c, http.StatusServiceUnavailable, "no available channel")
		return
	}

	// 初始化 Metrics
	metrics := NewRelayMetrics(apiKeyID, requestModel, internalRequest)

	// 请求级上下文
	req := &relayRequest{
		c:               c,
		inAdapter:       inAdapter,
		internalRequest: internalRequest,
		metrics:         metrics,
		apiKeyID:        apiKeyID,
		requestModel:    requestModel,
		iter:            iter,
	}

	var lastErr error

	for iter.Next() {
		select {
		case <-c.Request.Context().Done():
			log.Infof("request context canceled by client or upstream proxy, stopping retry")
			metrics.Save(
				c.Request.Context(),
				false,
				newClientCanceledError(c.Request.Context().Err()),
				iter.Attempts(),
			)
			return
		default:
		}

		item := iter.Item()

		// 获取通道
		channel, err := op.ChannelGet(item.ChannelID, c.Request.Context())
		if err != nil {
			log.Warnf("failed to get channel %d: %v", item.ChannelID, err)
			iter.Skip(item.ChannelID, 0, fmt.Sprintf("channel_%d", item.ChannelID), fmt.Sprintf("channel not found: %v", err))
			lastErr = err
			continue
		}
		if !channel.Enabled {
			iter.Skip(channel.ID, 0, channel.Name, "channel disabled")
			continue
		}

		// [fork] GroupItem 级启用检查
		if !item.Enabled {
			iter.Skip(channel.ID, 0, channel.Name, "group item disabled")
			continue
		}

		// [fork] 获取所有可用 key（按 TotalCost 升序、LastUseTimeStamp 升序）
		keys := channel.GetChannelKeys()
		if len(keys) == 0 {
			iter.Skip(channel.ID, 0, channel.Name, "no available key")
			continue
		}

		resolvedType := channel.Type
		if channel.Type == outbound.OutboundTypeAuto {
			resolvedType, err = outbound.ResolveAutoByInboundType(inboundType)
			if err != nil {
				iter.Skip(channel.ID, 0, channel.Name, err.Error())
				lastErr = err
				continue
			}
		}

		// 出站适配器（渠道级检查）
		outAdapter := outbound.Get(resolvedType)
		if outAdapter == nil {
			iter.Skip(channel.ID, 0, channel.Name, fmt.Sprintf("unsupported channel type: %d", resolvedType))
			continue
		}

		// 类型兼容性检查（渠道级）
		if internalRequest.IsEmbeddingRequest() && !outbound.IsEmbeddingChannelType(resolvedType) {
			iter.Skip(channel.ID, 0, channel.Name, "channel type not compatible with embedding request")
			continue
		}
		if internalRequest.IsChatRequest() && !outbound.IsChatChannelType(resolvedType) {
			iter.Skip(channel.ID, 0, channel.Name, "channel type not compatible with chat request")
			continue
		}

		// 设置实际模型
		internalRequest.Model = item.ModelName

		// [fork] 内层循环：遍历该渠道的所有可用 key
		for _, usedKey := range keys {
			// 熔断检查（key 级粒度）
			if iter.SkipCircuitBreak(channel.ID, usedKey.ID, channel.Name) {
				continue
			}

			log.Infof("request model %s, mode: %d, forwarding to channel: %s model: %s key: %d (attempt %d/%d, sticky=%t)",
				requestModel, group.Mode, channel.Name, item.ModelName, usedKey.ID,
				iter.Index()+1, iter.Len(), iter.IsSticky())

			// 构造尝试级上下文
			ra := &relayAttempt{
				relayRequest:         req,
				outAdapter:           outAdapter,
				resolvedType:         resolvedType,
				channel:              channel,
				usedKey:              usedKey,
				firstTokenTimeOutSec: effectiveFirstTokenTimeOutSec(group.FirstTokenTimeOut),
			}

			result := ra.attempt()
			if result.Success {
				metrics.Save(c.Request.Context(), true, nil, iter.Attempts())
				return
			}
			if isClientCanceledError(result.Err) {
				metrics.Save(c.Request.Context(), false, result.Err, iter.Attempts())
				return
			}
			if result.Written {
				metrics.Save(c.Request.Context(), false, result.Err, iter.Attempts())
				return
			}
			lastErr = result.Err
		}
	}

	// 所有通道都失败
	metrics.Save(c.Request.Context(), false, lastErr, iter.Attempts())
	resp.Error(c, http.StatusBadGateway, "all channels failed")
}

// attempt 统一管理一次通道尝试的完整生命周期
func (ra *relayAttempt) attempt() attemptResult {
	span := ra.iter.StartAttempt(ra.channel.ID, ra.usedKey.ID, ra.channel.Name)

	// [fork] capture the final body written to the inbound client for log detail.
	originalWriter := ra.c.Writer
	writerSnapshot := &captureResponseWriter{ResponseWriter: originalWriter}
	ra.c.Writer = writerSnapshot
	defer func() {
		ra.metrics.SetResponseSnapshots(ra.metrics.OriginalResponseContent, writerSnapshot.CapturedBody())
		ra.c.Writer = originalWriter
	}()

	// 转发请求
	statusCode, fwdErr := ra.forward()
	ra.metrics.SetOutboundRequest(ra.outboundRequestContent, ra.outboundRequestProtocol)

	// 更新 channel key 状态
	ra.usedKey.StatusCode = statusCode
	ra.usedKey.LastUseTimeStamp = time.Now().Unix()

	if fwdErr == nil {
		// ====== 成功 ======
		ra.collectResponse()
		ra.captureStreamPreview()
		ra.usedKey.TotalCost += ra.metrics.Stats.InputCost + ra.metrics.Stats.OutputCost
		op.ChannelKeyUpdate(ra.usedKey)

		span.End(dbmodel.AttemptSuccess, statusCode, "")

		// Channel 维度统计
		op.StatsChannelUpdate(ra.channel.ID, dbmodel.StatsMetrics{
			WaitTime:       span.Duration().Milliseconds(),
			RequestSuccess: 1,
		})

		// 熔断器：记录成功
		balancer.RecordSuccess(ra.channel.ID, ra.usedKey.ID, ra.internalRequest.Model)
		// 会话保持：更新粘性记录
		balancer.SetSticky(ra.apiKeyID, ra.requestModel, ra.channel.ID, ra.usedKey.ID)

		return attemptResult{Success: true}
	}

	// ====== 失败 ======
	op.ChannelKeyUpdate(ra.usedKey)
	clientCanceled := isClientCanceledError(fwdErr)
	displayErr := fwdErr
	if clientCanceled {
		displayErr = clientCanceledDisplayError(fwdErr)
	}
	attemptStatus := dbmodel.AttemptFailed
	if clientCanceled {
		attemptStatus = dbmodel.AttemptClientCanceled
	}
	span.End(attemptStatus, statusCode, displayErr.Error())

	// Channel 维度统计
	channelMetrics := dbmodel.StatsMetrics{WaitTime: span.Duration().Milliseconds()}
	if clientCanceled {
		log.Infof("client/proxy canceled relay attempt; skip channel failure accounting and circuit breaker")
	} else {
		channelMetrics.RequestFailed = 1
	}
	op.StatsChannelUpdate(ra.channel.ID, channelMetrics)

	// 熔断器：记录失败
	if !clientCanceled {
		balancer.RecordFailure(ra.channel.ID, ra.usedKey.ID, ra.internalRequest.Model)
	}

	written := ra.c.Writer.Written()
	if written {
		ra.collectResponse()
		ra.captureStreamPreview()
	}
	return attemptResult{
		Success: false,
		Written: written,
		Err:     fmt.Errorf("channel %s failed: %w", ra.channel.Name, displayErr),
	}
}

// parseRequest 解析并验证入站请求
func parseRequest(inboundType inbound.InboundType, c *gin.Context) (*model.InternalLLMRequest, model.Inbound, error) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return nil, nil, err
	}

	inAdapter := inbound.Get(inboundType)
	internalRequest, err := inAdapter.TransformRequest(c.Request.Context(), body)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return nil, nil, err
	}
	internalRequest.RawRequest = append([]byte(nil), body...)
	if internalRequest.RawAPIFormat == "" {
		internalRequest.RawAPIFormat = rawAPIFormatFromInboundType(inboundType)
	}

	// Pass through the original query parameters
	internalRequest.Query = c.Request.URL.Query()

	if err := internalRequest.Validate(); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return nil, nil, err
	}

	return internalRequest, inAdapter, nil
}

// forward 转发请求到上游服务
func (ra *relayAttempt) forward() (int, error) {
	ctx := ra.c.Request.Context()
	passthrough := shouldPassthroughSameProtocol(ra.inboundType(), ra.resolvedType)

	// 构建出站请求
	var outboundRequest *http.Request
	var err error
	if passthrough {
		outboundRequest, err = buildPassthroughRequest(ctx, ra.internalRequest, ra.inboundType(), ra.channel.GetBaseUrl(), ra.usedKey.ChannelKey)
	} else {
		outboundRequest, err = ra.outAdapter.TransformRequest(
			ctx,
			ra.internalRequest,
			ra.channel.GetBaseUrl(),
			ra.usedKey.ChannelKey,
		)
	}
	if err != nil {
		log.Warnf("failed to create request: %v", err)
		return 0, fmt.Errorf("failed to create request: %w", err)
	}
	if err := applyParamOverrideToHTTPRequest(outboundRequest, ra.channel.ParamOverride); err != nil {
		log.Warnf("failed to apply param override: %v", err)
		return 0, fmt.Errorf("failed to apply param override: %w", err)
	}
	ra.outboundRequestContent = snapshotHTTPRequestBody(outboundRequest)
	ra.outboundRequestProtocol = relayProtocolNameFromOutboundType(ra.resolvedType)

	// 复制请求头
	ra.copyHeaders(outboundRequest)

	// 发送请求
	response, err := ra.sendRequest(outboundRequest)
	if err != nil {
		return 0, fmt.Errorf("failed to send request: %w", err)
	}
	defer response.Body.Close()

	// [fork] tee the upstream body so logs keep the raw response before conversion.
	rawResponseSnapshot := &bytes.Buffer{}
	response.Body = teeReadCloser{
		Reader: io.TeeReader(response.Body, rawResponseSnapshot),
		Closer: response.Body,
	}
	defer func() {
		if rawResponseSnapshot.Len() > 0 {
			ra.metrics.OriginalResponseContent = rawResponseSnapshot.String()
		}
	}()

	// 检查响应状态
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, err := io.ReadAll(response.Body)
		if err != nil {
			return 0, fmt.Errorf("failed to read response body: %w", err)
		}
		// [fork] keep upstream error payload visible in log detail.
		ra.metrics.SetResponseSnapshots(string(body), "")
		return response.StatusCode, fmt.Errorf("upstream error: %d: %s", response.StatusCode, string(body))
	}

	// 处理响应
	if ra.internalRequest.Stream != nil && *ra.internalRequest.Stream {
		if passthrough {
			err = ra.handlePassthroughStreamResponse(ctx, response)
		} else {
			err = ra.handleStreamResponse(ctx, response)
		}
		if err != nil {
			return 0, err
		}
		return response.StatusCode, nil
	}
	if passthrough {
		err = ra.handlePassthroughResponse(ctx, response)
	} else {
		err = ra.handleResponse(ctx, response)
	}
	if err != nil {
		return 0, err
	}
	return response.StatusCode, nil
}

func effectiveFirstTokenTimeOutSec(configured int) int {
	if configured > 0 {
		return configured
	}
	return defaultFirstTokenTimeOutSec
}

// [fork] Preserve Close while teeing the upstream body for diagnostics.
type teeReadCloser struct {
	io.Reader
	io.Closer
}

func newFirstTokenTimeoutError(seconds int) error {
	return fmt.Errorf("first token timeout (%ds)", seconds)
}

func newClientCanceledError(cause error) error {
	if cause == nil {
		cause = context.Canceled
	}
	return fmt.Errorf("client/proxy canceled request: 客户端或前置网关取消请求: %w", cause)
}

func clientCanceledDisplayError(err error) error {
	if err != nil && strings.Contains(err.Error(), "client/proxy canceled request") {
		return err
	}
	return newClientCanceledError(err)
}

func isClientCanceledError(err error) bool {
	return errors.Is(err, context.Canceled)
}

func (ra *relayAttempt) inboundType() inbound.InboundType {
	switch ra.internalRequest.RawAPIFormat {
	case model.APIFormatOpenAIChatCompletion:
		return inbound.InboundTypeOpenAIChat
	case model.APIFormatOpenAIResponse:
		return inbound.InboundTypeOpenAIResponse
	case model.APIFormatAnthropicMessage:
		return inbound.InboundTypeAnthropic
	case model.APIFormatOpenAIEmbedding:
		return inbound.InboundTypeOpenAIEmbedding
	default:
		return inbound.InboundTypeOpenAIChat
	}
}

// copyHeaders 复制请求头，过滤 hop-by-hop 头
func (ra *relayAttempt) copyHeaders(outboundRequest *http.Request) {
	for key, values := range ra.c.Request.Header {
		if hopByHopHeaders[strings.ToLower(key)] {
			continue
		}
		for _, value := range values {
			outboundRequest.Header.Set(key, value)
		}
	}
	if len(ra.channel.CustomHeader) > 0 {
		for _, header := range ra.channel.CustomHeader {
			outboundRequest.Header.Set(header.HeaderKey, header.HeaderValue)
		}
	}
}

// sendRequest 发送 HTTP 请求
func (ra *relayAttempt) sendRequest(req *http.Request) (*http.Response, error) {
	httpClient, err := helper.ChannelHttpClient(ra.channel)
	if err != nil {
		log.Warnf("failed to get http client: %v", err)
		return nil, err
	}

	response, err := httpClient.Do(req)
	if err != nil {
		log.Warnf("failed to send request: %v", err)
		return nil, err
	}

	return response, nil
}

// handleStreamResponse 处理流式响应
func (ra *relayAttempt) handleStreamResponse(ctx context.Context, response *http.Response) error {
	if ct := response.Header.Get("Content-Type"); ct != "" && !strings.Contains(strings.ToLower(ct), "text/event-stream") {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 16*1024))
		return fmt.Errorf("upstream returned non-SSE content-type %q for stream request: %s", ct, string(body))
	}

	// 设置 SSE 响应头
	ra.c.Header("Content-Type", "text/event-stream")
	ra.c.Header("Cache-Control", "no-cache")
	ra.c.Header("Connection", "keep-alive")
	ra.c.Header("X-Accel-Buffering", "no")

	firstToken := true

	type sseReadResult struct {
		data string
		err  error
	}
	results := make(chan sseReadResult, 1)
	go func() {
		defer close(results)
		readCfg := &sse.ReadConfig{MaxEventSize: maxSSEEventSize}
		for ev, err := range sse.Read(response.Body, readCfg) {
			if err != nil {
				results <- sseReadResult{err: err}
				return
			}
			results <- sseReadResult{data: ev.Data}
		}
	}()

	var firstTokenTimer *time.Timer
	var firstTokenC <-chan time.Time
	if firstToken && ra.firstTokenTimeOutSec > 0 {
		firstTokenTimer = time.NewTimer(time.Duration(ra.firstTokenTimeOutSec) * time.Second)
		firstTokenC = firstTokenTimer.C
		defer func() {
			if firstTokenTimer != nil {
				firstTokenTimer.Stop()
			}
		}()
	}

	for {
		select {
		case <-ctx.Done():
			log.Infof("client/proxy canceled request, stopping stream")
			return newClientCanceledError(ctx.Err())
		case <-firstTokenC:
			log.Warnf("first token timeout (%ds), switching channel", ra.firstTokenTimeOutSec)
			_ = response.Body.Close()
			return newFirstTokenTimeoutError(ra.firstTokenTimeOutSec)
		case r, ok := <-results:
			if !ok {
				log.Infof("stream end")
				return nil
			}
			if r.err != nil {
				log.Warnf("failed to read event: %v", r.err)
				return fmt.Errorf("failed to read stream event: %w", r.err)
			}

			data, err := ra.transformStreamData(ctx, r.data)
			if err != nil || len(data) == 0 {
				continue
			}
			if firstToken {
				ra.metrics.SetFirstTokenTime(time.Now())
				firstToken = false
				if firstTokenTimer != nil {
					if !firstTokenTimer.Stop() {
						select {
						case <-firstTokenTimer.C:
						default:
						}
					}
					firstTokenTimer = nil
					firstTokenC = nil
				}
			}

			ra.c.Writer.Write(data)
			ra.c.Writer.Flush()
		}
	}
}

// transformStreamData 转换流式数据
func (ra *relayAttempt) transformStreamData(ctx context.Context, data string) ([]byte, error) {
	internalStream, err := ra.outAdapter.TransformStream(ctx, []byte(data))
	if err != nil {
		log.Warnf("failed to transform stream: %v", err)
		return nil, err
	}
	if internalStream == nil {
		return nil, nil
	}

	inStream, err := ra.inAdapter.TransformStream(ctx, internalStream)
	if err != nil {
		log.Warnf("failed to transform stream: %v", err)
		return nil, err
	}

	return inStream, nil
}

// handleResponse 处理非流式响应
func (ra *relayAttempt) handleResponse(ctx context.Context, response *http.Response) error {
	internalResponse, err := ra.outAdapter.TransformResponse(ctx, response)
	if err != nil {
		log.Warnf("failed to transform response: %v", err)
		return fmt.Errorf("failed to transform outbound response: %w", err)
	}

	inResponse, err := ra.inAdapter.TransformResponse(ctx, internalResponse)
	if err != nil {
		log.Warnf("failed to transform response: %v", err)
		return fmt.Errorf("failed to transform inbound response: %w", err)
	}

	ra.c.Data(http.StatusOK, "application/json", inResponse)
	return nil
}

// collectResponse 收集响应信息
func (ra *relayAttempt) collectResponse() {
	if ra.passthroughInternalResponse != nil {
		ra.metrics.SetInternalResponse(ra.passthroughInternalResponse, ra.internalRequest.Model)
		return
	}

	internalResponse, err := ra.inAdapter.GetInternalResponse(ra.c.Request.Context())
	if err != nil || internalResponse == nil {
		return
	}

	ra.metrics.SetInternalResponse(internalResponse, ra.internalRequest.Model)
}

// [fork] build a final JSON preview for stream logs from the aggregated internal response.
func (ra *relayAttempt) captureStreamPreview() {
	if ra.metrics == nil || ra.metrics.InternalResponse == nil || ra.internalRequest == nil || ra.internalRequest.Stream == nil || !*ra.internalRequest.Stream {
		return
	}

	inboundAdapter := ra.inAdapter
	if inboundAdapter == nil {
		return
	}

	previewBody, err := inboundAdapter.TransformResponse(ra.c.Request.Context(), ra.metrics.InternalResponse)
	if err != nil || len(previewBody) == 0 {
		return
	}

	if json.Valid(previewBody) {
		ra.metrics.SetStreamPreviewContent(string(previewBody))
		return
	}

	// Fallback: keep the raw body if the inbound adapter already emitted a non-JSON stream snapshot.
	ra.metrics.SetStreamPreviewContent(string(previewBody))
}

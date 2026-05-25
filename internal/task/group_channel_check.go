package task

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/helper"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	authropicOutbound "github.com/bestruirui/octopus/internal/transformer/outbound/authropic"
	openaiOutbound "github.com/bestruirui/octopus/internal/transformer/outbound/openai"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/snowflake"
	"github.com/tmaxmax/go-sse"
)

const (
	groupChannelCheckQueueSize      = 128
	groupChannelCheckWorkerCount    = 2
	groupChannelCheckBatchParallel  = 5 // [fork] 批量测活按渠道并发，单渠道串行
	groupChannelCheckRequestTimeout = 45 * time.Second
	groupChannelCheckPreviewLimit   = 16 * 1024
)

var (
	groupChannelCheckQueue     chan int64
	groupChannelCheckQueueOnce sync.Once
	groupChannelCheckExecuting sync.Map
)

type groupChannelCheckProbeResult struct {
	requestKind          string
	requestURL           string
	baseURL              string
	channelKeyID         int
	channelKeyIndex      int
	channelKeyPreview    string
	channelKeyRemark     string
	responseStatusCode   int
	durationMs           int
	requestContent       string
	openAIRequestCurl    string
	anthropicRequestCurl string
	responsePreview      string
	responseContent      string
	attempts             []model.GroupChannelCheckAttempt
	err                  error
}

func InitGroupChannelCheckQueue() {
	groupChannelCheckQueueOnce.Do(func() {
		groupChannelCheckQueue = make(chan int64, groupChannelCheckQueueSize)
		for i := 0; i < groupChannelCheckWorkerCount; i++ {
			go runGroupChannelCheckWorker()
		}
		go resumeGroupChannelCheckTasks()
	})
}

func EnqueueGroupChannelCheckTask(taskID int64) error {
	if taskID <= 0 {
		return fmt.Errorf("invalid task id")
	}
	InitGroupChannelCheckQueue()
	select {
	case groupChannelCheckQueue <- taskID:
		return nil
	default:
		return fmt.Errorf("channel check queue is full")
	}
}

func runGroupChannelCheckWorker() {
	for taskID := range groupChannelCheckQueue {
		executeGroupChannelCheckTask(taskID)
	}
}

func resumeGroupChannelCheckTasks() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	taskIDs, err := op.GroupChannelCheckTaskListRunnableIDs(ctx)
	if err != nil {
		log.Warnf("resume group channel check tasks failed: %v", err)
		return
	}
	for _, taskID := range taskIDs {
		if err := EnqueueGroupChannelCheckTask(taskID); err != nil {
			log.Warnf("requeue group channel check task failed (task=%d): %v", taskID, err)
			return
		}
	}
}

func executeGroupChannelCheckTask(taskID int64) {
	if _, loaded := groupChannelCheckExecuting.LoadOrStore(taskID, struct{}{}); loaded {
		return
	}
	defer groupChannelCheckExecuting.Delete(taskID)

	ctx := context.Background()
	task, err := op.GroupChannelCheckTaskGet(taskID, ctx)
	if err != nil {
		log.Warnf("group channel check task not found (task=%d): %v", taskID, err)
		return
	}

	startedAt := task.StartedAt
	if startedAt == 0 {
		startedAt = time.Now().Unix()
	}
	if err := op.GroupChannelCheckTaskUpdate(taskID, map[string]any{
		"status":     model.GroupChannelCheckTaskStatusRunning,
		"started_at": startedAt,
		"last_error": "",
	}, ctx); err != nil {
		log.Warnf("failed to mark group channel check task running (task=%d): %v", taskID, err)
		return
	}
	if _, err := op.GroupChannelCheckTaskRefreshSummary(taskID, ctx); err != nil {
		log.Warnf("failed to refresh group channel check summary (task=%d): %v", taskID, err)
	}

	lastErr := executeGroupChannelCheckTaskItems(task, ctx)

	taskAfter, err := op.GroupChannelCheckTaskRefreshSummary(taskID, ctx)
	if err != nil {
		log.Warnf("failed to finalize group channel check summary (task=%d): %v", taskID, err)
		return
	}

	hasRunnableItems := false // [fork] queued 项未点击发送，不算异常未完成。
	if pendingItems, pendingErr := op.GroupChannelCheckTaskListPendingItems(taskID, ctx); pendingErr == nil && len(pendingItems) > 0 {
		hasRunnableItems = true
	}
	finalUpdates := map[string]any{
		"last_error": lastErr,
		"status":     taskAfter.Status,
	}
	if taskAfter.Status != model.GroupChannelCheckTaskStatusPending {
		finalUpdates["finished_at"] = taskAfter.FinishedAt
	}
	if taskAfter.Status == model.GroupChannelCheckTaskStatusRunning || (taskAfter.Status == model.GroupChannelCheckTaskStatusPending && hasRunnableItems) {
		finalUpdates["status"] = model.GroupChannelCheckTaskStatusFailed
		finalUpdates["finished_at"] = time.Now().Unix()
		if lastErr == "" {
			finalUpdates["last_error"] = "task finished unexpectedly"
		}
	}
	if err := op.GroupChannelCheckTaskUpdate(taskID, finalUpdates, ctx); err != nil {
		log.Warnf("failed to finalize group channel check task (task=%d): %v", taskID, err)
	}
}

func executeGroupChannelCheckTaskItems(task *model.GroupChannelCheckTask, ctx context.Context) string {
	channelParallel := 1
	if task != nil && task.Mode == model.GroupChannelCheckTaskModeBatch {
		channelParallel = groupChannelCheckBatchParallel
	}

	activeChannels := make(map[int]struct{}, channelParallel)
	var activeMu sync.Mutex
	var waitGroup sync.WaitGroup

	lastErr := ""
	setLastErr := func(err error) {
		if err == nil {
			return
		}
		activeMu.Lock()
		lastErr = err.Error()
		activeMu.Unlock()
	}
	setLastErrText := func(text string) {
		if strings.TrimSpace(text) == "" {
			return
		}
		activeMu.Lock()
		lastErr = text
		activeMu.Unlock()
	}
	getLastErr := func() string {
		activeMu.Lock()
		defer activeMu.Unlock()
		return lastErr
	}

	idleRetry := 0
	stopScheduling := false
	for {
		launched := false
		for {
			activeMu.Lock()
			if len(activeChannels) >= channelParallel || stopScheduling {
				activeMu.Unlock()
				break
			}
			busyChannels := make(map[int]struct{}, len(activeChannels))
			for channelID := range activeChannels {
				busyChannels[channelID] = struct{}{}
			}
			activeMu.Unlock()

			item, nextErr := nextGroupChannelCheckPendingItem(task.ID, busyChannels, ctx)
			if nextErr != nil {
				log.Warnf("failed to load next group channel check item (task=%d): %v", task.ID, nextErr)
				setLastErr(nextErr)
				stopScheduling = true
				break
			}
			if item == nil {
				break
			}

			activeMu.Lock()
			if _, exists := activeChannels[item.ChannelID]; exists {
				activeMu.Unlock()
				continue
			}
			activeChannels[item.ChannelID] = struct{}{}
			activeMu.Unlock()

			launched = true
			idleRetry = 0
			waitGroup.Add(1)
			go func(item *model.GroupChannelCheckTaskItem) {
				defer waitGroup.Done()
				if err := processGroupChannelCheckTaskItem(task, item, ctx); err != nil {
					setLastErr(err)
				}
				activeMu.Lock()
				delete(activeChannels, item.ChannelID)
				activeMu.Unlock()
			}(item)
		}

		if stopScheduling {
			break
		}

		activeMu.Lock()
		activeCount := len(activeChannels)
		activeMu.Unlock()
		if activeCount == 0 {
			taskAfter, refreshErr := op.GroupChannelCheckTaskRefreshSummary(task.ID, ctx)
			if refreshErr != nil {
				log.Warnf("failed to finalize group channel check summary (task=%d): %v", task.ID, refreshErr)
				setLastErr(refreshErr)
				break
			}
			if taskAfter.Status == model.GroupChannelCheckTaskStatusPending || taskAfter.Status == model.GroupChannelCheckTaskStatusRunning {
				pendingItems, pendingErr := op.GroupChannelCheckTaskListPendingItems(task.ID, ctx)
				if pendingErr != nil {
					setLastErr(pendingErr)
					break
				}
				if len(pendingItems) == 0 { // [fork] 剩余 queued 项等待手动点击发送，不阻塞本次 worker。
					break
				}
				if idleRetry >= 5 {
					setLastErrText("task finished unexpectedly")
					break
				}
				idleRetry++
				time.Sleep(200 * time.Millisecond)
				continue
			}
			break
		}

		if !launched {
			time.Sleep(50 * time.Millisecond)
		}
	}

	waitGroup.Wait()
	return getLastErr()
}

func nextGroupChannelCheckPendingItem(taskID int64, busyChannels map[int]struct{}, ctx context.Context) (*model.GroupChannelCheckTaskItem, error) {
	items, err := op.GroupChannelCheckTaskListPendingItems(taskID, ctx)
	if err != nil {
		return nil, err
	}
	for idx := range items {
		if _, busy := busyChannels[items[idx].ChannelID]; busy {
			continue
		}
		item := items[idx]
		return &item, nil
	}
	return nil, nil
}

func processGroupChannelCheckTaskItem(task *model.GroupChannelCheckTask, item *model.GroupChannelCheckTaskItem, ctx context.Context) error {
	if task == nil || item == nil {
		return fmt.Errorf("group channel check task item is nil")
	}

	itemStart := time.Now().Unix()
	if err := op.GroupChannelCheckTaskItemUpdate(item.ID, map[string]any{
		"status":               model.GroupChannelCheckItemStatusRunning,
		"started_at":           itemStart,
		"finished_at":          int64(0),
		"error":                "",
		"response_status_code": 0,
	}, ctx); err != nil {
		log.Warnf("failed to mark group channel check item running (item=%d): %v", item.ID, err)
		return err
	}
	if _, err := op.GroupChannelCheckTaskRefreshSummary(task.ID, ctx); err != nil {
		log.Warnf("failed to refresh running group channel check summary (task=%d): %v", task.ID, err)
	}

	result := probeGroupChannelCheckItem(item.ChannelID, item.ModelName, model.GroupChannelCheckProtocol(item.RequestKind))
	finishedAt := time.Now().Unix()
	finishedItem := *item
	finishedItem.RequestKind = result.requestKind
	finishedItem.RequestURL = result.requestURL
	finishedItem.BaseURL = result.baseURL
	finishedItem.ChannelKeyID = result.channelKeyID
	finishedItem.ChannelKeyIndex = result.channelKeyIndex
	finishedItem.ChannelKeyPreview = result.channelKeyPreview
	finishedItem.ChannelKeyRemark = result.channelKeyRemark
	finishedItem.ResponseStatusCode = result.responseStatusCode
	finishedItem.DurationMs = result.durationMs
	finishedItem.RequestContent = result.requestContent
	finishedItem.OpenAIRequestCurl = result.openAIRequestCurl
	finishedItem.AnthropicRequestCurl = result.anthropicRequestCurl
	finishedItem.ResponsePreview = result.responsePreview
	finishedItem.ResponseContent = result.responseContent
	finishedItem.FinishedAt = finishedAt
	finishedItem.Attempts = result.attempts
	if result.err != nil {
		finishedItem.Status = model.GroupChannelCheckItemStatusFailed
		finishedItem.Error = result.err.Error()
	} else {
		finishedItem.Status = model.GroupChannelCheckItemStatusSuccess
		finishedItem.Error = ""
	}

	if err := op.GroupChannelCheckTaskItemSaveResult(finishedItem, ctx); err != nil {
		log.Warnf("failed to update group channel check item result (item=%d): %v", item.ID, err)
		return err
	}

	group, groupErr := op.GroupGet(task.GroupID, ctx)
	if groupErr == nil {
		if err := op.GroupChannelCheckStateUpsertByItem(*group, finishedItem, ctx); err != nil {
			log.Warnf("failed to sync group channel check state (item=%d): %v", item.ID, err)
		}
	}
	if _, err := op.GroupChannelCheckTaskRefreshSummary(task.ID, ctx); err != nil {
		log.Warnf("failed to refresh finished group channel check summary (task=%d): %v", task.ID, err)
	}

	return result.err
}

func probeGroupChannelCheckItem(channelID int, modelName string, protocol model.GroupChannelCheckProtocol) (result groupChannelCheckProbeResult) {
	startTime := time.Now()
	defer func() {
		result.durationMs = int(time.Since(startTime).Milliseconds())
	}()

	ctx, cancel := context.WithTimeout(context.Background(), groupChannelCheckRequestTimeout)
	defer cancel()

	channel, err := op.ChannelGet(channelID, ctx)
	if err != nil {
		result.err = fmt.Errorf("channel not found: %w", err)
		return
	}

	baseURL := strings.TrimSpace(channel.GetBaseUrl())
	result.baseURL = baseURL
	if baseURL == "" {
		result.err = fmt.Errorf("channel base url is empty")
		return
	}

	keys := channel.GetChannelKeys()
	if len(keys) == 0 {
		result.err = fmt.Errorf("no available channel key")
		return
	}

	resolvedType, err := resolveGroupChannelCheckProtocol(protocol)
	if err != nil {
		result.err = err
		return
	}

	request, requestKind, err := buildGroupChannelCheckRequest(resolvedType, modelName)
	if err != nil {
		result.err = err
		return
	}
	result.requestKind = requestKind
	if err := request.Validate(); err != nil {
		result.err = fmt.Errorf("invalid check request: %w", err)
		return
	}

	outAdapter := outbound.Get(resolvedType)
	if outAdapter == nil {
		result.err = fmt.Errorf("unsupported channel type: %d", resolvedType)
		return
	}

	// [fork] 多 key 渠道测活对齐 relay 语义：任意 1 个 key 成功即视为渠道可用。
	attempts := make([]model.GroupChannelCheckAttempt, 0, len(keys))
	failures := make([]string, 0, len(keys))
	for _, usedKey := range keys {
		attempt := probeGroupChannelCheckItemWithKey(ctx, channel, request, requestKind, outAdapter, usedKey)
		attempts = append(attempts, buildGroupChannelCheckAttempt(attempt))
		if attempt.err == nil {
			attempt.attempts = attempts
			return attempt
		}
		result = attempt
		failures = append(failures, summarizeGroupChannelCheckKeyFailure(attempt))
	}

	result.attempts = attempts
	if len(failures) > 0 {
		summary := fmt.Sprintf("all %d keys failed", len(keys))
		result.responsePreview = summary
		result.responseContent = trimProbePayload(summary + "\n\n" + strings.Join(failures, "\n\n"))
		if result.err != nil {
			result.err = fmt.Errorf("%s: %w", summary, result.err)
		} else {
			result.err = errors.New(summary)
		}
	}
	return
}

func buildGroupChannelCheckAttempt(result groupChannelCheckProbeResult) model.GroupChannelCheckAttempt {
	status := model.GroupChannelCheckItemStatusSuccess
	if result.err != nil {
		status = model.GroupChannelCheckItemStatusFailed
	}

	responseContent := strings.TrimSpace(result.responseContent)
	if responseContent == "" {
		responseContent = strings.TrimSpace(result.responsePreview)
	}
	if responseContent == "" && result.err != nil {
		responseContent = strings.TrimSpace(result.err.Error())
	}

	return model.GroupChannelCheckAttempt{
		Status:               status,
		ChannelKeyID:         result.channelKeyID,
		ChannelKeyIndex:      result.channelKeyIndex,
		ChannelKeyPreview:    result.channelKeyPreview,
		ChannelKeyRemark:     result.channelKeyRemark,
		ResponseStatusCode:   result.responseStatusCode,
		OpenAIRequestCurl:    result.openAIRequestCurl,
		AnthropicRequestCurl: result.anthropicRequestCurl,
		ResponseContent:      trimProbePayload(responseContent),
		Error: func() string {
			if result.err == nil {
				return ""
			}
			return result.err.Error()
		}(),
	}
}

func probeGroupChannelCheckItemWithKey(
	ctx context.Context,
	channel *model.Channel,
	request *transformerModel.InternalLLMRequest,
	requestKind string,
	outAdapter transformerModel.Outbound,
	usedKey model.ChannelKey,
) (result groupChannelCheckProbeResult) {
	result.requestKind = requestKind
	result.baseURL = strings.TrimSpace(channel.GetBaseUrl())
	result.channelKeyID = usedKey.ID
	result.channelKeyIndex = findChannelKeyIndex(channel.Keys, usedKey.ID)
	result.channelKeyPreview = buildChannelKeyPreview(usedKey.ChannelKey)
	result.channelKeyRemark = usedKey.Remark

	outboundRequest, err := outAdapter.TransformRequest(ctx, request, result.baseURL, usedKey.ChannelKey)
	if err != nil {
		result.err = fmt.Errorf("failed to build outbound request: %w", err)
		return
	}
	applyGroupChannelCheckHeaders(outboundRequest, channel)

	result.requestContent = snapshotHTTPRequestBody(outboundRequest)
	result.openAIRequestCurl, result.anthropicRequestCurl = buildGroupChannelCheckCompatibleCurls(ctx, channel, request, usedKey)
	if outboundRequest.URL != nil {
		result.requestURL = outboundRequest.URL.String()
	}

	httpClient, err := helper.ChannelHttpClient(channel)
	if err != nil {
		result.err = fmt.Errorf("failed to get http client: %w", err)
		return
	}

	response, err := httpClient.Do(outboundRequest)
	if err != nil {
		result.err = fmt.Errorf("failed to send request: %w", err)
		return
	}
	defer response.Body.Close()

	result.responseStatusCode = response.StatusCode
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		result.err = fmt.Errorf("failed to read response body: %w", err)
		return
	}
	result.responseContent = trimProbePayload(string(responseBody))

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		result.responsePreview = trimProbePayload(string(responseBody))
		result.err = fmt.Errorf("upstream error: %d: %s", response.StatusCode, trimProbePayload(string(responseBody)))
		return
	}

	response.Body = io.NopCloser(bytes.NewReader(responseBody))
	response.ContentLength = int64(len(responseBody))
	internalResponse, err := transformGroupChannelCheckResponse(ctx, outAdapter, request, response)
	if err != nil {
		result.responsePreview = trimProbePayload(string(responseBody))
		result.err = fmt.Errorf("failed to parse upstream response: %w", err)
		return
	}

	result.responsePreview = summarizeGroupChannelCheckResponse(requestKind, internalResponse)
	if result.responsePreview == "" {
		result.responsePreview = trimProbePayload(string(responseBody))
	}
	return
}

func transformGroupChannelCheckResponse(
	ctx context.Context,
	outAdapter transformerModel.Outbound,
	request *transformerModel.InternalLLMRequest,
	response *http.Response,
) (*transformerModel.InternalLLMResponse, error) {
	if request != nil && request.Stream != nil && *request.Stream {
		return transformGroupChannelCheckStreamResponse(ctx, outAdapter, response)
	}
	return outAdapter.TransformResponse(ctx, response)
}

func transformGroupChannelCheckStreamResponse(
	ctx context.Context,
	outAdapter transformerModel.Outbound,
	response *http.Response,
) (*transformerModel.InternalLLMResponse, error) {
	if response == nil {
		return nil, fmt.Errorf("response is nil")
	}
	if ct := response.Header.Get("Content-Type"); ct != "" && !strings.Contains(strings.ToLower(ct), "text/event-stream") {
		body, _ := io.ReadAll(io.LimitReader(response.Body, groupChannelCheckPreviewLimit))
		return nil, fmt.Errorf("upstream returned non-SSE content-type %q for stream request: %s", ct, trimProbePayload(string(body)))
	}

	readCfg := &sse.ReadConfig{MaxEventSize: groupChannelCheckPreviewLimit}
	var lastResponse *transformerModel.InternalLLMResponse
	hasEvent := false
	for event, err := range sse.Read(response.Body, readCfg) {
		if err != nil {
			return nil, fmt.Errorf("failed to read stream event: %w", err)
		}
		internalResponse, err := outAdapter.TransformStream(ctx, []byte(event.Data))
		if err != nil {
			return nil, fmt.Errorf("failed to transform stream event: %w", err)
		}
		if internalResponse == nil {
			continue
		}
		hasEvent = true
		lastResponse = internalResponse
		if internalResponse.Object == "[DONE]" {
			break
		}
	}
	if !hasEvent {
		return nil, fmt.Errorf("stream response has no events")
	}
	if lastResponse == nil {
		return nil, fmt.Errorf("stream response is empty")
	}
	if lastResponse.Object == "[DONE]" {
		return &transformerModel.InternalLLMResponse{Object: "chat.completion.chunk"}, nil
	}
	return lastResponse, nil
}

func summarizeGroupChannelCheckKeyFailure(result groupChannelCheckProbeResult) string {
	labelParts := make([]string, 0, 3)
	if result.channelKeyIndex > 0 {
		labelParts = append(labelParts, fmt.Sprintf("key %d", result.channelKeyIndex))
	} else {
		labelParts = append(labelParts, "key")
	}
	if result.channelKeyPreview != "" {
		labelParts = append(labelParts, result.channelKeyPreview)
	}
	if strings.TrimSpace(result.channelKeyRemark) != "" {
		labelParts = append(labelParts, result.channelKeyRemark)
	}

	statusText := "-"
	if result.responseStatusCode > 0 {
		statusText = fmt.Sprintf("%d", result.responseStatusCode)
	}

	reason := strings.TrimSpace(result.responseContent)
	if reason == "" {
		reason = strings.TrimSpace(result.responsePreview)
	}
	if reason == "" && result.err != nil {
		reason = strings.TrimSpace(result.err.Error())
	}
	if reason == "" {
		reason = "unknown error"
	}

	return trimProbePayload(fmt.Sprintf("%s\nstatus %s\n%s", strings.Join(labelParts, " · "), statusText, reason))
}

func resolveGroupChannelCheckProtocol(protocol model.GroupChannelCheckProtocol) (outbound.OutboundType, error) {
	switch protocol {
	case model.GroupChannelCheckProtocolOpenAIChat:
		return outbound.OutboundTypeOpenAIChat, nil
	case model.GroupChannelCheckProtocolOpenAIResponse:
		return outbound.OutboundTypeOpenAIResponse, nil
	case model.GroupChannelCheckProtocolAnthropic:
		return outbound.OutboundTypeAnthropic, nil
	default:
		return 0, fmt.Errorf("invalid check protocol: %s", protocol)
	}
}

func buildGroupChannelCheckRequest(channelType outbound.OutboundType, modelName string) (*transformerModel.InternalLLMRequest, string, error) {
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		return nil, "", fmt.Errorf("model name is empty")
	}

	prompt := "hello"
	maxTokens := int64(16)
	stream := false
	switch channelType {
	case outbound.OutboundTypeOpenAIChat:
		return &transformerModel.InternalLLMRequest{
			Model:     modelName,
			Messages:  []transformerModel.Message{{Role: "user", Content: transformerModel.MessageContent{Content: &prompt}}},
			MaxTokens: &maxTokens,
			Stream:    &stream,
		}, string(model.GroupChannelCheckProtocolOpenAIChat), nil
	case outbound.OutboundTypeOpenAIResponse:
		rawRequest, err := json.Marshal(map[string]any{
			"model":      modelName,
			"input":      prompt,
			"max_tokens": maxTokens,
			"stream":     stream,
		})
		if err != nil {
			return nil, "", fmt.Errorf("failed to marshal responses check request: %w", err)
		}
		return &transformerModel.InternalLLMRequest{
			Model:        modelName,
			Messages:     []transformerModel.Message{{Role: "user", Content: transformerModel.MessageContent{Content: &prompt}}},
			MaxTokens:    &maxTokens,
			Stream:       &stream,
			RawAPIFormat: transformerModel.APIFormatOpenAIResponse,
			RawRequest:   rawRequest,
		}, string(model.GroupChannelCheckProtocolOpenAIResponse), nil
	case outbound.OutboundTypeAnthropic:
		return &transformerModel.InternalLLMRequest{
			Model:     modelName,
			Messages:  []transformerModel.Message{{Role: "user", Content: transformerModel.MessageContent{Content: &prompt}}},
			MaxTokens: &maxTokens,
			Stream:    &stream,
		}, string(model.GroupChannelCheckProtocolAnthropic), nil
	}
	return nil, "", fmt.Errorf("unsupported check protocol type: %d", channelType)
}

func applyGroupChannelCheckHeaders(req *http.Request, channel *model.Channel) {
	if req == nil || channel == nil {
		return
	}
	for _, header := range channel.CustomHeader {
		req.Header.Set(header.HeaderKey, header.HeaderValue)
	}
}

func snapshotHTTPRequestBody(req *http.Request) string {
	if req == nil || req.Body == nil {
		return ""
	}

	if req.GetBody != nil {
		body, err := req.GetBody()
		if err == nil {
			defer body.Close()
			data, readErr := io.ReadAll(body)
			if readErr == nil {
				return trimProbePayload(string(data))
			}
		}
	}

	data, err := io.ReadAll(req.Body)
	if err != nil {
		return ""
	}
	req.Body = io.NopCloser(bytes.NewReader(data))
	return trimProbePayload(string(data))
}

func buildGroupChannelCheckCurl(req *http.Request, requestContent string) string {
	if req == nil || req.URL == nil {
		return ""
	}

	parts := []string{"curl"}
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}
	if method != http.MethodGet {
		parts = append(parts, "-X", shellQuoteForCurl(method))
	}

	headerKeys := make([]string, 0, len(req.Header))
	for headerKey := range req.Header {
		headerKeys = append(headerKeys, headerKey)
	}
	sort.Strings(headerKeys)
	for _, headerKey := range headerKeys {
		values := req.Header[headerKey]
		key := strings.TrimSpace(headerKey)
		if key == "" {
			continue
		}
		for _, value := range values {
			parts = append(parts, "-H", shellQuoteForCurl(key+": "+strings.TrimSpace(value)))
		}
	}

	if strings.TrimSpace(requestContent) != "" && method != http.MethodGet {
		parts = append(parts, "--data-raw", shellQuoteForCurl(requestContent))
	}

	parts = append(parts, shellQuoteForCurl(req.URL.String()))
	return strings.Join(parts, " ")
}

func buildGroupChannelCheckCompatibleCurls(
	ctx context.Context,
	channel *model.Channel,
	request *transformerModel.InternalLLMRequest,
	usedKey model.ChannelKey,
) (string, string) {
	if channel == nil || request == nil {
		return "", ""
	}

	openAICurl := ""
	anthropicCurl := ""

	if openAIReq := buildGroupChannelCheckCompatibleRequest(
		ctx,
		channel,
		request,
		usedKey.ChannelKey,
		func() transformerModel.Outbound {
			if request.IsEmbeddingRequest() {
				return &openaiOutbound.EmbeddingOutbound{}
			}
			return &openaiOutbound.ChatOutbound{}
		},
	); openAIReq != nil {
		openAIContent := snapshotHTTPRequestBody(openAIReq)
		openAICurl = buildGroupChannelCheckCurl(openAIReq, openAIContent)
	}

	if !request.IsEmbeddingRequest() {
		if anthropicReq := buildGroupChannelCheckCompatibleRequest(
			ctx,
			channel,
			request,
			usedKey.ChannelKey,
			func() transformerModel.Outbound { return &authropicOutbound.MessageOutbound{} },
		); anthropicReq != nil {
			anthropicContent := snapshotHTTPRequestBody(anthropicReq)
			anthropicCurl = buildGroupChannelCheckCurl(anthropicReq, anthropicContent)
		}
	}

	return openAICurl, anthropicCurl
}

func buildGroupChannelCheckCompatibleRequest(
	ctx context.Context,
	channel *model.Channel,
	request *transformerModel.InternalLLMRequest,
	key string,
	outboundFactory func() transformerModel.Outbound,
) *http.Request {
	if channel == nil || request == nil || outboundFactory == nil {
		return nil
	}

	requestCopy, err := cloneGroupChannelCheckInternalRequest(request)
	if err != nil {
		return nil
	}
	compatibleOutbound := outboundFactory()
	if compatibleOutbound == nil {
		return nil
	}

	outboundRequest, err := compatibleOutbound.TransformRequest(ctx, requestCopy, strings.TrimSpace(channel.GetBaseUrl()), key)
	if err != nil {
		return nil
	}
	applyGroupChannelCheckHeaders(outboundRequest, channel)
	return outboundRequest
}

func cloneGroupChannelCheckInternalRequest(request *transformerModel.InternalLLMRequest) (*transformerModel.InternalLLMRequest, error) {
	if request == nil {
		return nil, fmt.Errorf("request is nil")
	}

	data, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}

	var cloned transformerModel.InternalLLMRequest
	if err := json.Unmarshal(data, &cloned); err != nil {
		return nil, err
	}
	return &cloned, nil
}

func shellQuoteForCurl(input string) string {
	if input == "" {
		return "''"
	}
	if needsDoubleQuotedCurlArg(input) {
		return strconv.Quote(input)
	}
	return "'" + strings.ReplaceAll(input, "'", `'\"'\"'`) + "'"
}

func needsDoubleQuotedCurlArg(input string) bool {
	for _, r := range input {
		if r == '\n' || r == '\r' || r == '\t' {
			return true
		}
		if r < 0x20 {
			return true
		}
	}
	return false
}

func summarizeGroupChannelCheckResponse(requestKind string, response *transformerModel.InternalLLMResponse) string {
	if response == nil {
		return ""
	}

	switch requestKind {
	case "embedding":
		dimension := 0
		if len(response.EmbeddingData) > 0 {
			if len(response.EmbeddingData[0].Embedding.FloatArray) > 0 {
				dimension = len(response.EmbeddingData[0].Embedding.FloatArray)
			} else if response.EmbeddingData[0].Embedding.Base64String != nil {
				dimension = len(*response.EmbeddingData[0].Embedding.Base64String)
			}
		}
		if response.Usage != nil {
			return fmt.Sprintf("embedding_count=%d, dimension=%d, total_tokens=%d", len(response.EmbeddingData), dimension, response.Usage.TotalTokens)
		}
		return fmt.Sprintf("embedding_count=%d, dimension=%d", len(response.EmbeddingData), dimension)
	default:
		text := extractResponseText(response)
		if text == "" {
			encoded, err := json.Marshal(response)
			if err == nil {
				return trimProbePayload(string(encoded))
			}
			return ""
		}
		if response.Usage != nil {
			return trimProbePayload(fmt.Sprintf("%s\n\nprompt_tokens=%d, completion_tokens=%d, total_tokens=%d",
				text, response.Usage.PromptTokens, response.Usage.CompletionTokens, response.Usage.TotalTokens))
		}
		return trimProbePayload(text)
	}
}

func extractResponseText(response *transformerModel.InternalLLMResponse) string {
	if response == nil {
		return ""
	}
	for _, choice := range response.Choices {
		if choice.Message == nil {
			continue
		}
		if choice.Message.Content.Content != nil && strings.TrimSpace(*choice.Message.Content.Content) != "" {
			return strings.TrimSpace(*choice.Message.Content.Content)
		}
		if len(choice.Message.Content.MultipleContent) > 0 {
			parts := make([]string, 0, len(choice.Message.Content.MultipleContent))
			for _, part := range choice.Message.Content.MultipleContent {
				if part.Type == "text" && part.Text != nil && strings.TrimSpace(*part.Text) != "" {
					parts = append(parts, strings.TrimSpace(*part.Text))
				}
			}
			if len(parts) > 0 {
				return strings.Join(parts, "\n")
			}
		}
	}
	return ""
}

func trimProbePayload(input string) string {
	input = strings.TrimSpace(input)
	if len(input) <= groupChannelCheckPreviewLimit {
		return input
	}
	return input[:groupChannelCheckPreviewLimit] + "\n...[truncated]"
}

func buildChannelKeyPreview(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	if len(key) <= 8 {
		return key
	}
	return key[:4] + "..." + key[len(key)-4:]
}

func findChannelKeyIndex(keys []model.ChannelKey, keyID int) int {
	for idx, key := range keys {
		if key.ID == keyID {
			return idx + 1
		}
	}
	return 0
}

func BuildGroupChannelCheckTask(group model.Group, items []model.GroupItem, mode model.GroupChannelCheckTaskMode, channelNameByID map[int]string, channelTypeByID map[int]int) *model.GroupChannelCheckTask {
	now := time.Now().Unix()
	sortedItems := make([]model.GroupItem, len(items))
	copy(sortedItems, items)
	sort.Slice(sortedItems, func(i, j int) bool {
		return sortedItems[i].Priority < sortedItems[j].Priority
	})
	task := &model.GroupChannelCheckTask{
		ID:           snowflake.GenerateID(),
		GroupID:      group.ID,
		GroupName:    group.Name,
		Mode:         mode,
		Status:       model.GroupChannelCheckTaskStatusPending,
		TotalCount:   len(sortedItems),
		PendingCount: len(sortedItems),
		CreatedAt:    now,
		Items:        make([]model.GroupChannelCheckTaskItem, 0, len(sortedItems)),
	}

	for _, item := range sortedItems {
		task.Items = append(task.Items, model.GroupChannelCheckTaskItem{
			ID:          snowflake.GenerateID(),
			TaskID:      task.ID,
			GroupID:     group.ID,
			GroupItemID: item.ID,
			ChannelID:   item.ChannelID,
			ChannelName: channelNameByID[item.ChannelID],
			ChannelType: channelTypeByID[item.ChannelID],
			ModelName:   item.ModelName,
			Status:      model.GroupChannelCheckItemStatusQueued,
		})
	}
	return task
}

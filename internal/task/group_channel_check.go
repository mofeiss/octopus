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
	"strings"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/helper"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/snowflake"
)

const (
	groupChannelCheckQueueSize      = 128
	groupChannelCheckWorkerCount    = 2
	groupChannelCheckRequestTimeout = 45 * time.Second
	groupChannelCheckPreviewLimit   = 16 * 1024
)

var (
	groupChannelCheckQueue     chan int64
	groupChannelCheckQueueOnce sync.Once
	groupChannelCheckExecuting sync.Map
)

type groupChannelCheckProbeResult struct {
	requestKind        string
	requestURL         string
	baseURL            string
	channelKeyID       int
	channelKeyIndex    int
	channelKeyPreview  string
	channelKeyRemark   string
	responseStatusCode int
	durationMs         int
	requestContent     string
	responsePreview    string
	responseContent    string
	attempts           []model.GroupChannelCheckAttempt
	err                error
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

	lastErr := ""
	idleRetry := 0
	for {
		item, nextErr := op.GroupChannelCheckTaskNextPendingItem(taskID, ctx)
		if nextErr != nil {
			log.Warnf("failed to load next group channel check item (task=%d): %v", taskID, nextErr)
			lastErr = nextErr.Error()
			break
		}
		if item == nil {
			taskAfter, refreshErr := op.GroupChannelCheckTaskRefreshSummary(taskID, ctx)
			if refreshErr != nil {
				log.Warnf("failed to finalize group channel check summary (task=%d): %v", taskID, refreshErr)
				lastErr = refreshErr.Error()
				break
			}
			if taskAfter.Status == model.GroupChannelCheckTaskStatusPending || taskAfter.Status == model.GroupChannelCheckTaskStatusRunning {
				if idleRetry >= 5 {
					lastErr = "task finished unexpectedly"
					break
				}
				idleRetry++
				time.Sleep(200 * time.Millisecond)
				continue
			}
			if err := op.GroupChannelCheckTaskUpdate(taskID, map[string]any{
				"last_error":  lastErr,
				"finished_at": taskAfter.FinishedAt,
				"status":      taskAfter.Status,
			}, ctx); err != nil {
				log.Warnf("failed to finalize group channel check task (task=%d): %v", taskID, err)
			}
			return
		}

		idleRetry = 0
		itemStart := time.Now().Unix()
		if err := op.GroupChannelCheckTaskItemUpdate(item.ID, map[string]any{
			"status":               model.GroupChannelCheckItemStatusRunning,
			"started_at":           itemStart,
			"finished_at":          int64(0),
			"error":                "",
			"response_status_code": 0,
		}, ctx); err != nil {
			log.Warnf("failed to mark group channel check item running (item=%d): %v", item.ID, err)
			lastErr = err.Error()
			continue
		}
		if _, err := op.GroupChannelCheckTaskRefreshSummary(taskID, ctx); err != nil {
			log.Warnf("failed to refresh running group channel check summary (task=%d): %v", taskID, err)
		}

		result := probeGroupChannelCheckItem(item.ChannelID, item.ModelName)
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
		finishedItem.ResponsePreview = result.responsePreview
		finishedItem.ResponseContent = result.responseContent
		finishedItem.FinishedAt = finishedAt
		finishedItem.Attempts = result.attempts
		if result.err != nil {
			finishedItem.Status = model.GroupChannelCheckItemStatusFailed
			finishedItem.Error = result.err.Error()
			lastErr = result.err.Error()
		} else {
			finishedItem.Status = model.GroupChannelCheckItemStatusSuccess
			finishedItem.Error = ""
		}

		if err := op.GroupChannelCheckTaskItemSaveResult(finishedItem, ctx); err != nil {
			log.Warnf("failed to update group channel check item result (item=%d): %v", item.ID, err)
			lastErr = err.Error()
			continue
		}

		group, groupErr := op.GroupGet(task.GroupID, ctx)
		if groupErr == nil {
			if err := op.GroupChannelCheckStateUpsertByItem(*group, finishedItem, ctx); err != nil {
				log.Warnf("failed to sync group channel check state (item=%d): %v", item.ID, err)
			}
		}
		if _, err := op.GroupChannelCheckTaskRefreshSummary(taskID, ctx); err != nil {
			log.Warnf("failed to refresh finished group channel check summary (task=%d): %v", taskID, err)
		}
	}

	taskAfter, err := op.GroupChannelCheckTaskRefreshSummary(taskID, ctx)
	if err != nil {
		log.Warnf("failed to finalize group channel check summary (task=%d): %v", taskID, err)
		return
	}

	finalUpdates := map[string]any{
		"last_error":  lastErr,
		"finished_at": taskAfter.FinishedAt,
		"status":      taskAfter.Status,
	}
	if taskAfter.Status == model.GroupChannelCheckTaskStatusRunning || taskAfter.Status == model.GroupChannelCheckTaskStatusPending {
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

func probeGroupChannelCheckItem(channelID int, modelName string) (result groupChannelCheckProbeResult) {
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

	request, requestKind, err := buildGroupChannelCheckRequest(channel.Type, modelName)
	if err != nil {
		result.err = err
		return
	}
	result.requestKind = requestKind
	if err := request.Validate(); err != nil {
		result.err = fmt.Errorf("invalid check request: %w", err)
		return
	}

	outAdapter := outbound.Get(channel.Type)
	if outAdapter == nil {
		result.err = fmt.Errorf("unsupported channel type: %d", channel.Type)
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
		Status:             status,
		ChannelKeyID:       result.channelKeyID,
		ChannelKeyIndex:    result.channelKeyIndex,
		ChannelKeyPreview:  result.channelKeyPreview,
		ChannelKeyRemark:   result.channelKeyRemark,
		ResponseStatusCode: result.responseStatusCode,
		ResponseContent:    trimProbePayload(responseContent),
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
	internalResponse, err := outAdapter.TransformResponse(ctx, response)
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

func buildGroupChannelCheckRequest(channelType outbound.OutboundType, modelName string) (*transformerModel.InternalLLMRequest, string, error) {
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		return nil, "", fmt.Errorf("model name is empty")
	}

	switch channelType {
	case outbound.OutboundTypeOpenAIEmbedding:
		input := "health check"
		return &transformerModel.InternalLLMRequest{
			Model:          modelName,
			EmbeddingInput: &transformerModel.EmbeddingInput{Single: &input},
		}, "embedding", nil
	default:
		content := "Reply with OK only."
		maxTokens := int64(8)
		temperature := 0.0
		return &transformerModel.InternalLLMRequest{
			Model: modelName,
			Messages: []transformerModel.Message{
				{
					Role: "user",
					Content: transformerModel.MessageContent{
						Content: &content,
					},
				},
			},
			MaxTokens:   &maxTokens,
			Temperature: &temperature,
		}, "chat", nil
	}
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
			Status:      model.GroupChannelCheckItemStatusPending,
		})
	}
	return task
}

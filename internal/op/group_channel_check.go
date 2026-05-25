package op

import (
	"context"
	"fmt"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

func GroupChannelCheckTaskCreate(task *model.GroupChannelCheckTask, ctx context.Context) error {
	if task == nil {
		return fmt.Errorf("task is nil")
	}

	return db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Omit("Items").Create(task).Error; err != nil {
			return err
		}
		if len(task.Items) == 0 {
			return nil
		}
		return tx.Create(&task.Items).Error
	})
}

func GroupChannelCheckTaskGet(taskID int64, ctx context.Context) (*model.GroupChannelCheckTask, error) {
	var task model.GroupChannelCheckTask
	if err := db.GetDB().WithContext(ctx).
		Preload("Items", func(tx *gorm.DB) *gorm.DB {
			return tx.Order("id ASC")
		}).
		First(&task, "id = ?", taskID).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

func GroupChannelCheckTaskListLatest(ctx context.Context) ([]model.GroupChannelCheckTask, error) {
	var tasks []model.GroupChannelCheckTask
	if err := db.GetDB().WithContext(ctx).
		Order("created_at DESC, id DESC").
		Find(&tasks).Error; err != nil {
		return nil, err
	}

	result := make([]model.GroupChannelCheckTask, 0)
	seen := make(map[int]struct{})
	for _, task := range tasks {
		if _, ok := seen[task.GroupID]; ok {
			continue
		}
		seen[task.GroupID] = struct{}{}
		result = append(result, task)
	}
	return result, nil
}

func GroupChannelCheckTaskListRunnableIDs(ctx context.Context) ([]int64, error) {
	ids := make([]int64, 0)
	if err := db.GetDB().WithContext(ctx).
		Model(&model.GroupChannelCheckTask{}).
		Where("status IN ?", []model.GroupChannelCheckTaskStatus{
			model.GroupChannelCheckTaskStatusPending,
			model.GroupChannelCheckTaskStatusRunning,
		}).
		Where("EXISTS (SELECT 1 FROM group_channel_check_task_items WHERE group_channel_check_task_items.task_id = group_channel_check_tasks.id AND group_channel_check_task_items.status = ?)", model.GroupChannelCheckItemStatusPending).
		Order("created_at ASC, id ASC").
		Pluck("id", &ids).Error; err != nil {
		return nil, err
	}
	return ids, nil
}

func GroupChannelCheckTaskUpdate(taskID int64, updates map[string]any, ctx context.Context) error {
	if len(updates) == 0 {
		return nil
	}
	return db.GetDB().WithContext(ctx).
		Model(&model.GroupChannelCheckTask{}).
		Where("id = ?", taskID).
		Updates(updates).Error
}

func GroupChannelCheckTaskItemUpdate(itemID int64, updates map[string]any, ctx context.Context) error {
	if len(updates) == 0 {
		return nil
	}
	return db.GetDB().WithContext(ctx).
		Model(&model.GroupChannelCheckTaskItem{}).
		Where("id = ?", itemID).
		Updates(updates).Error
}

func GroupChannelCheckTaskItemSaveResult(item model.GroupChannelCheckTaskItem, ctx context.Context) error {
	return db.GetDB().WithContext(ctx).
		Model(&model.GroupChannelCheckTaskItem{}).
		Where("id = ?", item.ID).
		Select(
			"status",
			"request_kind",
			"request_url",
			"base_url",
			"channel_key_id",
			"channel_key_index",
			"channel_key_preview",
			"channel_key_remark",
			"response_status_code",
			"duration_ms",
			"request_content",
			"openai_request_curl",
			"anthropic_request_curl",
			"response_preview",
			"response_content",
			"error",
			"finished_at",
			"attempts",
		).
		Updates(&item).Error
}

func GroupChannelCheckTaskRefreshSummary(taskID int64, ctx context.Context) (*model.GroupChannelCheckTask, error) {
	var task model.GroupChannelCheckTask
	if err := db.GetDB().WithContext(ctx).First(&task, "id = ?", taskID).Error; err != nil {
		return nil, err
	}

	var items []model.GroupChannelCheckTaskItem
	if err := db.GetDB().WithContext(ctx).
		Where("task_id = ?", taskID).
		Order("id ASC").
		Find(&items).Error; err != nil {
		return nil, err
	}

	totalCount := len(items)
	pendingCount := 0
	queuedCount := 0 // [fork] 已加入窗口但尚未点击发送的测活项
	runningCount := 0
	successCount := 0
	failedCount := 0
	for _, item := range items {
		switch item.Status {
		case model.GroupChannelCheckItemStatusQueued:
			queuedCount++
		case model.GroupChannelCheckItemStatusPending:
			pendingCount++
		case model.GroupChannelCheckItemStatusRunning:
			runningCount++
		case model.GroupChannelCheckItemStatusSuccess:
			successCount++
		case model.GroupChannelCheckItemStatusFailed:
			failedCount++
		}
	}

	status := deriveGroupChannelCheckTaskStatus(totalCount, queuedCount, pendingCount, runningCount, successCount, failedCount)
	updates := map[string]any{
		"total_count":   totalCount,
		"pending_count": pendingCount,
		"running_count": runningCount,
		"success_count": successCount,
		"failed_count":  failedCount,
		"status":        status,
	}
	if status == model.GroupChannelCheckTaskStatusPending {
		updates["started_at"] = int64(0)
		updates["finished_at"] = int64(0)
	} else if status == model.GroupChannelCheckTaskStatusRunning {
		updates["finished_at"] = int64(0)
	} else if task.FinishedAt == 0 {
		updates["finished_at"] = time.Now().Unix()
	}

	if err := GroupChannelCheckTaskUpdate(taskID, updates, ctx); err != nil {
		return nil, err
	}

	task.TotalCount = totalCount
	task.PendingCount = pendingCount
	task.RunningCount = runningCount
	task.SuccessCount = successCount
	task.FailedCount = failedCount
	task.Status = status
	if finishedAt, ok := updates["finished_at"].(int64); ok {
		task.FinishedAt = finishedAt
	}
	return &task, nil
}

func deriveGroupChannelCheckTaskStatus(totalCount, queuedCount, pendingCount, runningCount, successCount, failedCount int) model.GroupChannelCheckTaskStatus {
	switch {
	case totalCount == 0:
		return model.GroupChannelCheckTaskStatusFailed
	case runningCount > 0:
		return model.GroupChannelCheckTaskStatusRunning
	case queuedCount == totalCount:
		return model.GroupChannelCheckTaskStatusPending
	case pendingCount == totalCount:
		return model.GroupChannelCheckTaskStatusPending
	case pendingCount > 0:
		return model.GroupChannelCheckTaskStatusRunning
	case queuedCount > 0 && successCount+failedCount+queuedCount == totalCount:
		return model.GroupChannelCheckTaskStatusPending
	case successCount == totalCount:
		return model.GroupChannelCheckTaskStatusSuccess
	case successCount > 0 && failedCount > 0:
		return model.GroupChannelCheckTaskStatusPartialSuccess
	default:
		return model.GroupChannelCheckTaskStatusFailed
	}
}

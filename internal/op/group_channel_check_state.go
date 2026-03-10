package op

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/utils/snowflake"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func attachGroupChannelCheckStates(groups []model.Group, ctx context.Context) error {
	if len(groups) == 0 {
		return nil
	}

	groupIDs := make([]int, 0, len(groups))
	groupItemIDs := make([]int, 0)
	for _, group := range groups {
		groupIDs = append(groupIDs, group.ID)
		for _, item := range group.Items {
			if item.ID > 0 {
				groupItemIDs = append(groupItemIDs, item.ID)
			}
		}
	}
	if len(groupItemIDs) == 0 {
		return nil
	}

	var states []model.GroupChannelCheckState
	if err := db.GetDB().WithContext(ctx).
		Where("group_id IN ? AND group_item_id IN ?", groupIDs, groupItemIDs).
		Find(&states).Error; err != nil {
		return err
	}

	stateByGroupItemID := make(map[int]model.GroupChannelCheckState, len(states))
	for _, state := range states {
		stateByGroupItemID[state.GroupItemID] = state
	}

	for groupIdx := range groups {
		for itemIdx := range groups[groupIdx].Items {
			item := &groups[groupIdx].Items[itemIdx]
			state, ok := stateByGroupItemID[item.ID]
			if !ok {
				continue
			}
			item.HealthCheckTaskID = state.TaskID
			item.HealthCheckStatus = state.Status
			item.HealthCheckCheckedAt = state.CheckedAt
			item.HealthCheckConsecutiveFailures = state.ConsecutiveFailures
			item.HealthCheckResponseStatusCode = state.ResponseStatusCode
			item.HealthCheckDurationMs = state.DurationMs
			item.HealthCheckError = state.Error
		}
	}

	return nil
}

func GroupChannelCheckTaskFindActiveByGroup(groupID int, ctx context.Context) (*model.GroupChannelCheckTask, error) {
	var task model.GroupChannelCheckTask
	if err := db.GetDB().WithContext(ctx).
		Where("group_id = ? AND status IN ?", groupID, []model.GroupChannelCheckTaskStatus{
			model.GroupChannelCheckTaskStatusPending,
			model.GroupChannelCheckTaskStatusRunning,
		}).
		Order("created_at DESC, id DESC").
		First(&task).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

func GroupChannelCheckTaskNextPendingItem(taskID int64, ctx context.Context) (*model.GroupChannelCheckTaskItem, error) {
	var item model.GroupChannelCheckTaskItem
	if err := db.GetDB().WithContext(ctx).
		Where("task_id = ? AND status = ?", taskID, model.GroupChannelCheckItemStatusPending).
		Order("id ASC").
		First(&item).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &item, nil
}

func GroupChannelCheckTaskListPendingItems(taskID int64, ctx context.Context) ([]model.GroupChannelCheckTaskItem, error) {
	items := make([]model.GroupChannelCheckTaskItem, 0)
	if err := db.GetDB().WithContext(ctx).
		Where("task_id = ? AND status = ?", taskID, model.GroupChannelCheckItemStatusPending).
		Order("id ASC").
		Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func GroupChannelCheckTaskAppendItems(
	taskID int64,
	group model.Group,
	items []model.GroupItem,
	mode model.GroupChannelCheckTaskMode,
	channelNameByID map[int]string,
	channelTypeByID map[int]int,
	ctx context.Context,
) (*model.GroupChannelCheckTask, error) {
	if len(items) == 0 {
		return GroupChannelCheckTaskGet(taskID, ctx)
	}

	if err := db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task model.GroupChannelCheckTask
		if err := tx.First(&task, "id = ?", taskID).Error; err != nil {
			return err
		}
		if task.Status != model.GroupChannelCheckTaskStatusPending && task.Status != model.GroupChannelCheckTaskStatusRunning {
			return fmt.Errorf("task is not active")
		}

		var existing []model.GroupChannelCheckTaskItem
		if err := tx.Where("task_id = ?", taskID).Find(&existing).Error; err != nil {
			return err
		}

		existingGroupItemIDs := make(map[int]struct{}, len(existing))
		existingKeys := make(map[string]struct{}, len(existing))
		for _, item := range existing {
			if item.GroupItemID > 0 {
				existingGroupItemIDs[item.GroupItemID] = struct{}{}
			}
			existingKeys[fmt.Sprintf("%d|%s", item.ChannelID, item.ModelName)] = struct{}{}
		}

		newItems := make([]model.GroupChannelCheckTaskItem, 0, len(items))
		for _, item := range items {
			if item.ID > 0 {
				if _, ok := existingGroupItemIDs[item.ID]; ok {
					continue
				}
			}
			key := fmt.Sprintf("%d|%s", item.ChannelID, item.ModelName)
			if _, ok := existingKeys[key]; ok {
				continue
			}

			newItems = append(newItems, model.GroupChannelCheckTaskItem{
				ID:          snowflake.GenerateID(),
				TaskID:      taskID,
				GroupID:     group.ID,
				GroupItemID: item.ID,
				ChannelID:   item.ChannelID,
				ChannelName: channelNameByID[item.ChannelID],
				ChannelType: channelTypeByID[item.ChannelID],
				ModelName:   item.ModelName,
				Status:      model.GroupChannelCheckItemStatusPending,
			})
		}

		if len(newItems) > 0 {
			if err := tx.Create(&newItems).Error; err != nil {
				return err
			}
		}

		if mode == model.GroupChannelCheckTaskModeBatch && task.Mode != model.GroupChannelCheckTaskModeBatch {
			if err := tx.Model(&model.GroupChannelCheckTask{}).
				Where("id = ?", taskID).
				Update("mode", model.GroupChannelCheckTaskModeBatch).Error; err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		return nil, err
	}

	if _, err := GroupChannelCheckTaskRefreshSummary(taskID, ctx); err != nil {
		return nil, err
	}
	return GroupChannelCheckTaskGet(taskID, ctx)
}

func GroupChannelCheckStateUpsertByItem(group model.Group, item model.GroupChannelCheckTaskItem, ctx context.Context) error {
	if item.GroupItemID <= 0 {
		return nil
	}
	if item.Status != model.GroupChannelCheckItemStatusSuccess && item.Status != model.GroupChannelCheckItemStatusFailed {
		return nil
	}

	checkedAt := item.FinishedAt
	if checkedAt == 0 {
		checkedAt = time.Now().Unix()
	}

	var existing model.GroupChannelCheckState
	err := db.GetDB().WithContext(ctx).Where("group_item_id = ?", item.GroupItemID).First(&existing).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	state := model.GroupChannelCheckState{
		GroupID:            group.ID,
		GroupItemID:        item.GroupItemID,
		ChannelID:          item.ChannelID,
		ModelName:          item.ModelName,
		TaskID:             item.TaskID,
		Status:             item.Status,
		CheckedAt:          checkedAt,
		ResponseStatusCode: item.ResponseStatusCode,
		DurationMs:         item.DurationMs,
		Error:              item.Error,
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		state.ID = snowflake.GenerateID()
	} else {
		state.ID = existing.ID
		state.ConsecutiveFailures = existing.ConsecutiveFailures
		state.SuccessAt = existing.SuccessAt
		state.FailedAt = existing.FailedAt
	}

	if item.Status == model.GroupChannelCheckItemStatusSuccess {
		state.ConsecutiveFailures = 0
		state.SuccessAt = checkedAt
	} else {
		state.ConsecutiveFailures++
		state.FailedAt = checkedAt
	}

	if err := db.GetDB().WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "group_item_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"group_id", "channel_id", "model_name", "task_id", "status", "checked_at", "success_at", "failed_at", "consecutive_failures", "response_status_code", "duration_ms", "error"}),
	}).Create(&state).Error; err != nil {
		return err
	}

	desiredEnabled := determineGroupItemEnabledByHealthCheck(group, state)
	if desiredEnabled != nil {
		if err := db.GetDB().WithContext(ctx).
			Model(&model.GroupItem{}).
			Where("id = ?", item.GroupItemID).
			Update("enabled", *desiredEnabled).Error; err != nil {
			return err
		}
	}

	return groupRefreshCacheByID(group.ID, ctx)
}

func determineGroupItemEnabledByHealthCheck(group model.Group, state model.GroupChannelCheckState) *bool {
	if !group.AutoHealthCheckEnabled {
		return nil
	}

	if state.Status == model.GroupChannelCheckItemStatusSuccess {
		enabled := true
		return &enabled
	}

	if state.Status == model.GroupChannelCheckItemStatusFailed &&
		group.AutoHealthCheckFailThreshold > 0 &&
		state.ConsecutiveFailures >= group.AutoHealthCheckFailThreshold {
		enabled := false
		return &enabled
	}

	return nil
}

func GroupChannelCheckTaskSyncGroupItems(taskID int64, ctx context.Context) (*model.GroupChannelCheckSyncResult, error) {
	task, err := GroupChannelCheckTaskGet(taskID, ctx)
	if err != nil {
		return nil, err
	}

	result := &model.GroupChannelCheckSyncResult{
		GroupID:   task.GroupID,
		GroupName: task.GroupName,
	}

	if err := db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, item := range task.Items {
			if item.GroupItemID <= 0 {
				result.SkippedCount++
				continue
			}

			switch item.Status {
			case model.GroupChannelCheckItemStatusSuccess:
				if err := tx.Model(&model.GroupItem{}).Where("id = ?", item.GroupItemID).Update("enabled", true).Error; err != nil {
					return err
				}
				result.EnabledCount++
			case model.GroupChannelCheckItemStatusFailed:
				if err := tx.Model(&model.GroupItem{}).Where("id = ?", item.GroupItemID).Update("enabled", false).Error; err != nil {
					return err
				}
				result.DisabledCount++
			default:
				result.SkippedCount++
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}

	if err := groupRefreshCacheByID(task.GroupID, ctx); err != nil {
		return nil, err
	}

	return result, nil
}

func GroupListAutoHealthCheckDue(now int64, ctx context.Context) ([]model.Group, error) {
	var groups []model.Group
	if err := db.GetDB().WithContext(ctx).
		Preload("Items").
		Where("auto_health_check_enabled = ? AND auto_health_check_interval_minutes > 0 AND auto_health_check_fail_threshold > 0 AND auto_health_check_next_run_at > 0 AND auto_health_check_next_run_at <= ?", true, now).
		Order("auto_health_check_next_run_at ASC, id ASC").
		Find(&groups).Error; err != nil {
		return nil, err
	}
	return groups, nil
}

func GroupSetAutoHealthCheckNextRun(groupID int, nextRunAt int64, ctx context.Context) error {
	if err := db.GetDB().WithContext(ctx).
		Model(&model.Group{}).
		Where("id = ?", groupID).
		Update("auto_health_check_next_run_at", nextRunAt).Error; err != nil {
		return err
	}
	return groupRefreshCacheByID(groupID, ctx)
}

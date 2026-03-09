package task

import (
	"context"
	"errors"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"gorm.io/gorm"
)

const TaskGroupAutoHealthCheck = "group_auto_health_check"

func CreateOrAppendGroupChannelCheckTask(
	group model.Group,
	items []model.GroupItem,
	mode model.GroupChannelCheckTaskMode,
	appendToActive bool,
	ctx context.Context,
) (*model.GroupChannelCheckTask, error) {
	if len(items) == 0 {
		return nil, gorm.ErrRecordNotFound
	}

	channelNameByID, channelTypeByID := resolveGroupChannelCheckChannelMeta(items, ctx)

	if appendToActive {
		activeTask, err := op.GroupChannelCheckTaskFindActiveByGroup(group.ID, ctx)
		if err == nil {
			return op.GroupChannelCheckTaskAppendItems(activeTask.ID, group, items, mode, channelNameByID, channelTypeByID, ctx)
		}
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}

	task := BuildGroupChannelCheckTask(group, items, mode, channelNameByID, channelTypeByID)
	if err := op.GroupChannelCheckTaskCreate(task, ctx); err != nil {
		return nil, err
	}
	if err := EnqueueGroupChannelCheckTask(task.ID); err != nil {
		_ = op.GroupChannelCheckTaskUpdate(task.ID, map[string]any{
			"status":        model.GroupChannelCheckTaskStatusFailed,
			"pending_count": 0,
			"failed_count":  len(task.Items),
			"finished_at":   time.Now().Unix(),
			"last_error":    err.Error(),
		}, ctx)
		return nil, err
	}
	return task, nil
}

func RunAutoGroupChannelChecks() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	groups, err := op.GroupListAutoHealthCheckDue(time.Now().Unix(), ctx)
	if err != nil {
		return
	}

	for _, group := range groups {
		nextRunAt := time.Now().Add(time.Duration(group.AutoHealthCheckIntervalMinutes) * time.Minute).Unix()
		if err := op.GroupSetAutoHealthCheckNextRun(group.ID, nextRunAt, ctx); err != nil {
			continue
		}
		if len(group.Items) == 0 {
			continue
		}
		_, _ = CreateOrAppendGroupChannelCheckTask(group, group.Items, model.GroupChannelCheckTaskModeBatch, true, ctx)
	}
}

func resolveGroupChannelCheckChannelMeta(items []model.GroupItem, ctx context.Context) (map[int]string, map[int]int) {
	channelNameByID := make(map[int]string, len(items))
	channelTypeByID := make(map[int]int, len(items))
	for _, item := range items {
		if _, ok := channelNameByID[item.ChannelID]; ok {
			continue
		}
		channel, err := op.ChannelGet(item.ChannelID, ctx)
		if err != nil {
			channelNameByID[item.ChannelID] = "Unknown Channel"
			channelTypeByID[item.ChannelID] = -1
			continue
		}
		channelNameByID[item.ChannelID] = channel.Name
		channelTypeByID[item.ChannelID] = int(channel.Type)
	}
	return channelNameByID, channelTypeByID
}

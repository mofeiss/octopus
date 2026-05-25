package task

import (
	"context"
	"errors"
	"fmt"
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
	enqueue bool,
	protocol model.GroupChannelCheckProtocol,
	ctx context.Context,
) (*model.GroupChannelCheckTask, error) {
	if len(items) == 0 {
		return nil, gorm.ErrRecordNotFound
	}

	channelNameByID, channelTypeByID := resolveGroupChannelCheckChannelMeta(items, ctx)

	if appendToActive {
		activeTask, err := op.GroupChannelCheckTaskFindActiveByGroup(group.ID, ctx)
		if err == nil {
			task, err := op.GroupChannelCheckTaskAppendItems(activeTask.ID, group, items, mode, channelNameByID, channelTypeByID, ctx)
			if err != nil {
				return nil, err
			}
			if enqueue {
				if err := enqueueGroupChannelCheckTaskItems(task.ID, protocol, ctx); err != nil {
					return nil, err
				}
				return op.GroupChannelCheckTaskGet(task.ID, ctx)
			}
			return task, nil
		}
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}

	task := BuildGroupChannelCheckTask(group, items, mode, channelNameByID, channelTypeByID)
	if err := op.GroupChannelCheckTaskCreate(task, ctx); err != nil {
		return nil, err
	}
	if !enqueue {
		return task, nil
	}
	if err := enqueueGroupChannelCheckTaskItems(task.ID, protocol, ctx); err != nil {
		return nil, err
	}
	return op.GroupChannelCheckTaskGet(task.ID, ctx)
}

func SendGroupChannelCheckTask(taskID int64, itemID int64, protocol model.GroupChannelCheckProtocol, ctx context.Context) (*model.GroupChannelCheckTask, error) {
	if err := sendGroupChannelCheckTaskItems(taskID, itemID, protocol, ctx); err != nil {
		return nil, err
	}
	return op.GroupChannelCheckTaskGet(taskID, ctx)
}

func sendGroupChannelCheckTaskItems(taskID int64, itemID int64, protocol model.GroupChannelCheckProtocol, ctx context.Context) error {
	return markAndEnqueueGroupChannelCheckTaskItems(taskID, itemID, protocol, true, ctx)
}

func enqueueGroupChannelCheckTaskItems(taskID int64, protocol model.GroupChannelCheckProtocol, ctx context.Context) error {
	return markAndEnqueueGroupChannelCheckTaskItems(taskID, 0, protocol, false, ctx)
}

func markAndEnqueueGroupChannelCheckTaskItems(taskID int64, itemID int64, protocol model.GroupChannelCheckProtocol, includeCompleted bool, ctx context.Context) error {
	if taskID <= 0 {
		return fmt.Errorf("invalid task id")
	}
	if !model.IsGroupChannelCheckProtocol(string(protocol)) {
		return fmt.Errorf("invalid protocol")
	}
	if includeCompleted {
		if err := op.GroupChannelCheckTaskMarkQueuedForSend(taskID, itemID, protocol, ctx); err != nil {
			return err
		}
	} else {
		if err := op.GroupChannelCheckTaskMarkQueuedPendingForSend(taskID, protocol, ctx); err != nil {
			return err
		}
	}
	if err := EnqueueGroupChannelCheckTask(taskID); err != nil {
		task, taskErr := op.GroupChannelCheckTaskGet(taskID, ctx)
		if taskErr != nil {
			return err
		}
		_ = op.GroupChannelCheckTaskUpdate(taskID, map[string]any{
			"status":        model.GroupChannelCheckTaskStatusFailed,
			"pending_count": 0,
			"failed_count":  len(task.Items),
			"finished_at":   time.Now().Unix(),
			"last_error":    err.Error(),
		}, ctx)
		return err
	}
	return nil
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
		_, _ = CreateOrAppendGroupChannelCheckTask(group, group.Items, model.GroupChannelCheckTaskModeBatch, true, true, model.GroupChannelCheckProtocolOpenAIChat, ctx)
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

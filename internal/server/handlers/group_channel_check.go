package handlers

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/server/router"
	taskpkg "github.com/bestruirui/octopus/internal/task"
	"github.com/gin-gonic/gin"
)

func init() {
	router.NewGroupRouter("/api/v1/group/channel-check").
		Use(middleware.Auth()).
		Use(middleware.RequireJSON()).
		AddRoute(
			router.NewRoute("/create", http.MethodPost).
				Handle(createGroupChannelCheckTask),
		).
		AddRoute(
			router.NewRoute("/sync-status/:id", http.MethodPost).
				Handle(syncGroupChannelCheckTaskStatus),
		)

	router.NewGroupRouter("/api/v1/group/channel-check").
		Use(middleware.Auth()).
		AddRoute(
			router.NewRoute("/latest", http.MethodGet).
				Handle(listLatestGroupChannelCheckTasks),
		).
		AddRoute(
			router.NewRoute("/detail/:id", http.MethodGet).
				Handle(getGroupChannelCheckTaskDetail),
		)
}

func createGroupChannelCheckTask(c *gin.Context) {
	var req model.GroupChannelCheckCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	group, err := op.GroupGet(req.GroupID, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusNotFound, err.Error())
		return
	}

	items, mode, err := selectGroupChannelCheckItems(group, req)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if len(items) == 0 {
		resp.Error(c, http.StatusBadRequest, "no channel found in group")
		return
	}

	task, err := taskpkg.CreateOrAppendGroupChannelCheckTask(*group, items, mode, mode == model.GroupChannelCheckTaskModeSingle, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	resp.Success(c, task)
}

func listLatestGroupChannelCheckTasks(c *gin.Context) {
	tasks, err := op.GroupChannelCheckTaskListLatest(c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, tasks)
}

func getGroupChannelCheckTaskDetail(c *gin.Context) {
	taskID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidParam)
		return
	}

	task, err := op.GroupChannelCheckTaskGet(taskID, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusNotFound, err.Error())
		return
	}
	resp.Success(c, task)
}

func syncGroupChannelCheckTaskStatus(c *gin.Context) {
	taskID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidParam)
		return
	}

	result, err := op.GroupChannelCheckTaskSyncGroupItems(taskID, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, result)
}

func selectGroupChannelCheckItems(group *model.Group, req model.GroupChannelCheckCreateRequest) ([]model.GroupItem, model.GroupChannelCheckTaskMode, error) {
	if group == nil {
		return nil, model.GroupChannelCheckTaskModeBatch, fmt.Errorf("group is nil")
	}

	items := group.Items
	if req.GroupItemID <= 0 && req.ChannelID <= 0 && req.ModelName == "" {
		return items, model.GroupChannelCheckTaskModeBatch, nil
	}

	selected := make([]model.GroupItem, 0, 1)
	for _, item := range items {
		if req.GroupItemID > 0 && item.ID == req.GroupItemID {
			selected = append(selected, item)
			break
		}
		if req.GroupItemID == 0 && req.ChannelID > 0 && req.ModelName != "" &&
			item.ChannelID == req.ChannelID && item.ModelName == req.ModelName {
			selected = append(selected, item)
			break
		}
	}
	if len(selected) == 0 {
		return nil, model.GroupChannelCheckTaskModeSingle, fmt.Errorf("group item not found")
	}
	return selected, model.GroupChannelCheckTaskModeSingle, nil
}

package handlers

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/helper"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/server/router"
	"github.com/bestruirui/octopus/internal/task"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/gin-gonic/gin"
)

const channelKeyAuditLogPrefix = "[audit][channel_key]"

func init() {
	router.NewGroupRouter("/api/v1/channel").
		Use(middleware.Auth()).
		Use(middleware.RequireJSON()).
		AddRoute(
			router.NewRoute("/list", http.MethodGet).
				Handle(listChannel),
		).
		AddRoute(
			router.NewRoute("/create", http.MethodPost).
				Handle(createChannel),
		).
		AddRoute(
			router.NewRoute("/update", http.MethodPost).
				Handle(updateChannel),
		).
		AddRoute(
			router.NewRoute("/enable", http.MethodPost).
				Handle(enableChannel),
		).
		AddRoute(
			router.NewRoute("/delete/:id", http.MethodDelete).
				Handle(deleteChannel),
		).
		AddRoute(
			router.NewRoute("/fetch-model", http.MethodPost).
				Handle(fetchModel),
		)
	router.NewGroupRouter("/api/v1/channel").
		Use(middleware.Auth()).
		AddRoute(
			router.NewRoute("/sync", http.MethodPost).
				Handle(syncChannel),
		).
		AddRoute(
			router.NewRoute("/last-sync-time", http.MethodGet).
				Handle(getLastSyncTime),
		)
}

func listChannel(c *gin.Context) {
	channels, err := op.ChannelList(c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	for i, channel := range channels {
		stats := op.StatsChannelGet(channel.ID)
		channels[i].Stats = &stats
	}
	resp.Success(c, channels)
}

func createChannel(c *gin.Context) {
	var channel model.Channel
	if err := c.ShouldBindJSON(&channel); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	// [fork] channel key audit
	log.Infof("%s op=api_channel_create_request client_ip=%s user_agent=%q channel_name=%q key_count=%d",
		channelKeyAuditLogPrefix, c.ClientIP(), c.Request.UserAgent(), channel.Name, len(channel.Keys))
	if err := op.ChannelCreate(&channel, c.Request.Context()); err != nil {
		log.Warnf("%s op=api_channel_create_failed client_ip=%s channel_name=%q err=%v",
			channelKeyAuditLogPrefix, c.ClientIP(), channel.Name, err)
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	log.Infof("%s op=api_channel_create_success client_ip=%s channel_id=%d channel_name=%q",
		channelKeyAuditLogPrefix, c.ClientIP(), channel.ID, channel.Name)
	stats := op.StatsChannelGet(channel.ID)
	channel.Stats = &stats
	go func(channel *model.Channel) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		modelStr := channel.Model + "," + channel.CustomModel
		modelArray := strings.Split(modelStr, ",")
		helper.LLMPriceAddToDB(modelArray, ctx)
		helper.ChannelBaseUrlDelayUpdate(channel, ctx)
		helper.ChannelAutoGroup(channel, ctx)
	}(&channel)
	resp.Success(c, channel)
}

func updateChannel(c *gin.Context) {
	var req model.ChannelUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	// [fork] channel key audit
	log.Infof("%s op=api_channel_update_request client_ip=%s user_agent=%q channel_id=%d add=%d update=%d delete=%d",
		channelKeyAuditLogPrefix, c.ClientIP(), c.Request.UserAgent(), req.ID, len(req.KeysToAdd), len(req.KeysToUpdate), len(req.KeysToDelete))
	channel, err := op.ChannelUpdate(&req, c.Request.Context())
	if err != nil {
		log.Warnf("%s op=api_channel_update_failed client_ip=%s channel_id=%d err=%v",
			channelKeyAuditLogPrefix, c.ClientIP(), req.ID, err)
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	log.Infof("%s op=api_channel_update_success client_ip=%s channel_id=%d",
		channelKeyAuditLogPrefix, c.ClientIP(), channel.ID)
	stats := op.StatsChannelGet(channel.ID)
	channel.Stats = &stats
	go func(channel *model.Channel) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		modelStr := channel.Model + "," + channel.CustomModel
		modelArray := strings.Split(modelStr, ",")
		helper.LLMPriceAddToDB(modelArray, ctx)
		helper.ChannelBaseUrlDelayUpdate(channel, ctx)
		helper.ChannelAutoGroup(channel, ctx)
	}(channel)
	resp.Success(c, channel)
}

func enableChannel(c *gin.Context) {
	var request struct {
		ID      int  `json:"id"`
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	if err := op.ChannelEnabled(request.ID, request.Enabled, c.Request.Context()); err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, nil)
}

func deleteChannel(c *gin.Context) {
	id := c.Param("id")
	idNum, err := strconv.Atoi(id)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidParam)
		return
	}
	// [fork] channel key audit
	log.Infof("%s op=api_channel_delete_request client_ip=%s user_agent=%q channel_id=%d",
		channelKeyAuditLogPrefix, c.ClientIP(), c.Request.UserAgent(), idNum)
	if err := op.ChannelDel(idNum, c.Request.Context()); err != nil {
		log.Warnf("%s op=api_channel_delete_failed client_ip=%s channel_id=%d err=%v",
			channelKeyAuditLogPrefix, c.ClientIP(), idNum, err)
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	log.Infof("%s op=api_channel_delete_success client_ip=%s channel_id=%d",
		channelKeyAuditLogPrefix, c.ClientIP(), idNum)
	resp.Success(c, nil)
}
func fetchModel(c *gin.Context) {
	var request model.Channel
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	models, err := helper.FetchModels(c.Request.Context(), request)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, models)
}

func syncChannel(c *gin.Context) {
	task.SyncModelsTask()
	resp.Success(c, nil)
}

func getLastSyncTime(c *gin.Context) {
	time := task.GetLastSyncModelsTime()
	resp.Success(c, time)
}

package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/server/router"
	"github.com/gin-gonic/gin"
)

func init() {
	router.NewGroupRouter("/api/v1/log").
		Use(middleware.Auth()).
		AddRoute(
			router.NewRoute("/list", http.MethodGet).
				Handle(listLog),
		).
		AddRoute(
			router.NewRoute("/clear", http.MethodDelete).
				Handle(clearLog),
		).
		AddRoute(
			router.NewRoute("/stream-token", http.MethodGet).
				Handle(getStreamToken),
		)

	router.NewGroupRouter("/api/v1/log").
		AddRoute(
			router.NewRoute("/stream", http.MethodGet).
				Handle(streamLog),
		)

	router.NewGroupRouter("/api/v1/apikey/log").
		Use(middleware.APIKeyAuth()).
		AddRoute(
			router.NewRoute("/list", http.MethodGet).
				Handle(listAPIKeyLog),
		).
		AddRoute(
			router.NewRoute("/stream-token", http.MethodGet).
				Handle(getAPIKeyStreamToken),
		)

	router.NewGroupRouter("/api/v1/apikey/log").
		AddRoute(
			router.NewRoute("/stream", http.MethodGet).
				Handle(streamAPIKeyLog),
		)
}

func parseLogListParams(c *gin.Context) (page int, pageSize int, startTime *int, endTime *int, err error) {
	page, _ = strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ = strconv.Atoi(c.DefaultQuery("page_size", "20"))
	startTimeStr := c.Query("start_time")
	endTimeStr := c.Query("end_time")

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	if startTimeStr != "" && endTimeStr != "" {
		st, parseErr := strconv.Atoi(startTimeStr)
		if parseErr != nil {
			return 0, 0, nil, nil, parseErr
		}
		et, parseErr := strconv.Atoi(endTimeStr)
		if parseErr != nil {
			return 0, 0, nil, nil, parseErr
		}
		startTime = &st
		endTime = &et
	}

	return page, pageSize, startTime, endTime, nil
}

func matchLogScope(relayLog model.RelayLog, scope op.RelayLogStreamScope) bool {
	if scope.APIKeyID <= 0 {
		return true
	}
	if relayLog.APIKeyID == scope.APIKeyID {
		return true
	}
	return relayLog.APIKeyID == 0 && scope.APIKeyName != "" && relayLog.APIKeyName == scope.APIKeyName
}

func streamLogWithScope(c *gin.Context, requireAPIKeyScope bool) {
	token := c.Query("token")
	scope, ok := op.RelayLogStreamTokenVerify(token)
	if token == "" || !ok {
		resp.Error(c, http.StatusUnauthorized, "invalid stream token")
		return
	}
	if requireAPIKeyScope && scope.APIKeyID <= 0 {
		resp.Error(c, http.StatusUnauthorized, "invalid stream token")
		return
	}

	op.RelayLogStreamTokenRevoke(token)

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	logChan := op.RelayLogSubscribe()
	defer op.RelayLogUnsubscribe(logChan)

	ctx := c.Request.Context()

	for {
		select {
		case <-ctx.Done():
			return
		case relayLog, alive := <-logChan:
			if !alive {
				return
			}
			if !matchLogScope(relayLog, scope) {
				continue
			}
			data, marshalErr := json.Marshal(relayLog)
			if marshalErr != nil {
				continue
			}
			c.Writer.Write([]byte(fmt.Sprintf("data: %s\n\n", data)))
			c.Writer.Flush()
		}
	}
}

func listLog(c *gin.Context) {
	page, pageSize, startTime, endTime, err := parseLogListParams(c)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	logs, err := op.RelayLogList(c.Request.Context(), startTime, endTime, page, pageSize, nil, nil)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	resp.Success(c, logs)
}

func clearLog(c *gin.Context) {
	if err := op.RelayLogClear(c.Request.Context()); err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, nil)
}

func getStreamToken(c *gin.Context) {
	token, err := op.RelayLogStreamTokenCreate(op.RelayLogStreamScope{})
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, gin.H{"token": token})
}

func streamLog(c *gin.Context) {
	streamLogWithScope(c, false)
}

func listAPIKeyLog(c *gin.Context) {
	page, pageSize, startTime, endTime, err := parseLogListParams(c)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	apiKeyID := c.GetInt("api_key_id")
	apiKeyName := c.GetString("api_key_name")
	logs, err := op.RelayLogList(c.Request.Context(), startTime, endTime, page, pageSize, &apiKeyID, &apiKeyName)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, logs)
}

func getAPIKeyStreamToken(c *gin.Context) {
	scope := op.RelayLogStreamScope{
		APIKeyID:   c.GetInt("api_key_id"),
		APIKeyName: c.GetString("api_key_name"),
	}
	token, err := op.RelayLogStreamTokenCreate(scope)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, gin.H{"token": token})
}

func streamAPIKeyLog(c *gin.Context) {
	streamLogWithScope(c, true)
}

package op

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

func setupRelayLogTestDB(t *testing.T) context.Context {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "relay-log-test.db")
	if err := db.InitDB("sqlite", dbPath, false); err != nil {
		t.Fatalf("init db failed: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	if err := InitCache(); err != nil {
		t.Fatalf("init cache failed: %v", err)
	}

	resetRelayLogTestState()
	return context.Background()
}

func resetRelayLogTestState() {
	relayLogCacheLock.Lock()
	relayLogCache = make([]model.RelayLog, 0, relayLogMaxSize)
	relayLogCacheLock.Unlock()

	relayLogSubscribersLock.Lock()
	for ch := range relayLogSubscribers {
		close(ch)
		delete(relayLogSubscribers, ch)
	}
	relayLogSubscribersLock.Unlock()

	relayLogStreamTokensLock.Lock()
	relayLogStreamTokens = make(map[string]RelayLogStreamScope)
	relayLogStreamTokensLock.Unlock()
}

func TestRelayLogListSummaryOmitsContentAndMarksFlag(t *testing.T) {
	ctx := setupRelayLogTestDB(t)

	logEntry := model.RelayLog{
		Time:                    time.Now().Unix(),
		RequestModelName:        "gpt-4o-mini",
		ChannelId:               1,
		ChannelName:             "test-channel",
		ActualModelName:         "gpt-4o-mini",
		InputTokens:             10,
		OutputTokens:            20,
		Ftut:                    123,
		UseTime:                 456,
		Cost:                    0.001,
		RequestContent:          `{"req":"full"}`,
		OriginalRequestContent:  `{"raw":"full"}`,
		OutboundRequestContent:  `{"outbound":"full"}`,
		OriginalResponseContent: `{"upstream":"full"}`,
		ResponseContent:         `{"resp":"full"}`,
		StreamPreviewContent:    `{"preview":"full"}`,
	}
	if err := RelayLogAdd(ctx, logEntry); err != nil {
		t.Fatalf("relay log add failed: %v", err)
	}
	if err := RelayLogSaveDBTask(ctx); err != nil {
		t.Fatalf("relay log save db task failed: %v", err)
	}

	relayLogCacheLock.Lock()
	relayLogCache = make([]model.RelayLog, 0, relayLogMaxSize)
	relayLogCacheLock.Unlock()

	logs, err := RelayLogListSummary(ctx, nil, nil, 1, 20, nil, nil)
	if err != nil {
		t.Fatalf("relay log list summary failed: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 summary log, got %d", len(logs))
	}
	if logs[0].RequestContent != "" {
		t.Fatalf("expected request content omitted, got %q", logs[0].RequestContent)
	}
	if logs[0].OriginalRequestContent != "" {
		t.Fatalf("expected original request content omitted, got %q", logs[0].OriginalRequestContent)
	}
	if logs[0].OutboundRequestContent != "" {
		t.Fatalf("expected outbound request content omitted, got %q", logs[0].OutboundRequestContent)
	}
	if logs[0].OriginalResponseContent != "" {
		t.Fatalf("expected original response content omitted, got %q", logs[0].OriginalResponseContent)
	}
	if logs[0].ResponseContent != "" {
		t.Fatalf("expected response content omitted, got %q", logs[0].ResponseContent)
	}
	if logs[0].StreamPreviewContent != "" {
		t.Fatalf("expected stream preview content omitted, got %q", logs[0].StreamPreviewContent)
	}
	if !logs[0].ContentOmitted {
		t.Fatalf("expected content_omitted=true for summary log")
	}
}

func TestRelayLogGetByIDRespectsAPIKeyScope(t *testing.T) {
	ctx := setupRelayLogTestDB(t)

	fullLog := model.RelayLog{
		ID:                      10001,
		Time:                    time.Now().Unix(),
		RequestModelName:        "gpt-4o",
		ChannelId:               2,
		ChannelName:             "scope-channel",
		ActualModelName:         "gpt-4o",
		RequestContent:          `{"request":"data"}`,
		OriginalRequestContent:  `{"raw":"request"}`,
		OriginalRequestProtocol: "OpenAI",
		OutboundRequestContent:  `{"outbound":"request"}`,
		OutboundRequestProtocol: "Anthropic",
		OriginalResponseContent: `{"upstream":"response"}`,
		ResponseContent:         `{"response":"data"}`,
		StreamPreviewContent:    `{"preview":"response"}`,
		APIKeyID:                10,
		APIKeyName:              "ak-10",
	}
	if err := db.GetDB().WithContext(ctx).Create(&fullLog).Error; err != nil {
		t.Fatalf("insert scoped relay log failed: %v", err)
	}

	name := "ak-10"
	apiKeyID := 10
	got, err := RelayLogGetByID(ctx, fullLog.ID, &apiKeyID, &name)
	if err != nil {
		t.Fatalf("get by id with matched scope failed: %v", err)
	}
	if got == nil {
		t.Fatalf("expected scoped relay log, got nil")
	}
	if got.RequestContent == "" || got.ResponseContent == "" {
		t.Fatalf("expected full content from detail endpoint path")
	}
	if got.OriginalResponseContent == "" {
		t.Fatalf("expected full upstream response diagnostics from detail endpoint path")
	}
	if got.StreamPreviewContent == "" {
		t.Fatalf("expected final stream preview from detail endpoint path")
	}
	if got.OriginalRequestContent == "" || got.OutboundRequestContent == "" {
		t.Fatalf("expected full request diagnostics from detail endpoint path")
	}

	mismatchName := "ak-11"
	mismatchID := 11
	notFound, err := RelayLogGetByID(ctx, fullLog.ID, &mismatchID, &mismatchName)
	if err != nil {
		t.Fatalf("get by id with mismatched scope failed: %v", err)
	}
	if notFound != nil {
		t.Fatalf("expected nil for mismatched scope, got %+v", notFound)
	}
}

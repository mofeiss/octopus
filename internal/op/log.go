package op

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/snowflake"
	"gorm.io/gorm"
)

const relayLogMaxSize = 20
const relayLogMaxSizeNoDB = 100 // 当不保存到数据库时，允许更大的缓存用于实时查询

var relayLogCache = make([]model.RelayLog, 0, relayLogMaxSize)
var relayLogCacheLock sync.Mutex

var relayLogFlushLock sync.Mutex

var relayLogSubscribers = make(map[chan model.RelayLog]struct{})
var relayLogSubscribersLock sync.RWMutex

type RelayLogStreamScope struct {
	APIKeyID   int
	APIKeyName string
}

var relayLogStreamTokens = make(map[string]RelayLogStreamScope)
var relayLogStreamTokensLock sync.RWMutex

func RelayLogStreamTokenCreate(scope RelayLogStreamScope) (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	token := hex.EncodeToString(bytes)

	relayLogStreamTokensLock.Lock()
	relayLogStreamTokens[token] = scope
	relayLogStreamTokensLock.Unlock()

	return token, nil
}

func RelayLogStreamTokenVerify(token string) (RelayLogStreamScope, bool) {
	relayLogStreamTokensLock.RLock()
	scope, ok := relayLogStreamTokens[token]
	relayLogStreamTokensLock.RUnlock()
	return scope, ok
}

func RelayLogStreamTokenRevoke(token string) {
	relayLogStreamTokensLock.Lock()
	delete(relayLogStreamTokens, token)
	relayLogStreamTokensLock.Unlock()
}

func RelayLogSubscribe() chan model.RelayLog {
	ch := make(chan model.RelayLog, 10)
	relayLogSubscribersLock.Lock()
	relayLogSubscribers[ch] = struct{}{}
	relayLogSubscribersLock.Unlock()
	return ch
}

func RelayLogUnsubscribe(ch chan model.RelayLog) {
	relayLogSubscribersLock.Lock()
	delete(relayLogSubscribers, ch)
	relayLogSubscribersLock.Unlock()
	close(ch)
}

func notifySubscribers(relayLog model.RelayLog) {
	relayLogSubscribersLock.RLock()
	defer relayLogSubscribersLock.RUnlock()

	for ch := range relayLogSubscribers {
		select {
		case ch <- relayLog:
		default:
		}
	}
}

func relayLogFlushToDB(ctx context.Context) error {
	relayLogFlushLock.Lock()
	defer relayLogFlushLock.Unlock()

	relayLogCacheLock.Lock()
	if len(relayLogCache) == 0 {
		relayLogCacheLock.Unlock()
		return nil
	}
	batch := make([]model.RelayLog, len(relayLogCache))
	copy(batch, relayLogCache)
	flushedUpto := len(batch)
	relayLogCacheLock.Unlock()

	result := db.GetDB().WithContext(ctx).Create(&batch)
	if result.Error != nil {
		return result.Error
	}

	relayLogCacheLock.Lock()
	if len(relayLogCache) >= flushedUpto {
		relayLogCache = relayLogCache[flushedUpto:]
	} else {
		relayLogCache = relayLogCache[:0]
	}
	if len(relayLogCache) == 0 {
		relayLogCache = make([]model.RelayLog, 0, relayLogMaxSize)
	}
	relayLogCacheLock.Unlock()

	return nil
}

func RelayLogAdd(ctx context.Context, relayLog model.RelayLog) error {
	enabled, err := SettingGetBool(model.SettingKeyRelayLogKeepEnabled)
	if err != nil {
		return err
	}
	maxSize := relayLogMaxSize
	if !enabled {
		maxSize = relayLogMaxSizeNoDB
	}
	relayLog.ID = snowflake.GenerateID()
	go notifySubscribers(relayLog)

	relayLogCacheLock.Lock()
	relayLogCache = append(relayLogCache, relayLog)
	if len(relayLogCache) >= maxSize {
		if enabled {
			relayLogCacheLock.Unlock()
			return relayLogFlushToDB(ctx)
		}
		// 如果未启用日志保存，移除最旧的日志，保留最新的日志用于实时查询
		keepSize := maxSize / 2
		if len(relayLogCache) > keepSize {
			relayLogCache = relayLogCache[len(relayLogCache)-keepSize:]
		}
	}
	relayLogCacheLock.Unlock()
	return nil
}

func RelayLogSaveDBTask(ctx context.Context) error {
	log.Debugf("relay log save db task started")
	startTime := time.Now()
	defer func() {
		log.Debugf("relay log save db task finished, save time: %s", time.Since(startTime))
	}()
	enabled, err := SettingGetBool(model.SettingKeyRelayLogKeepEnabled)
	if err != nil {
		return err
	}

	if enabled {
		if err := relayLogFlushToDB(ctx); err != nil {
			return err
		}
		return relayLogCleanup(ctx)
	}

	// 如果未启用日志保存，检查缓存大小，如果超过限制则清理旧日志
	relayLogCacheLock.Lock()
	if len(relayLogCache) > relayLogMaxSizeNoDB {
		keepSize := relayLogMaxSizeNoDB / 2
		relayLogCache = relayLogCache[len(relayLogCache)-keepSize:]
	}
	relayLogCacheLock.Unlock()

	return nil
}

func relayLogCleanup(ctx context.Context) error {
	keepPeriod, err := SettingGetInt(model.SettingKeyRelayLogKeepPeriod)
	if err != nil {
		return err
	}

	if keepPeriod <= 0 {
		return nil
	}

	cutoffTime := time.Now().Add(-time.Duration(keepPeriod) * 24 * time.Hour).Unix()
	return db.GetDB().WithContext(ctx).Where("time < ?", cutoffTime).Delete(&model.RelayLog{}).Error
}

func matchRelayLogAPIKeyScope(log model.RelayLog, apiKeyID *int, apiKeyName *string) bool {
	if apiKeyID == nil || *apiKeyID <= 0 {
		return true
	}
	if log.APIKeyID == *apiKeyID {
		return true
	}
	return log.APIKeyID == 0 && apiKeyName != nil && *apiKeyName != "" && log.APIKeyName == *apiKeyName
}

// [fork] build summary payload without heavy content
func relayLogToSummary(relayLog model.RelayLog) model.RelayLog {
	summary := relayLog
	summary.RequestContent = ""
	summary.OriginalRequestContent = ""
	summary.OutboundRequestContent = ""
	summary.OriginalResponseContent = ""
	summary.ResponseContent = ""
	summary.StreamPreviewContent = ""
	summary.ContentOmitted = true
	return summary
}

func relayLogListWithContentFlag(
	ctx context.Context,
	startTime, endTime *int,
	page, pageSize int,
	apiKeyID *int,
	apiKeyName *string,
	includeContent bool,
) ([]model.RelayLog, error) {
	enabled, err := SettingGetBool(model.SettingKeyRelayLogKeepEnabled)
	if err != nil {
		return nil, err
	}
	hasTimeFilter := startTime != nil && endTime != nil

	// 获取缓存中符合条件的日志
	relayLogCacheLock.Lock()
	var cachedLogs []model.RelayLog
	for _, relayLog := range relayLogCache {
		if !matchRelayLogAPIKeyScope(relayLog, apiKeyID, apiKeyName) {
			continue
		}
		if hasTimeFilter {
			if relayLog.Time < int64(*startTime) || relayLog.Time > int64(*endTime) {
				continue
			}
		}
		if includeContent {
			cachedLogs = append(cachedLogs, relayLog)
		} else {
			cachedLogs = append(cachedLogs, relayLogToSummary(relayLog))
		}
	}
	relayLogCacheLock.Unlock()

	// 反转缓存日志顺序（原本新的在末尾，反转后新的在前面，方便分页）
	for i, j := 0, len(cachedLogs)-1; i < j; i, j = i+1, j-1 {
		cachedLogs[i], cachedLogs[j] = cachedLogs[j], cachedLogs[i]
	}

	cacheCount := len(cachedLogs)
	offset := (page - 1) * pageSize

	var result []model.RelayLog

	// 先从缓存中取（缓存是最新的日志）
	if offset < cacheCount {
		cacheEnd := offset + pageSize
		if cacheEnd > cacheCount {
			cacheEnd = cacheCount
		}
		result = append(result, cachedLogs[offset:cacheEnd]...)
	}

	// 如果启用了日志保存，缓存不够时从数据库补充
	if enabled {
		remaining := pageSize - len(result)
		if remaining > 0 {
			dbOffset := 0
			if offset > cacheCount {
				dbOffset = offset - cacheCount
			}

			query := db.GetDB().WithContext(ctx)
			if hasTimeFilter {
				query = query.Where("time >= ? AND time <= ?", *startTime, *endTime)
			}
			if apiKeyID != nil && *apiKeyID > 0 {
				if apiKeyName != nil && *apiKeyName != "" {
					query = query.Where("(api_key_id = ? OR (api_key_id = 0 AND api_key_name = ?))", *apiKeyID, *apiKeyName)
				} else {
					query = query.Where("api_key_id = ?", *apiKeyID)
				}
			}
			if !includeContent {
				query = query.Omit("request_content", "original_request_content", "outbound_request_content", "original_response_content", "response_content", "stream_preview_content")
			}

			var dbLogs []model.RelayLog
			if err := query.Order("id DESC").Offset(dbOffset).Limit(remaining).Find(&dbLogs).Error; err != nil {
				return nil, err
			}
			if !includeContent {
				for i := range dbLogs {
					dbLogs[i].ContentOmitted = true
				}
			}
			result = append(result, dbLogs...)
		}
	}

	return result, nil
}

// RelayLogList 查询日志列表，支持可选的时间范围过滤
// startTime 和 endTime 为 nil 时表示不限制时间范围
func RelayLogList(ctx context.Context, startTime, endTime *int, page, pageSize int, apiKeyID *int, apiKeyName *string) ([]model.RelayLog, error) {
	return relayLogListWithContentFlag(ctx, startTime, endTime, page, pageSize, apiKeyID, apiKeyName, true)
}

// [fork] RelayLogListSummary 查询不包含 request/response 正文的摘要日志
func RelayLogListSummary(ctx context.Context, startTime, endTime *int, page, pageSize int, apiKeyID *int, apiKeyName *string) ([]model.RelayLog, error) {
	return relayLogListWithContentFlag(ctx, startTime, endTime, page, pageSize, apiKeyID, apiKeyName, false)
}

// [fork] RelayLogGetByID returns full log content for detail view with scope guard
func RelayLogGetByID(ctx context.Context, id int64, apiKeyID *int, apiKeyName *string) (*model.RelayLog, error) {
	relayLogCacheLock.Lock()
	for i := len(relayLogCache) - 1; i >= 0; i-- {
		if relayLogCache[i].ID != id {
			continue
		}
		if !matchRelayLogAPIKeyScope(relayLogCache[i], apiKeyID, apiKeyName) {
			continue
		}
		cached := relayLogCache[i]
		relayLogCacheLock.Unlock()
		return &cached, nil
	}
	relayLogCacheLock.Unlock()

	query := db.GetDB().WithContext(ctx).Where("id = ?", id)
	if apiKeyID != nil && *apiKeyID > 0 {
		if apiKeyName != nil && *apiKeyName != "" {
			query = query.Where("(api_key_id = ? OR (api_key_id = 0 AND api_key_name = ?))", *apiKeyID, *apiKeyName)
		} else {
			query = query.Where("api_key_id = ?", *apiKeyID)
		}
	}

	var relayLog model.RelayLog
	if err := query.First(&relayLog).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &relayLog, nil
}

func RelayLogClear(ctx context.Context) error {
	relayLogCacheLock.Lock()
	relayLogCache = make([]model.RelayLog, 0, relayLogMaxSize)
	relayLogCacheLock.Unlock()
	return db.GetDB().WithContext(ctx).Where("1 = 1").Delete(&model.RelayLog{}).Error
}

package op

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/utils/cache"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/xstrings"
	"gorm.io/gorm"
)

var channelCache = cache.New[int, model.Channel](16)
var channelKeyCache = cache.New[int, model.ChannelKey](16)
var channelKeyCacheNeedUpdate = make(map[int]struct{})
var channelKeyCacheNeedUpdateLock sync.Mutex

func ChannelList(ctx context.Context) ([]model.Channel, error) {
	channels := make([]model.Channel, 0, channelCache.Len())
	for _, channel := range channelCache.GetAll() {
		channels = append(channels, channel)
	}
	return channels, nil
}

func ChannelCreate(channel *model.Channel, ctx context.Context) error {
	if err := db.GetDB().WithContext(ctx).Create(channel).Error; err != nil {
		return err
	}
	channelCache.Set(channel.ID, *channel)
	for _, k := range channel.Keys {
		if k.ID != 0 {
			channelKeyCache.Set(k.ID, k)
		}
	}
	// [fork] channel key audit
	log.Infof("%s op=channel_create channel_id=%d channel_name=%q key_count=%d keys=%s",
		channelKeyAuditPrefix, channel.ID, channel.Name, len(channel.Keys), summarizeKeysForAudit(channel.Keys))
	return nil
}

// ChannelKeyUpdate 仅更新 ChannelKey 的内存缓存（不落库），并标记为需要在 SaveCache 时写入数据库。
func ChannelKeyUpdate(key model.ChannelKey) error {
	if key.ID == 0 || key.ChannelID == 0 {
		err := fmt.Errorf("invalid channel key")
		log.Warnf("%s op=runtime_update_invalid_key incoming=%s err=%v", channelKeyAuditPrefix, summarizeSingleKeyForAudit(key), err)
		return err
	}
	ch, ok := channelCache.Get(key.ChannelID)
	if !ok {
		err := fmt.Errorf("channel not found")
		log.Warnf("%s op=runtime_update_channel_not_found incoming=%s err=%v", channelKeyAuditPrefix, summarizeSingleKeyForAudit(key), err)
		return err
	}
	if len(ch.Keys) == 0 {
		err := fmt.Errorf("channel key not found")
		log.Warnf("%s op=runtime_update_no_keys channel_id=%d incoming=%s err=%v", channelKeyAuditPrefix, key.ChannelID, summarizeSingleKeyForAudit(key), err)
		return err
	}

	keys := make([]model.ChannelKey, len(ch.Keys))
	copy(keys, ch.Keys)

	found := false
	var merged model.ChannelKey
	for i := range keys {
		if keys[i].ID != key.ID {
			continue
		}
		// [fork] channel key audit: detect stale runtime snapshot touching config fields
		if key.ChannelKey != keys[i].ChannelKey {
			log.Warnf("%s op=runtime_update_key_mismatch channel_id=%d key_id=%d incoming_key=%q stored_key=%q",
				channelKeyAuditPrefix, key.ChannelID, key.ID, key.ChannelKey, keys[i].ChannelKey)
		}
		if key.Remark != keys[i].Remark {
			log.Warnf("%s op=runtime_update_remark_mismatch channel_id=%d key_id=%d incoming_remark=%q stored_remark=%q",
				channelKeyAuditPrefix, key.ChannelID, key.ID, key.Remark, keys[i].Remark)
		}
		if key.Enabled != keys[i].Enabled {
			log.Warnf("%s op=runtime_update_enabled_mismatch channel_id=%d key_id=%d incoming_enabled=%t stored_enabled=%t",
				channelKeyAuditPrefix, key.ChannelID, key.ID, key.Enabled, keys[i].Enabled)
		}
		// [fork] 仅允许运行时字段更新，避免 channel_key/remark/channel_id 被旧快照覆盖
		keys[i].StatusCode = key.StatusCode
		keys[i].LastUseTimeStamp = key.LastUseTimeStamp
		keys[i].TotalCost = key.TotalCost
		merged = keys[i]
		found = true
		break
	}
	if !found {
		// [fork] key 已被删除或不再属于该渠道时拒绝写入，避免后续落库复活旧 key
		err := fmt.Errorf("channel key %d not found in channel %d", key.ID, key.ChannelID)
		log.Warnf("%s op=runtime_update_missing_key incoming=%s err=%v", channelKeyAuditPrefix, summarizeSingleKeyForAudit(key), err)
		return err
	}

	ch.Keys = keys
	channelCache.Set(key.ChannelID, ch)
	channelKeyCache.Set(merged.ID, merged)
	channelKeyCacheNeedUpdateLock.Lock()
	channelKeyCacheNeedUpdate[key.ID] = struct{}{}
	channelKeyCacheNeedUpdateLock.Unlock()
	return nil
}
func ChannelBaseUrlUpdate(channelID int, baseUrl []model.BaseUrl) error {
	ch, ok := channelCache.Get(channelID)
	if !ok {
		return fmt.Errorf("channel not found")
	}
	// Copy to decouple callers from internal cache storage.
	if baseUrl == nil {
		ch.BaseUrls = nil
	} else {
		cp := make([]model.BaseUrl, len(baseUrl))
		copy(cp, baseUrl)
		ch.BaseUrls = cp
	}
	channelCache.Set(channelID, ch)
	return nil
}

// ChannelKeySaveDB 将运行时更新过的 ChannelKey 缓存写入数据库。
func ChannelKeySaveDB(ctx context.Context) error {
	channelKeyCacheNeedUpdateLock.Lock()
	keyIDs := make([]int, 0, len(channelKeyCacheNeedUpdate))
	for id := range channelKeyCacheNeedUpdate {
		keyIDs = append(keyIDs, id)
	}
	channelKeyCacheNeedUpdate = make(map[int]struct{})
	channelKeyCacheNeedUpdateLock.Unlock()

	if len(keyIDs) == 0 {
		return nil
	}

	log.Infof("%s op=save_db_begin pending=%d", channelKeyAuditPrefix, len(keyIDs))
	updatedCount := 0
	skippedCount := 0
	dbConn := db.GetDB().WithContext(ctx)
	for _, id := range keyIDs {
		k, ok := channelKeyCache.Get(id)
		if !ok {
			skippedCount++
			log.Warnf("%s op=save_db_cache_miss key_id=%d", channelKeyAuditPrefix, id)
			continue
		}

		// [fork] channel key audit: compare cache snapshot and DB config fields before runtime-field flush
		var dbKey model.ChannelKey
		if err := dbConn.Select("id", "channel_id", "channel_key", "remark").
			Where("id = ?", k.ID).
			First(&dbKey).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				skippedCount++
				log.Warnf("%s op=save_db_missing_row cache={id=%d,cid=%d,key=%q,remark=%q}",
					channelKeyAuditPrefix, k.ID, k.ChannelID, k.ChannelKey, k.Remark)
				continue
			}
			return err
		}
		if dbKey.ChannelID != k.ChannelID || dbKey.ChannelKey != k.ChannelKey || dbKey.Remark != k.Remark {
			log.Warnf("%s op=save_db_snapshot_mismatch key_id=%d db={id=%d,cid=%d,key=%q,remark=%q} cache={id=%d,cid=%d,key=%q,remark=%q}",
				channelKeyAuditPrefix, k.ID,
				dbKey.ID, dbKey.ChannelID, dbKey.ChannelKey, dbKey.Remark,
				k.ID, k.ChannelID, k.ChannelKey, k.Remark)
		}

		// [fork] 仅落库运行时字段，禁止覆盖 channel_key/remark/channel_id 等配置字段
		result := dbConn.Model(&model.ChannelKey{}).
			Where("id = ? AND channel_id = ?", k.ID, k.ChannelID).
			Updates(map[string]interface{}{
				"status_code":         k.StatusCode,
				"last_use_time_stamp": k.LastUseTimeStamp,
				"total_cost":          k.TotalCost,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			skippedCount++
			log.Warnf("%s op=save_db_no_rows_affected key_id=%d channel_id=%d", channelKeyAuditPrefix, k.ID, k.ChannelID)
			continue
		}
		updatedCount++
	}
	log.Infof("%s op=save_db_end pending=%d updated=%d skipped=%d", channelKeyAuditPrefix, len(keyIDs), updatedCount, skippedCount)
	return nil
}

func ChannelUpdate(req *model.ChannelUpdateRequest, ctx context.Context) (*model.Channel, error) {
	oldChannel, ok := channelCache.Get(req.ID)
	if !ok {
		return nil, fmt.Errorf("channel not found")
	}
	oldByID := make(map[int]model.ChannelKey, len(oldChannel.Keys))
	for _, k := range oldChannel.Keys {
		oldByID[k.ID] = k
	}
	// [fork] channel key audit
	log.Infof("%s op=channel_update_begin channel_id=%d req=%s keys_before=%s",
		channelKeyAuditPrefix, req.ID, summarizeChannelUpdateReqForAudit(req, oldByID), summarizeKeysForAudit(oldChannel.Keys))

	tx := db.GetDB().WithContext(ctx).Begin()
	defer func() {
		if r := recover(); r != nil {
			log.Warnf("%s op=channel_update_panic channel_id=%d panic=%v", channelKeyAuditPrefix, req.ID, r)
			tx.Rollback()
		}
	}()

	var selectFields []string
	updates := model.Channel{ID: req.ID}

	if req.Name != nil {
		selectFields = append(selectFields, "name")
		updates.Name = *req.Name
	}
	if req.Type != nil {
		selectFields = append(selectFields, "type")
		updates.Type = *req.Type
	}
	if req.Enabled != nil {
		selectFields = append(selectFields, "enabled")
		updates.Enabled = *req.Enabled
	}
	if req.BaseUrls != nil {
		selectFields = append(selectFields, "base_urls")
		updates.BaseUrls = *req.BaseUrls
	}
	if req.Model != nil {
		selectFields = append(selectFields, "model")
		updates.Model = *req.Model
	}
	if req.CustomModel != nil {
		selectFields = append(selectFields, "custom_model")
		updates.CustomModel = *req.CustomModel
	}
	if req.Proxy != nil {
		selectFields = append(selectFields, "proxy")
		updates.Proxy = *req.Proxy
	}
	if req.AutoSync != nil {
		selectFields = append(selectFields, "auto_sync")
		updates.AutoSync = *req.AutoSync
	}
	if req.AutoGroup != nil {
		selectFields = append(selectFields, "auto_group")
		updates.AutoGroup = *req.AutoGroup
	}
	if req.CustomHeader != nil {
		selectFields = append(selectFields, "custom_header")
		updates.CustomHeader = *req.CustomHeader
	}
	if req.ChannelProxy != nil {
		selectFields = append(selectFields, "channel_proxy")
		updates.ChannelProxy = req.ChannelProxy
	}
	if req.ParamOverride != nil {
		selectFields = append(selectFields, "param_override")
		updates.ParamOverride = req.ParamOverride
	}
	if req.MatchRegex != nil {
		selectFields = append(selectFields, "match_regex")
		updates.MatchRegex = req.MatchRegex
	}
	// [fork]
	if req.Remark != nil {
		selectFields = append(selectFields, "remark")
		updates.Remark = *req.Remark
	}

	// 只有当有字段需要更新时才执行 UPDATE
	if len(selectFields) > 0 {
		if err := tx.Model(&model.Channel{}).Where("id = ?", req.ID).Select(selectFields).Updates(&updates).Error; err != nil {
			tx.Rollback()
			log.Warnf("%s op=channel_update_failed stage=update_channel channel_id=%d err=%v", channelKeyAuditPrefix, req.ID, err)
			return nil, fmt.Errorf("failed to update channel: %w", err)
		}
	}

	// 删除 keys
	if len(req.KeysToDelete) > 0 {
		if err := tx.Where("id IN ? AND channel_id = ?", req.KeysToDelete, req.ID).Delete(&model.ChannelKey{}).Error; err != nil {
			tx.Rollback()
			log.Warnf("%s op=channel_update_failed stage=delete_keys channel_id=%d err=%v", channelKeyAuditPrefix, req.ID, err)
			return nil, fmt.Errorf("failed to delete channel keys: %w", err)
		}
	}

	// 更新 keys（逐条，只更新提供的字段）
	if len(req.KeysToUpdate) > 0 {
		for _, ku := range req.KeysToUpdate {
			updates := map[string]interface{}{}
			if ku.Enabled != nil {
				updates["enabled"] = *ku.Enabled
			}
			if ku.ChannelKey != nil {
				updates["channel_key"] = *ku.ChannelKey
			}
			if ku.Remark != nil {
				updates["remark"] = *ku.Remark
			}
			if len(updates) == 0 {
				continue
			}
			if err := tx.Model(&model.ChannelKey{}).
				Where("id = ? AND channel_id = ?", ku.ID, req.ID).
				Updates(updates).Error; err != nil {
				tx.Rollback()
				log.Warnf("%s op=channel_update_failed stage=update_key channel_id=%d key_id=%d err=%v", channelKeyAuditPrefix, req.ID, ku.ID, err)
				return nil, fmt.Errorf("failed to update channel key %d: %w", ku.ID, err)
			}
		}
	}

	// 新增 keys
	if len(req.KeysToAdd) > 0 {
		newKeys := make([]model.ChannelKey, 0, len(req.KeysToAdd))
		for _, ka := range req.KeysToAdd {
			newKeys = append(newKeys, model.ChannelKey{
				ChannelID:  req.ID,
				Enabled:    ka.Enabled,
				ChannelKey: ka.ChannelKey,
				Remark:     ka.Remark,
			})
		}
		if err := tx.Create(&newKeys).Error; err != nil {
			tx.Rollback()
			log.Warnf("%s op=channel_update_failed stage=create_keys channel_id=%d err=%v", channelKeyAuditPrefix, req.ID, err)
			return nil, fmt.Errorf("failed to create channel keys: %w", err)
		}
	}

	if err := tx.Commit().Error; err != nil {
		log.Warnf("%s op=channel_update_failed stage=commit channel_id=%d err=%v", channelKeyAuditPrefix, req.ID, err)
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// 刷新缓存并返回最新数据
	if err := channelRefreshCacheByID(req.ID, ctx); err != nil {
		log.Warnf("%s op=channel_update_failed stage=refresh_cache channel_id=%d err=%v", channelKeyAuditPrefix, req.ID, err)
		return nil, err
	}

	channel, ok := channelCache.Get(req.ID)
	if !ok {
		err := fmt.Errorf("channel not found after refresh")
		log.Warnf("%s op=channel_update_failed stage=post_refresh_get channel_id=%d err=%v", channelKeyAuditPrefix, req.ID, err)
		return nil, err
	}
	log.Infof("%s op=channel_update_success channel_id=%d keys_after=%s", channelKeyAuditPrefix, channel.ID, summarizeKeysForAudit(channel.Keys))
	return &channel, nil
}

func ChannelEnabled(id int, enabled bool, ctx context.Context) error {
	oldChannel, ok := channelCache.Get(id)
	if !ok {
		return fmt.Errorf("channel not found")
	}
	if err := db.GetDB().WithContext(ctx).Model(&model.Channel{}).Where("id = ?", id).Update("enabled", enabled).Error; err != nil {
		return err
	}
	oldChannel.Enabled = enabled
	channelCache.Set(id, oldChannel)
	return nil
}

func ChannelDel(id int, ctx context.Context) error {
	ch, ok := channelCache.Get(id)
	if !ok {
		return fmt.Errorf("channel not found")
	}
	// [fork] channel key audit
	log.Infof("%s op=channel_delete_begin channel_id=%d key_count=%d keys=%s",
		channelKeyAuditPrefix, id, len(ch.Keys), summarizeKeysForAudit(ch.Keys))

	// 开启事务
	tx := db.GetDB().WithContext(ctx).Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// 获取所有受影响的 GroupID，用于刷新缓存
	var affectedGroupIDs []int
	if err := tx.Model(&model.GroupItem{}).
		Where("channel_id = ?", id).
		Pluck("group_id", &affectedGroupIDs).Error; err != nil {
		tx.Rollback()
		log.Warnf("%s op=channel_delete_failed stage=query_affected_groups channel_id=%d err=%v", channelKeyAuditPrefix, id, err)
		return fmt.Errorf("failed to get affected groups: %w", err)
	}

	// 删除所有引用该渠道的 GroupItem
	if err := tx.Where("channel_id = ?", id).Delete(&model.GroupItem{}).Error; err != nil {
		tx.Rollback()
		log.Warnf("%s op=channel_delete_failed stage=delete_group_items channel_id=%d err=%v", channelKeyAuditPrefix, id, err)
		return fmt.Errorf("failed to delete group items: %w", err)
	}

	// 删除渠道 keys
	if err := tx.Where("channel_id = ?", id).Delete(&model.ChannelKey{}).Error; err != nil {
		tx.Rollback()
		log.Warnf("%s op=channel_delete_failed stage=delete_keys channel_id=%d err=%v", channelKeyAuditPrefix, id, err)
		return fmt.Errorf("failed to delete channel keys: %w", err)
	}

	// 删除统计数据
	if err := tx.Where("channel_id = ?", id).Delete(&model.StatsChannel{}).Error; err != nil {
		tx.Rollback()
		log.Warnf("%s op=channel_delete_failed stage=delete_stats channel_id=%d err=%v", channelKeyAuditPrefix, id, err)
		return fmt.Errorf("failed to delete channel stats: %w", err)
	}

	// 删除渠道
	if err := tx.Delete(&model.Channel{}, id).Error; err != nil {
		tx.Rollback()
		log.Warnf("%s op=channel_delete_failed stage=delete_channel channel_id=%d err=%v", channelKeyAuditPrefix, id, err)
		return fmt.Errorf("failed to delete channel: %w", err)
	}

	if err := tx.Commit().Error; err != nil {
		log.Warnf("%s op=channel_delete_failed stage=commit channel_id=%d err=%v", channelKeyAuditPrefix, id, err)
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// 删除缓存
	channelCache.Del(id)
	for _, k := range ch.Keys {
		if k.ID != 0 {
			channelKeyCache.Del(k.ID)
		}
	}
	StatsChannelDel(id)

	// 刷新受影响的分组缓存
	for _, groupID := range affectedGroupIDs {
		if err := groupRefreshCacheByID(groupID, ctx); err != nil {
			log.Warnf("failed to refresh group cache for group %d: %v", groupID, err)
		}
	}

	log.Infof("%s op=channel_delete_success channel_id=%d deleted_key_count=%d", channelKeyAuditPrefix, id, len(ch.Keys))
	return nil
}

func ChannelLLMList(ctx context.Context) ([]model.LLMChannel, error) {
	models := []model.LLMChannel{}
	for _, channel := range channelCache.GetAll() {
		modelNames := xstrings.SplitTrimCompact(",", channel.Model, channel.CustomModel)
		for _, modelName := range modelNames {
			if modelName == "" {
				continue
			}
			models = append(models, model.LLMChannel{
				Name:        modelName,
				Enabled:     channel.Enabled,
				ChannelID:   channel.ID,
				ChannelName: channel.Name,
			})
		}
	}
	return models, nil
}

func ChannelGet(id int, ctx context.Context) (*model.Channel, error) {
	channel, ok := channelCache.Get(id)
	if !ok {
		return nil, fmt.Errorf("channel not found")
	}
	return &channel, nil
}

func channelRefreshCache(ctx context.Context) error {
	channels := []model.Channel{}
	if err := db.GetDB().WithContext(ctx).
		Preload("Keys").
		Preload("Stats").
		Find(&channels).Error; err != nil {
		log.Warnf("failed to get channels: %v", err)
		return err
	}
	channelKeyCache.Clear()
	channelKeyCacheNeedUpdateLock.Lock()
	channelKeyCacheNeedUpdate = make(map[int]struct{})
	channelKeyCacheNeedUpdateLock.Unlock()
	for _, channel := range channels {
		channelCache.Set(channel.ID, channel)
		for _, k := range channel.Keys {
			if k.ID != 0 {
				channelKeyCache.Set(k.ID, k)
			}
		}
		// [fork] channel key audit
		log.Infof("%s op=cache_refresh channel_id=%d key_count=%d keys=%s",
			channelKeyAuditPrefix, channel.ID, len(channel.Keys), summarizeKeysForAudit(channel.Keys))
	}
	return nil
}

func channelRefreshCacheByID(id int, ctx context.Context) error {
	if old, ok := channelCache.Get(id); ok {
		for _, k := range old.Keys {
			if k.ID != 0 {
				channelKeyCache.Del(k.ID)
			}
		}
	}
	var channel model.Channel
	if err := db.GetDB().WithContext(ctx).
		Preload("Keys").
		Preload("Stats").
		First(&channel, id).Error; err != nil {
		return err
	}
	channelCache.Set(channel.ID, channel)
	for _, k := range channel.Keys {
		if k.ID != 0 {
			channelKeyCache.Set(k.ID, k)
		}
	}
	// [fork] channel key audit
	log.Infof("%s op=cache_refresh_by_id channel_id=%d key_count=%d keys=%s",
		channelKeyAuditPrefix, channel.ID, len(channel.Keys), summarizeKeysForAudit(channel.Keys))
	return nil
}

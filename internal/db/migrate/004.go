package migrate

import (
	"fmt"

	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	// [fork] 修复测活 curl 字段历史列名，避免旧库发送测试时缺列。
	RegisterAfterAutoMigration(Migration{
		Version: 4,
		Up:      ensureGroupChannelCheckCurlColumns,
	})
}

// 004: ensure group channel check curl columns use API JSON field names.
func ensureGroupChannelCheckCurlColumns(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}

	table := "group_channel_check_task_items"
	if !db.Migrator().HasTable(table) {
		return nil
	}

	hasOpenAIColumn := db.Migrator().HasColumn(table, "openai_request_curl")
	hasLegacyOpenAIColumn := db.Migrator().HasColumn(table, "open_ai_request_curl")
	if !hasOpenAIColumn {
		if hasLegacyOpenAIColumn {
			if err := db.Migrator().RenameColumn(table, "open_ai_request_curl", "openai_request_curl"); err != nil {
				return fmt.Errorf("failed to rename group channel check openai curl column: %w", err)
			}
		} else if err := db.Migrator().AddColumn(&model.GroupChannelCheckTaskItem{}, "OpenAIRequestCurl"); err != nil {
			return fmt.Errorf("failed to add group channel check openai curl column: %w", err)
		}
	} else if hasLegacyOpenAIColumn {
		if err := db.Exec("UPDATE group_channel_check_task_items SET openai_request_curl = open_ai_request_curl WHERE (openai_request_curl IS NULL OR openai_request_curl = '') AND open_ai_request_curl IS NOT NULL AND open_ai_request_curl <> ''").Error; err != nil {
			return fmt.Errorf("failed to copy legacy group channel check openai curl column: %w", err)
		}
	}

	if !db.Migrator().HasColumn(table, "anthropic_request_curl") {
		if err := db.Migrator().AddColumn(&model.GroupChannelCheckTaskItem{}, "AnthropicRequestCurl"); err != nil {
			return fmt.Errorf("failed to add group channel check anthropic curl column: %w", err)
		}
	}

	return nil
}

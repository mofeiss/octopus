package migrate

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestEnsureGroupChannelCheckCurlColumnsRenamesLegacyOpenAIColumn(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}

	if err := db.Exec(`
CREATE TABLE group_channel_check_task_items (
	id INTEGER PRIMARY KEY,
	open_ai_request_curl TEXT
);
INSERT INTO group_channel_check_task_items (id, open_ai_request_curl)
VALUES (1, 'curl old');
`).Error; err != nil {
		t.Fatalf("create legacy table failed: %v", err)
	}

	if err := ensureGroupChannelCheckCurlColumns(db); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	if !db.Migrator().HasColumn("group_channel_check_task_items", "openai_request_curl") {
		t.Fatalf("expected openai_request_curl column")
	}
	if !db.Migrator().HasColumn("group_channel_check_task_items", "anthropic_request_curl") {
		t.Fatalf("expected anthropic_request_curl column")
	}

	var curl string
	if err := db.Raw("SELECT openai_request_curl FROM group_channel_check_task_items WHERE id = 1").Scan(&curl).Error; err != nil {
		t.Fatalf("query migrated curl failed: %v", err)
	}
	if curl != "curl old" {
		t.Fatalf("openai_request_curl = %q, want %q", curl, "curl old")
	}
}

func TestEnsureGroupChannelCheckCurlColumnsCopiesWhenBothOpenAIColumnsExist(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}

	if err := db.Exec(`
CREATE TABLE group_channel_check_task_items (
	id INTEGER PRIMARY KEY,
	openai_request_curl TEXT,
	open_ai_request_curl TEXT
);
INSERT INTO group_channel_check_task_items (id, openai_request_curl, open_ai_request_curl)
VALUES (1, '', 'curl legacy');
`).Error; err != nil {
		t.Fatalf("create dual-column table failed: %v", err)
	}

	if err := ensureGroupChannelCheckCurlColumns(db); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	var curl string
	if err := db.Raw("SELECT openai_request_curl FROM group_channel_check_task_items WHERE id = 1").Scan(&curl).Error; err != nil {
		t.Fatalf("query copied curl failed: %v", err)
	}
	if curl != "curl legacy" {
		t.Fatalf("openai_request_curl = %q, want %q", curl, "curl legacy")
	}
}

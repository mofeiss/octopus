package op

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

func setupChannelKeyTestDB(t *testing.T) context.Context {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	if err := db.InitDB("sqlite", dbPath, false); err != nil {
		t.Fatalf("init db failed: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	if err := InitCache(); err != nil {
		t.Fatalf("init cache failed: %v", err)
	}

	return context.Background()
}

func TestChannelKeySaveDBDoesNotOverwriteEditedKeyFields(t *testing.T) {
	ctx := setupChannelKeyTestDB(t)

	ch := model.Channel{
		Name:    "channel-a",
		Type:    0,
		Enabled: true,
		Keys: []model.ChannelKey{
			{Enabled: true, ChannelKey: "old-key", Remark: "old-remark"},
		},
	}
	if err := ChannelCreate(&ch, ctx); err != nil {
		t.Fatalf("create channel failed: %v", err)
	}

	created, err := ChannelGet(ch.ID, ctx)
	if err != nil {
		t.Fatalf("get created channel failed: %v", err)
	}
	if len(created.Keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(created.Keys))
	}
	oldKey := created.Keys[0]

	newKey := "new-key"
	newRemark := "new-remark"
	_, err = ChannelUpdate(&model.ChannelUpdateRequest{
		ID: created.ID,
		KeysToUpdate: []model.ChannelKeyUpdateRequest{
			{ID: oldKey.ID, ChannelKey: &newKey, Remark: &newRemark},
		},
	}, ctx)
	if err != nil {
		t.Fatalf("channel update failed: %v", err)
	}

	stale := oldKey
	stale.StatusCode = 401
	stale.LastUseTimeStamp = 12345
	stale.TotalCost = 9.9
	if err := ChannelKeyUpdate(stale); err != nil {
		t.Fatalf("channel key update failed: %v", err)
	}
	if err := ChannelKeySaveDB(ctx); err != nil {
		t.Fatalf("channel key save db failed: %v", err)
	}
	if err := InitCache(); err != nil {
		t.Fatalf("re-init cache failed: %v", err)
	}

	final, err := ChannelGet(created.ID, ctx)
	if err != nil {
		t.Fatalf("get final channel failed: %v", err)
	}
	if len(final.Keys) != 1 {
		t.Fatalf("expected 1 key after save, got %d", len(final.Keys))
	}
	if final.Keys[0].ChannelKey != newKey {
		t.Fatalf("channel_key reverted unexpectedly, got %q want %q", final.Keys[0].ChannelKey, newKey)
	}
	if final.Keys[0].Remark != newRemark {
		t.Fatalf("remark reverted unexpectedly, got %q want %q", final.Keys[0].Remark, newRemark)
	}
	if final.Keys[0].StatusCode != stale.StatusCode {
		t.Fatalf("status_code not persisted, got %d want %d", final.Keys[0].StatusCode, stale.StatusCode)
	}
}

func TestDeletedChannelKeyCannotBeResurrectedByRuntimeUpdate(t *testing.T) {
	ctx := setupChannelKeyTestDB(t)

	ch := model.Channel{
		Name:    "channel-b",
		Type:    0,
		Enabled: true,
		Keys: []model.ChannelKey{
			{Enabled: true, ChannelKey: "to-delete", Remark: "remark"},
		},
	}
	if err := ChannelCreate(&ch, ctx); err != nil {
		t.Fatalf("create channel failed: %v", err)
	}

	created, err := ChannelGet(ch.ID, ctx)
	if err != nil {
		t.Fatalf("get created channel failed: %v", err)
	}
	if len(created.Keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(created.Keys))
	}
	deletedKey := created.Keys[0]

	_, err = ChannelUpdate(&model.ChannelUpdateRequest{
		ID:           created.ID,
		KeysToDelete: []int{deletedKey.ID},
	}, ctx)
	if err != nil {
		t.Fatalf("delete key via channel update failed: %v", err)
	}

	deletedKey.StatusCode = 500
	if err := ChannelKeyUpdate(deletedKey); err == nil {
		t.Fatalf("expected stale key update to fail for deleted key")
	}

	if err := ChannelKeySaveDB(ctx); err != nil {
		t.Fatalf("channel key save db failed: %v", err)
	}
	if err := InitCache(); err != nil {
		t.Fatalf("re-init cache failed: %v", err)
	}

	final, err := ChannelGet(created.ID, ctx)
	if err != nil {
		t.Fatalf("get final channel failed: %v", err)
	}
	if len(final.Keys) != 0 {
		t.Fatalf("deleted key resurrected unexpectedly, key_count=%d", len(final.Keys))
	}
}

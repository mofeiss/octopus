package op

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

func setupGroupAliasCache(t *testing.T, groups ...model.Group) {
	t.Helper()

	groupCache.Clear()
	groupMap.Clear()
	t.Cleanup(func() {
		groupCache.Clear()
		groupMap.Clear()
	})

	for _, group := range groups {
		groupCache.Set(group.ID, group)
		groupMapSetWithAliases(group)
	}
}

func TestGroupRouteAliasMatchUsesThreeStages(t *testing.T) {
	setupGroupAliasCache(t,
		model.Group{ID: 1, Name: "upper", SortOrder: 1, RouteAliases: "OPUS"},
		model.Group{ID: 2, Name: "lower", SortOrder: 2, RouteAliases: "opus"},
		model.Group{ID: 3, Name: "contains", SortOrder: 3, RouteAliases: "haiku"},
	)

	tests := []struct {
		name      string
		modelName string
		wantID    int
	}{
		{
			name:      "case-sensitive exact wins before case-insensitive order",
			modelName: "opus",
			wantID:    2,
		},
		{
			name:      "case-insensitive exact scans by group order",
			modelName: "OpUs",
			wantID:    1,
		},
		{
			name:      "case-insensitive substring matches alias inside full model",
			modelName: "Claude-Haiku-4-5",
			wantID:    3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := groupMatchByRouteAliases(tt.modelName)
			if !ok {
				t.Fatalf("expected route alias match for %q", tt.modelName)
			}
			if got.ID != tt.wantID {
				t.Fatalf("matched group id = %d, want %d", got.ID, tt.wantID)
			}
		})
	}
}

func TestGroupRouteAliasMatchDoesNotSubstringMatchGroupName(t *testing.T) {
	setupGroupAliasCache(t,
		model.Group{ID: 1, Name: "opus", SortOrder: 1},
		model.Group{ID: 2, Name: "fallback", SortOrder: 2, RouteAliases: "sonnet"},
	)

	if got, ok := groupMatchByRouteAliases("Claude-Opus-4-6"); ok {
		t.Fatalf("unexpected group-name substring match: %+v", got)
	}
	got, err := GroupGetMap("opus", nil)
	if err != nil {
		t.Fatalf("expected exact group name fallback to still work: %v", err)
	}
	if got.ID != 1 {
		t.Fatalf("exact group name matched id = %d, want 1", got.ID)
	}
}

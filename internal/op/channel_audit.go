package op

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/bestruirui/octopus/internal/model"
)

const channelKeyAuditPrefix = "[audit][channel_key]"

func maskKeyForAudit(key string) string {
	if key == "" {
		return "<empty>"
	}
	// [fork] user requested audit logs to keep full key for easier incident tracing.
	return key
}

func hashKeyForAudit(key string) string {
	if key == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(key))
	hexStr := hex.EncodeToString(sum[:])
	if len(hexStr) > 12 {
		return hexStr[:12]
	}
	return hexStr
}

func summarizeSingleKeyForAudit(k model.ChannelKey) string {
	return fmt.Sprintf("id=%d,cid=%d,en=%t,sc=%d,ts=%d,cost=%.6f,key=%s,key_hash=%s,remark=%q",
		k.ID, k.ChannelID, k.Enabled, k.StatusCode, k.LastUseTimeStamp, k.TotalCost, maskKeyForAudit(k.ChannelKey), hashKeyForAudit(k.ChannelKey), k.Remark)
}

func summarizeKeysForAudit(keys []model.ChannelKey) string {
	if len(keys) == 0 {
		return "[]"
	}
	cp := make([]model.ChannelKey, len(keys))
	copy(cp, keys)
	sort.Slice(cp, func(i, j int) bool {
		return cp[i].ID < cp[j].ID
	})
	items := make([]string, 0, len(cp))
	for _, k := range cp {
		items = append(items, "{"+summarizeSingleKeyForAudit(k)+"}")
	}
	return "[" + strings.Join(items, ", ") + "]"
}

func summarizeChannelUpdateReqForAudit(req *model.ChannelUpdateRequest, oldByID map[int]model.ChannelKey) string {
	if req == nil {
		return "<nil>"
	}

	addItems := make([]string, 0, len(req.KeysToAdd))
	for _, ka := range req.KeysToAdd {
		addItems = append(addItems, fmt.Sprintf("{en=%t,key=%s,key_hash=%s,remark=%q}",
			ka.Enabled, maskKeyForAudit(ka.ChannelKey), hashKeyForAudit(ka.ChannelKey), ka.Remark))
	}

	updateItems := make([]string, 0, len(req.KeysToUpdate))
	for _, ku := range req.KeysToUpdate {
		old, ok := oldByID[ku.ID]
		oldInfo := "<missing>"
		if ok {
			oldInfo = summarizeSingleKeyForAudit(old)
		}

		changeParts := make([]string, 0, 3)
		if ku.Enabled != nil {
			changeParts = append(changeParts, fmt.Sprintf("enabled=>%t", *ku.Enabled))
		}
		if ku.ChannelKey != nil {
			changeParts = append(changeParts, fmt.Sprintf("key=>%s,key_hash=>%s", maskKeyForAudit(*ku.ChannelKey), hashKeyForAudit(*ku.ChannelKey)))
		}
		if ku.Remark != nil {
			changeParts = append(changeParts, fmt.Sprintf("remark=>%q", *ku.Remark))
		}
		updateItems = append(updateItems, fmt.Sprintf("{id=%d,old=%s,patch=[%s]}", ku.ID, oldInfo, strings.Join(changeParts, "; ")))
	}

	deleteItems := make([]string, 0, len(req.KeysToDelete))
	for _, id := range req.KeysToDelete {
		old, ok := oldByID[id]
		if !ok {
			deleteItems = append(deleteItems, fmt.Sprintf("{id=%d,old=<missing>}", id))
			continue
		}
		deleteItems = append(deleteItems, fmt.Sprintf("{id=%d,old=%s}", id, summarizeSingleKeyForAudit(old)))
	}

	return fmt.Sprintf("add=%d %s; update=%d %s; delete=%d %s",
		len(req.KeysToAdd), limitAuditItems(addItems, 10),
		len(req.KeysToUpdate), limitAuditItems(updateItems, 10),
		len(req.KeysToDelete), limitAuditItems(deleteItems, 10))
}

func limitAuditItems(items []string, max int) string {
	if len(items) == 0 {
		return "[]"
	}
	if max <= 0 || len(items) <= max {
		return "[" + strings.Join(items, ", ") + "]"
	}
	return "[" + strings.Join(items[:max], ", ") + fmt.Sprintf(", ...(+%d)]", len(items)-max)
}

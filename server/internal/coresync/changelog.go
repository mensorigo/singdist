package coresync

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"singbox-ui/server/internal/store"
)

// changeActionLabel maps a change action to its Chinese label for the
// config-changes.log file.
func changeActionLabel(a changeAction) string {
	switch a {
	case changeUpdated:
		return "更新"
	case changeAdded:
		return "新增"
	case changeDeleted:
		return "删除"
	case changePassthrough:
		return "透传"
	case changeDroppedIllegal:
		return "丢弃非法"
	case changeReverted:
		return "恢复面板"
	case changeRestoreNote:
		return "提示"
	default:
		return string(a)
	}
}

// writeChangeRows persists change rows to the DB and appends them to
// <dataDir>/logs/config-changes.log (beside the core log file).
// Best-effort: a write failure never fails the sync itself.
// writeChangeRows persists change rows to the DB and appends them to
// <dataDir>/logs/config-changes.log (beside the core log file).
// Best-effort: a write failure never fails the sync itself.
func (s *Service) writeChangeRows(ctx context.Context, syncID int64, rows []change) {
	if len(rows) == 0 {
		return
	}
	loc := time.Local
	if cfg, err := s.settings.Get(ctx); err == nil {
		if l, err := time.LoadLocation(cfg.Timezone); err == nil {
			loc = l
		}
	}
	var file *os.File
	if s.logPath != "" {
		path := filepath.Join(filepath.Dir(s.logPath), "config-changes.log")
		if f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			file = f
			defer f.Close()
		} else {
			s.warn("open config-changes.log", "error", err)
		}
	}
	stamp := time.Now().In(loc).Format("2006-01-02 15:04:05")
	for _, ch := range rows {
		if _, err := s.st.AddConfigChange(ctx, store.ConfigChangeRow{
			At:        nowUnix(),
			SyncLogID: syncID,
			Action:    string(ch.action),
			Item:      ch.item,
			OldValue:  ch.oldVal,
			NewValue:  ch.newVal,
			Detail:    ch.detail,
		}); err != nil {
			s.warn("write config change row", "item", ch.item, "error", err)
		}
		if file != nil {
			line := stamp + " " + changeActionLabel(ch.action) + " " + ch.item
			if ch.oldVal != "" || ch.newVal != "" {
				line += " " + ch.oldVal + "→" + ch.newVal
			}
			if ch.detail != "" {
				line += " (" + ch.detail + ")"
			}
			if _, err := file.WriteString(line + "\n"); err != nil {
				s.warn("append config-changes.log", "error", err)
			}
		}
	}
}

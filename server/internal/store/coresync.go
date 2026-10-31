package store

import (
	"context"
	"fmt"
	"time"
)

// SyncLogRow is one sync-engine attempt. (An archived-versions table
// with rollback used to live alongside this log; it was removed
// 2026-10-24. The table itself still exists in old databases but
// nothing reads or writes it anymore.)
type SyncLogRow struct {
	ID     int64  `json:"id"`
	At     int64  `json:"at"`
	Action string `json:"action"` // sync | auto-sync
	Result string `json:"result"` // applied | no_change | check_failed | restart_failed | error
	Hash   string `json:"hash"`
	Detail string `json:"detail"`
}

func (s *Store) AddSyncLog(ctx context.Context, r SyncLogRow) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO sync_logs (at, action, result, hash, detail) VALUES (?, ?, ?, ?, ?)`,
		r.At, r.Action, r.Result, r.Hash, r.Detail)
	if err != nil {
		return 0, fmt.Errorf("store: add sync log: %w", err)
	}
	return res.LastInsertId()
}

// SyncLogRetention mirrors core_logs (90 days) but in Unix seconds:
// sync `at` is seconds (coresync nowUnix), while core `ts` is milliseconds.
const SyncLogRetention = 90 * 24 * time.Hour

// PruneSyncLogs deletes sync rows and their change rows older than the
// retention window. Each table is pruned by its own `at`; change rows
// are NOT cascade-deleted (sync_log_id has no FK), so both tables are
// deleted explicitly. The deletes are idempotent, so a crash between
// them is completed by the next prune run.
func (s *Store) PruneSyncLogs(ctx context.Context, beforeTs int64) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM config_change_logs WHERE at < ?`, beforeTs); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM sync_logs WHERE at < ?`, beforeTs)
	return err
}

func (s *Store) ListSyncLogs(ctx context.Context, limit int) ([]SyncLogRow, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, at, action, result, hash, detail FROM sync_logs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list sync logs: %w", err)
	}
	defer rows.Close()
	out := []SyncLogRow{}
	for rows.Next() {
		var r SyncLogRow
		if err := rows.Scan(&r.ID, &r.At, &r.Action, &r.Result, &r.Hash, &r.Detail); err != nil {
			return nil, fmt.Errorf("store: scan sync log: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// NOTE: the config_versions helpers (add/list/get/delete/prune/note)
// were deleted with the versions feature. The table and its migration
// stay in place so old databases keep opening; nothing references them.

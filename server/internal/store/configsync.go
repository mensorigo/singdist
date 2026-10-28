package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ConfigChangeRow is one classified external (file-origin) change: an
// import into panel state, a preserved passthrough fragment, a dropped
// illegal value, a reverted file edit, or a restore note. Secrets
// (uuid, password, private keys) are never stored in old/new values.
type ConfigChangeRow struct {
	ID        int64  `json:"id"`
	At        int64  `json:"at"`
	SyncLogID int64  `json:"syncLogId"`
	Action    string `json:"action"` // updated | added | deleted | passthrough | dropped_illegal | reverted | restore_note
	Item      string `json:"item"`
	OldValue  string `json:"oldValue"`
	NewValue  string `json:"newValue"`
	Detail    string `json:"detail"`
}

func (s *Store) AddConfigChange(ctx context.Context, r ConfigChangeRow) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO config_change_logs (at, sync_log_id, action, item, old_value, new_value, detail) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		r.At, r.SyncLogID, r.Action, r.Item, r.OldValue, r.NewValue, r.Detail)
	if err != nil {
		return 0, fmt.Errorf("store: add config change: %w", err)
	}
	return res.LastInsertId()
}

type ConfigChangeFilter struct {
	Keyword string
	Before  int64
	Limit   int
}

func (s *Store) ListConfigChanges(ctx context.Context, f ConfigChangeFilter) ([]ConfigChangeRow, error) {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	var args []any
	var conds []string
	if f.Before > 0 {
		conds = append(conds, "id < ?")
		args = append(args, f.Before)
	}
	if kw := strings.TrimSpace(f.Keyword); kw != "" {
		like := "%" + kw + "%"
		conds = append(conds, "(item LIKE ? OR action LIKE ? OR old_value LIKE ? OR new_value LIKE ? OR detail LIKE ?)")
		args = append(args, like, like, like, like, like)
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}
	args = append(args, f.Limit)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, at, sync_log_id, action, item, old_value, new_value, detail FROM config_change_logs `+where+` ORDER BY id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list config changes: %w", err)
	}
	defer rows.Close()
	out := []ConfigChangeRow{}
	for rows.Next() {
		var r ConfigChangeRow
		if err := rows.Scan(&r.ID, &r.At, &r.SyncLogID, &r.Action, &r.Item, &r.OldValue, &r.NewValue, &r.Detail); err != nil {
			return nil, fmt.Errorf("store: scan config change: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SyncState is the classifier baseline: the config bytes as last
// written (or observed) by the sync engine, plus the passthrough tag
// set from that same sync. BlockedHash is the merged-config hash of
// an input that failed at runtime and was rolled back; auto-sync
// skips that input until it changes.
type SyncState struct {
	LastDoc         string
	LastHash        string
	PassthroughTags []string
	BlockedHash     string
	UpdatedAt       int64
}

// CountPendingAudits returns how many audit rows are still waiting
// to be applied by a sync (applied_hash = ”). Used by the first-run
// ambiguity guard: pending rows plus a changed file with no baseline
// means the sources cannot be separated.
func (s *Store) CountPendingAudits(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_logs WHERE applied_hash = ''`).Scan(&n)
	return n, err
}

// LatestPendingAuditAt returns the newest audit row still waiting to be
// applied by a sync (applied_hash = ”), or 0 when none exists. The
// auto-sync checker uses it as the last-change time for the
// interval=0 debounce: a change only syncs once no new change has
// landed for a few seconds.
func (s *Store) LatestPendingAuditAt(ctx context.Context) (int64, error) {
	var at int64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(at), 0) FROM audit_logs WHERE applied_hash = ''`).Scan(&at)
	return at, err
}

func (s *Store) GetSyncState(ctx context.Context) (SyncState, error) {
	var st SyncState
	var tagsJSON string
	err := s.db.QueryRowContext(ctx,
		`SELECT last_doc, last_hash, passthrough_tags, blocked_hash, updated_at FROM sync_state WHERE id = 1`).
		Scan(&st.LastDoc, &st.LastHash, &tagsJSON, &st.BlockedHash, &st.UpdatedAt)
	if err != nil {
		// No row yet: first run after upgrade. Callers treat an empty
		// LastDoc as "no baseline" and bootstrap conservatively.
		return SyncState{}, nil
	}
	if tagsJSON != "" {
		// Best-effort decode; a corrupt tag set degrades to "none"
		// rather than failing the sync.
		_ = json.Unmarshal([]byte(tagsJSON), &st.PassthroughTags)
	}
	return st, nil
}

func (s *Store) SetSyncState(ctx context.Context, st SyncState) error {
	tagsJSON, _ := json.Marshal(st.PassthroughTags)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sync_state (id, last_doc, last_hash, passthrough_tags, updated_at) VALUES (1, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET last_doc = excluded.last_doc, last_hash = excluded.last_hash, passthrough_tags = excluded.passthrough_tags, updated_at = excluded.updated_at`,
		st.LastDoc, st.LastHash, tagsJSON, st.UpdatedAt)
	if err != nil {
		return fmt.Errorf("store: set sync state: %w", err)
	}
	return nil
}

// SetBlockedHash records (or clears, with "") the merged-config hash
// of an input that failed at runtime and was rolled back. It touches
// only the block column so a block update never clobbers the baseline.
func (s *Store) SetBlockedHash(ctx context.Context, hash string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sync_state (id, blocked_hash) VALUES (1, ?)
		 ON CONFLICT(id) DO UPDATE SET blocked_hash = excluded.blocked_hash`,
		hash)
	if err != nil {
		return fmt.Errorf("store: set blocked hash: %w", err)
	}
	return nil
}

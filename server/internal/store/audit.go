package store

import (
	"context"
	"strings"
	"time"
)

// AuditRow is one panel-operation audit entry: a mutating API call and
// its outcome. result is "success" or "failed"; detail carries the error
// code on failure, empty on success.
type AuditRow struct {
	ID     int64  `json:"id"`
	At     int64  `json:"at"`
	Action string `json:"action"`
	Target string `json:"target"`
	Result string `json:"result"`
	Detail string `json:"detail"`
}

// auditRetention bounds the audit history (90 days, pruned on write).
const auditRetention = 90 * 24 * time.Hour

// AddAuditLog appends a row and prunes entries older than the retention
// window. Best-effort pruning: a prune failure never fails the write.
func (s *Store) AddAuditLog(ctx context.Context, r AuditRow) error {
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_logs (at, action, target, result, detail) VALUES (?, ?, ?, ?, ?)`,
		r.At, r.Action, r.Target, r.Result, r.Detail); err != nil {
		return err
	}
	_, _ = s.db.ExecContext(ctx,
		`DELETE FROM audit_logs WHERE at < ?`,
		time.Now().Add(-auditRetention).Unix())
	return nil
}

// AuditFilter scopes ListAuditLogs. Result is "" (all), "success" or
// "failed". Keyword matches action/target/detail (case-insensitive
// substring). BeforeID pages backwards (id < BeforeID); 0 means latest.
type AuditFilter struct {
	Result  string
	Keyword string
	Before  int64
	Limit   int
}

// ListAuditLogs returns audit rows newest first.
func (s *Store) ListAuditLogs(ctx context.Context, f AuditFilter) ([]AuditRow, error) {
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	var args []any
	var where []string
	if f.Result == "success" || f.Result == "failed" {
		where = append(where, "result = ?")
		args = append(args, f.Result)
	}
	if kw := strings.TrimSpace(f.Keyword); kw != "" {
		where = append(where, "(action LIKE ? OR target LIKE ? OR detail LIKE ?)")
		like := "%" + kw + "%"
		args = append(args, like, like, like)
	}
	if f.Before > 0 {
		where = append(where, "id < ?")
		args = append(args, f.Before)
	}
	query := `SELECT id, at, action, target, result, detail FROM audit_logs`
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditRow{}
	for rows.Next() {
		var r AuditRow
		if err := rows.Scan(&r.ID, &r.At, &r.Action, &r.Target, &r.Result, &r.Detail); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

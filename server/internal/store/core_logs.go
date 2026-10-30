package store

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"strings"
	"time"
)

// CoreLogRow is one ingested core-journal record: a core system line
// (kind "core") or one grouped client connection (kind "connection").
// Ts is unix milliseconds, matching the Logs page's Entry timestamps.
type CoreLogRow struct {
	ID          int64
	Ts          int64
	Kind        string
	Level       string
	Message     string
	Source      string
	Destination string
	Tag         string
	User        string
}

// CoreLogRetention bounds the ingested history (90 days, pruned by the
// follower), mirroring the audit retention.
const CoreLogRetention = 90 * 24 * time.Hour

// InsertCoreLogsWithFilePos appends rows and records the follower's
// file position in the same transaction, so a crash can never leave
// rows committed without their position (which would re-insert them
// on resume) or vice versa. An empty batch still advances the
// position: bytes that only fed a not-yet-flushed connection group
// are consumed all the same.
func (s *Store) InsertCoreLogsWithFilePos(ctx context.Context, rows []CoreLogRow, path string, inode, offset int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if len(rows) > 0 {
		stmt, err := tx.PrepareContext(ctx,
			`INSERT INTO core_logs (ts, kind, level, message, source, destination, tag, user)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, r := range rows {
			if _, err := stmt.ExecContext(ctx, r.Ts, r.Kind, r.Level, r.Message,
				r.Source, r.Destination, r.Tag, r.User); err != nil {
				return err
			}
		}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO log_file_state (id, path, inode, offset, updated_at) VALUES (1, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET path = excluded.path, inode = excluded.inode, offset = excluded.offset, updated_at = excluded.updated_at`,
		path, inode, offset, time.Now().Unix()); err != nil {
		return err
	}
	return tx.Commit()
}

// CoreLogFilter scopes ListCoreLogs. Keyword matches message, source,
// destination, tag and user (case-insensitive substring). BeforeID
// pages backwards (id < BeforeID); 0 means latest.
type CoreLogFilter struct {
	Kind    string
	Keyword string
	Level   string
	Before  int64
	Limit   int
}

// ListCoreLogs returns ingested rows newest first (insertion order,
// which follows journal order).
func (s *Store) ListCoreLogs(ctx context.Context, f CoreLogFilter) ([]CoreLogRow, error) {
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	var args []any
	var where []string
	if f.Kind != "" {
		where = append(where, "kind = ?")
		args = append(args, f.Kind)
	}
	if f.Level != "" {
		where = append(where, "level = ?")
		args = append(args, f.Level)
	}
	if kw := strings.TrimSpace(f.Keyword); kw != "" {
		esc := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(kw)
		like := "%" + esc + "%"
		where = append(where, `(message LIKE ? ESCAPE '\' OR source LIKE ? ESCAPE '\' OR destination LIKE ? ESCAPE '\' OR tag LIKE ? ESCAPE '\' OR user LIKE ? ESCAPE '\')`)
		args = append(args, like, like, like, like, like)
	}
	if f.Before > 0 {
		where = append(where, "id < ?")
		args = append(args, f.Before)
	}
	query := `SELECT id, ts, kind, level, message, source, destination, tag, user FROM core_logs`
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
	out := []CoreLogRow{}
	for rows.Next() {
		var r CoreLogRow
		if err := rows.Scan(&r.ID, &r.Ts, &r.Kind, &r.Level, &r.Message,
			&r.Source, &r.Destination, &r.Tag, &r.User); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// PruneCoreLogs deletes ingested rows older than the retention window.
func (s *Store) PruneCoreLogs(ctx context.Context, beforeTs int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM core_logs WHERE ts < ?`, beforeTs)
	return err
}

// GetFileState returns the follower's last durably consumed file
// position, if any. The follower resumes there, so a restart re-reads
// only uncommitted bytes instead of the whole file.
func (s *Store) GetFileState(ctx context.Context) (path string, inode, offset int64, found bool, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT path, inode, offset FROM log_file_state WHERE id = 1`).Scan(&path, &inode, &offset)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, 0, false, nil
	}
	if err != nil {
		return "", 0, 0, false, err
	}
	return path, inode, offset, true, nil
}

// MinCoreLogsTs returns the oldest ingested timestamp, if any. The
// size-cap prune deletes day-sized chunks from the oldest end.
func (s *Store) MinCoreLogsTs(ctx context.Context) (int64, bool, error) {
	var ts *int64
	err := s.db.QueryRowContext(ctx, `SELECT MIN(ts) FROM core_logs`).Scan(&ts)
	if err != nil {
		return 0, false, err
	}
	if ts == nil {
		return 0, false, nil
	}
	return *ts, true, nil
}

// CoreLogsUsedBytes estimates the core_logs data size from page
// accounting (total pages minus freelist, so deleted-but-unvacuumed
// rows do not inflate it). Used by the size-cap prune, not by reads.
func (s *Store) CoreLogsUsedBytes(ctx context.Context) (int64, error) {
	var pages, freelist, size int64
	if err := s.db.QueryRowContext(ctx, `PRAGMA page_count`).Scan(&pages); err != nil {
		return 0, err
	}
	if err := s.db.QueryRowContext(ctx, `PRAGMA freelist_count`).Scan(&freelist); err != nil {
		return 0, err
	}
	if err := s.db.QueryRowContext(ctx, `PRAGMA page_size`).Scan(&size); err != nil {
		return 0, err
	}
	used := (pages - freelist) * size
	if used < 0 {
		used = 0
	}
	return used, nil
}

// RecentConnections returns, per client source address (IP:port),
// the latest connection record at or after sinceMs: inbound tag
// and core username. The traffic collector matches Clash
// connections to these records by source address.
func (s *Store) RecentConnections(ctx context.Context, sinceMs int64) (map[string]struct {
	Tag  string
	User string
}, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT source, tag, user, ts FROM core_logs WHERE kind = 'connection' AND ts >= ? ORDER BY ts DESC`, sinceMs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]struct {
		Tag  string
		User string
	}{}
	for rows.Next() {
		var source, tag, user string
		var ts int64
		if err := rows.Scan(&source, &tag, &user, &ts); err != nil {
			return nil, err
		}
		if source == "" || tag == "" {
			continue
		}
		if _, ok := out[source]; !ok {
			out[source] = struct {
				Tag  string
				User string
			}{Tag: tag, User: user}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// OnlineIPs returns, per log user name, the set of distinct source IPs
// with a connection record at or after sinceMs. Source is "ip:port"
// (Go Addr.String form, so IPv6 arrives bracketed); the port is
// stripped, and an unparseable source counts as-is rather than being
// dropped. Rows with an empty user (e.g. shadowsocks, which has no
// per-user field) cannot be attributed and are skipped.
func (s *Store) OnlineIPs(ctx context.Context, sinceMs int64) (map[string]map[string]struct{}, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT user, source FROM core_logs WHERE kind = 'connection' AND ts >= ?`, sinceMs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]map[string]struct{}{}
	for rows.Next() {
		var user, source string
		if err := rows.Scan(&user, &source); err != nil {
			return nil, err
		}
		if user == "" || source == "" {
			continue
		}
		ip := source
		if host, _, err := net.SplitHostPort(source); err == nil {
			ip = host
		}
		m := out[user]
		if m == nil {
			m = map[string]struct{}{}
			out[user] = m
		}
		m[ip] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

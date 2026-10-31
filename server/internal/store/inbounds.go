package store

import (
	"context"
	"fmt"
)

// InboundRow is one stored inbound (desired state). Params is the raw
// protocol-specific JSON object; interpreting it is the inbounds
// package's job, the store only persists it.
type InboundRow struct {
	ID        int64
	Name      string
	Protocol  string
	Port      int
	Params    string
	Enabled   bool
	RunLocal  bool // false = the local machine skips this inbound (nodes only)
	CreatedAt int64
	UpdatedAt int64
}

func (s *Store) ListInbounds(ctx context.Context) ([]InboundRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, protocol, port, params, enabled, run_local, created_at, updated_at
		 FROM inbounds ORDER BY port`)
	if err != nil {
		return nil, fmt.Errorf("store: list inbounds: %w", err)
	}
	defer rows.Close()
	out := []InboundRow{}
	for rows.Next() {
		var r InboundRow
		var enabled, runLocal int64
		if err := rows.Scan(&r.ID, &r.Name, &r.Protocol, &r.Port, &r.Params, &enabled, &runLocal, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, fmt.Errorf("store: scan inbound: %w", err)
		}
		r.Enabled = enabled != 0
		r.RunLocal = runLocal != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) CountInbounds(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM inbounds`).Scan(&n)
	return n, err
}

func (s *Store) CreateInbound(ctx context.Context, name, protocol string, port int, params string, runLocal bool, now int64) (int64, error) {
	rl := 0
	if runLocal {
		rl = 1
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO inbounds (name, protocol, port, params, run_local, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`, name, protocol, port, params, rl, now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateInbound(ctx context.Context, id int64, name string, port int, params string, runLocal bool, now int64) error {
	rl := 0
	if runLocal {
		rl = 1
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE inbounds SET name = ?, port = ?, params = ?, run_local = ?, updated_at = ? WHERE id = ?`,
		name, port, params, rl, now, id)
	return err
}

func (s *Store) DeleteInbound(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM inbounds WHERE id = ?`, id)
	return err
}

// SetInboundEnabled flips the enabled flag; returns false when the id
// does not exist.
func (s *Store) SetInboundEnabled(ctx context.Context, id int64, enabled bool, now int64) (bool, error) {
	v := 0
	if enabled {
		v = 1
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE inbounds SET enabled = ?, updated_at = ? WHERE id = ?`, v, now, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

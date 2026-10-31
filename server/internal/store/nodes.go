package store

import (
	"context"
	"encoding/json"
	"fmt"
)

// NodeRow is one remote agent node (a central push target). The token is
// stored in cleartext, like subscription tokens, because it must be
// presented on every push. It is never returned by the API.
// Paused: a paused node is excluded from subscription rendering and
// pushed the empty payload, so its panel inbounds stop on the node
// itself; nothing is deleted anywhere (record, assignments and
// inbound definitions all stay); enabling pushes the normal payload
// again and restores it.
type NodeRow struct {
	ID               int64
	Name             string
	Address          string
	Token            string
	PublicHost       string
	TLSMode          string
	UsageRatio       float64
	Paused           bool
	LastPushHash     string
	LastPushAt       int64
	LastPushResult   string
	LastError        string
	LastOnlineAt     int64
	LastInboundCount int
	LastVersion      string
	NextRetryAt      int64
	DesiredPushHash  string
	AgentStatus      string
	AgentStatusAt    int64
	CreatedAt        int64
	UpdatedAt        int64
}

// OnlineWithin is the grace window (seconds) during which a node that
// was last seen counts as online for subscription rendering.
const OnlineWithin = 300

// Online reports whether the node was seen within the grace window.
func (r *NodeRow) Online(now int64) bool {
	return r.LastOnlineAt > 0 && now-r.LastOnlineAt <= OnlineWithin
}

func scanNodeRow(rows interface {
	Scan(...any) error
}, r *NodeRow) error {
	var paused int64
	if err := rows.Scan(&r.ID, &r.Name, &r.Address, &r.Token, &r.PublicHost,
		&r.TLSMode, &r.UsageRatio, &paused, &r.LastPushHash, &r.LastPushAt,
		&r.LastPushResult, &r.LastError, &r.LastOnlineAt,
		&r.LastInboundCount, &r.LastVersion, &r.NextRetryAt,
		&r.DesiredPushHash, &r.AgentStatus, &r.AgentStatusAt,
		&r.CreatedAt, &r.UpdatedAt); err != nil {
		return err
	}
	r.Paused = paused != 0
	return nil
}

const nodeColumns = `id, name, address, token, public_host, tls_mode,
	usage_ratio, paused, last_push_hash, last_push_at, last_push_result,
	last_error, last_online_at, last_inbound_count, last_version,
	next_retry_at, desired_push_hash, agent_status, agent_status_at,
	created_at, updated_at`

func (s *Store) ListNodes(ctx context.Context) ([]NodeRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+nodeColumns+` FROM nodes ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("store: list nodes: %w", err)
	}
	defer rows.Close()
	out := []NodeRow{}
	for rows.Next() {
		var r NodeRow
		if err := scanNodeRow(rows, &r); err != nil {
			return nil, fmt.Errorf("store: scan node: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) GetNode(ctx context.Context, id int64) (*NodeRow, error) {
	var r NodeRow
	err := scanNodeRow(s.db.QueryRowContext(ctx,
		`SELECT `+nodeColumns+` FROM nodes WHERE id = ?`, id), &r)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *Store) CreateNode(ctx context.Context, r NodeRow) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO nodes (name, address, token, public_host, tls_mode,
		 usage_ratio, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		r.Name, r.Address, r.Token, r.PublicHost, r.TLSMode,
		r.UsageRatio, r.CreatedAt, r.UpdatedAt)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateNode updates the mutable node fields. Token/address changes are
// handled by the caller resetting push state (see ResetNodePushState):
// a node that moved to a different agent must be pushed fresh, because
// the stored last_push_hash belongs to the old agent. Pausing never
// resets push state: the hash is kept, so the poller pushes the empty
// payload (its hash differs from the old one) and enabling pushes the
// normal payload again only when it differs from the paused hash.
func (s *Store) UpdateNode(ctx context.Context, r NodeRow) error {
	paused := 0
	if r.Paused {
		paused = 1
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE nodes SET name = ?, address = ?, token = ?, public_host = ?,
		 tls_mode = ?, usage_ratio = ?, paused = ?, updated_at = ? WHERE id = ?`,
		r.Name, r.Address, r.Token, r.PublicHost, r.TLSMode,
		r.UsageRatio, paused, r.UpdatedAt, r.ID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ClearNodeNextRetry clears a pending push backoff so the next poll
// (5s) pushes promptly instead of waiting out an old failure window.
// Used when the pause flag flips, so a pause (stop) push and an
// enable (restore) push are not delayed by a stale backoff.
func (s *Store) ClearNodeNextRetry(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE nodes SET next_retry_at = 0 WHERE id = ?`, id)
	return err
}

// ResetNodePushState clears the push state so the poller pushes fresh
// (used when the node address or token changes: the stored hash belongs
// to the old agent and must not suppress a push to the new one). The
// desired hash and the agent status snapshot are cleared too: both
// belong to the old agent and must not make the new one look synced.
func (s *Store) ResetNodePushState(ctx context.Context, id, now int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE nodes SET last_push_hash = '', last_push_result = '',
		 last_error = '', next_retry_at = 0, desired_push_hash = '',
		 agent_status = '', agent_status_at = 0, updated_at = ? WHERE id = ?`,
		now, id)
	return err
}

// RecordDesiredPushHash stores the payload hash the pusher last built
// for a node, so the card can compare it with the last pushed hash
// without rebuilding the payload (which needs the cert manager).
func (s *Store) RecordDesiredPushHash(ctx context.Context, id int64, hash string, now int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE nodes SET desired_push_hash = ?, updated_at = ? WHERE id = ?`,
		hash, now, id)
	return err
}

// RecordNodeAgentStatus stores a successful status probe: the agent's
// full status reply as JSON, plus the online evidence RecordNodeOnline
// used to store on its own.
func (s *Store) RecordNodeAgentStatus(ctx context.Context, id int64, statusJSON string, inboundCount int, version string, now int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE nodes SET agent_status = ?, agent_status_at = ?,
		 last_online_at = ?, last_inbound_count = ?,
		 last_version = ?, updated_at = ? WHERE id = ?`,
		statusJSON, now, now, inboundCount, version, now, id)
	return err
}

// NodeDeleteResult is the panel-side outcome of deleting a node: how
// many assignments it had, and the single-machine inbounds deleted
// with it (by name, for the audit summary).
type NodeDeleteResult struct {
	Assignments     int
	DeletedInbounds []string
}

// DeleteNodePanelOnly deletes a node and its panel-side related data
// in one transaction, without ever contacting the node's agent: all
// of the node's assignments; every inbound that ran ONLY on this node
// (run_local off and this node its sole assignment), plus those
// inbounds' references in every template's inbound list; then the
// node row itself. Inbounds that also run locally or on other nodes
// keep their definition — only this node's assignment is removed.
// The node's own core config is untouched: its inbounds keep running
// there, they simply stop appearing in panel subscriptions (the
// renderer joins assignments and nodes, so both disappear with them).
func (s *Store) DeleteNodePanelOnly(ctx context.Context, id int64, now int64) (NodeDeleteResult, error) {
	var out NodeDeleteResult
	out.DeletedInbounds = []string{}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx,
		`SELECT inbound_id FROM inbound_nodes WHERE node_id = ? ORDER BY inbound_id`, id)
	if err != nil {
		return out, err
	}
	assigned := []int64{}
	for rows.Next() {
		var inboundID int64
		if err := rows.Scan(&inboundID); err != nil {
			_ = rows.Close()
			return out, err
		}
		assigned = append(assigned, inboundID)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return out, err
	}
	_ = rows.Close()
	out.Assignments = len(assigned)

	single := []int64{}
	for _, inboundID := range assigned {
		var name string
		var runLocal int64
		err := tx.QueryRowContext(ctx,
			`SELECT name, run_local FROM inbounds WHERE id = ?`, inboundID).Scan(&name, &runLocal)
		if err != nil {
			continue // orphan assignment: dropped with the rest below
		}
		var count int
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM inbound_nodes WHERE inbound_id = ?`, inboundID).Scan(&count); err != nil {
			return out, err
		}
		// run_local off and this node the sole assignment: this
		// inbound exists only for this node, so it goes with it.
		if runLocal == 0 && count == 1 {
			single = append(single, inboundID)
			out.DeletedInbounds = append(out.DeletedInbounds, name)
		}
	}

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM inbound_nodes WHERE node_id = ?`, id); err != nil {
		return out, err
	}
	singleSet := map[int64]bool{}
	for _, inboundID := range single {
		singleSet[inboundID] = true
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM inbounds WHERE id = ?`, inboundID); err != nil {
			return out, err
		}
	}
	if len(single) > 0 {
		// Drop the deleted inbounds from every template's list so no
		// template keeps pointing at an inbound that no longer
		// exists. (The pre-existing inbound delete leaves such
		// references inert; node delete cleans them up properly.)
		tplRows, err := tx.QueryContext(ctx,
			`SELECT id, inbound_ids FROM templates ORDER BY id`)
		if err != nil {
			return out, err
		}
		type tpl struct {
			id  int64
			raw string
		}
		tpls := []tpl{}
		for tplRows.Next() {
			var t tpl
			if err := tplRows.Scan(&t.id, &t.raw); err != nil {
				_ = tplRows.Close()
				return out, err
			}
			tpls = append(tpls, t)
		}
		if err := tplRows.Err(); err != nil {
			_ = tplRows.Close()
			return out, err
		}
		_ = tplRows.Close()
		for _, t := range tpls {
			var ids []int64
			if err := json.Unmarshal([]byte(t.raw), &ids); err != nil {
				continue // corrupt list: leave it untouched
			}
			filtered := []int64{}
			changed := false
			for _, inboundID := range ids {
				if singleSet[inboundID] {
					changed = true
					continue
				}
				filtered = append(filtered, inboundID)
			}
			if !changed {
				continue
			}
			raw, err := json.Marshal(filtered)
			if err != nil {
				return out, err
			}
			if _, err := tx.ExecContext(ctx,
				`UPDATE templates SET inbound_ids = ?, updated_at = ? WHERE id = ?`,
				string(raw), now, t.id); err != nil {
				return out, err
			}
		}
	}

	res, err := tx.ExecContext(ctx, `DELETE FROM nodes WHERE id = ?`, id)
	if err != nil {
		return out, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return out, err
	}
	if n == 0 {
		return out, ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return out, err
	}
	return out, nil
}

// RecordNodePushSuccess stores a successful push: the hash (so the
// poller skips identical input), the result, and online evidence (a
// successful push proves the node is reachable).
func (s *Store) RecordNodePushSuccess(ctx context.Context, id int64, hash, result string, now int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE nodes SET last_push_hash = ?, last_push_at = ?,
		 last_push_result = ?, last_error = '', last_online_at = ?,
		 next_retry_at = 0, updated_at = ? WHERE id = ?`,
		hash, now, result, now, now, id)
	return err
}

// RecordNodePushFailure stores a failed push without touching the hash
// (so the poller retries after next_retry_at instead of skipping).
func (s *Store) RecordNodePushFailure(ctx context.Context, id int64, result, errMsg string, now, nextRetry int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE nodes SET last_push_at = ?, last_push_result = ?,
		 last_error = ?, next_retry_at = ?, updated_at = ? WHERE id = ?`,
		now, result, errMsg, nextRetry, now, id)
	return err
}

// RecordNodeOnline stores a successful status probe.
func (s *Store) RecordNodeOnline(ctx context.Context, id int64, inboundCount int, version string, now int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE nodes SET last_online_at = ?, last_inbound_count = ?,
		 last_version = ?, updated_at = ? WHERE id = ?`,
		now, inboundCount, version, now, id)
	return err
}

// --- assignments ------------------------------------------------------------

// InboundAssignment is one inbound -> node assignment. Port 0 means
// "use the inbound's own port on that node"; >0 overrides it.
type InboundAssignment struct {
	NodeID int64
	Port   int
}

// AllInboundNodes returns every assignment row (used by the renderer to
// group inbounds per node in one query).
type InboundNodeRow struct {
	InboundID int64
	NodeID    int64
	Port      int // 0 = the inbound's own port
}

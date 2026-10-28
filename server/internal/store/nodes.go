package store

import (
	"context"
	"fmt"
)

// NodeRow is one remote agent node (a central push target). The token is
// stored in cleartext, like subscription tokens, because it must be
// presented on every push. It is never returned by the API.
type NodeRow struct {
	ID               int64
	Name             string
	Address          string
	Token            string
	PublicHost       string
	TLSMode          string
	UsageRatio       float64
	LastPushHash     string
	LastPushAt       int64
	LastPushResult   string
	LastError        string
	LastOnlineAt     int64
	LastInboundCount int
	LastVersion      string
	NextRetryAt      int64
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
	return rows.Scan(&r.ID, &r.Name, &r.Address, &r.Token, &r.PublicHost,
		&r.TLSMode, &r.UsageRatio, &r.LastPushHash, &r.LastPushAt,
		&r.LastPushResult, &r.LastError, &r.LastOnlineAt,
		&r.LastInboundCount, &r.LastVersion, &r.NextRetryAt,
		&r.CreatedAt, &r.UpdatedAt)
}

const nodeColumns = `id, name, address, token, public_host, tls_mode,
	usage_ratio, last_push_hash, last_push_at, last_push_result,
	last_error, last_online_at, last_inbound_count, last_version,
	next_retry_at, created_at, updated_at`

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
// the stored last_push_hash belongs to the old agent.
func (s *Store) UpdateNode(ctx context.Context, r NodeRow) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE nodes SET name = ?, address = ?, token = ?, public_host = ?,
		 tls_mode = ?, usage_ratio = ?, updated_at = ? WHERE id = ?`,
		r.Name, r.Address, r.Token, r.PublicHost, r.TLSMode,
		r.UsageRatio, r.UpdatedAt, r.ID)
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

// ResetNodePushState clears the push state so the poller pushes fresh
// (used when the node address or token changes: the stored hash belongs
// to the old agent and must not suppress a push to the new one).
func (s *Store) ResetNodePushState(ctx context.Context, id, now int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE nodes SET last_push_hash = '', last_push_result = '',
		 last_error = '', next_retry_at = 0, updated_at = ? WHERE id = ?`,
		now, id)
	return err
}

// DeleteNode removes the node and its assignment rows.
func (s *Store) DeleteNode(ctx context.Context, id int64) error {
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM inbound_nodes WHERE node_id = ?`, id); err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM nodes WHERE id = ?`, id)
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

// InboundNodeIDs returns the remote node ids assigned to an inbound.
func (s *Store) InboundNodeIDs(ctx context.Context, inboundID int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT node_id FROM inbound_nodes WHERE inbound_id = ? ORDER BY node_id`, inboundID)
	if err != nil {
		return nil, fmt.Errorf("store: inbound nodes: %w", err)
	}
	defer rows.Close()
	out := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// NodeInboundIDs returns the inbound ids assigned to a node.
func (s *Store) NodeInboundIDs(ctx context.Context, nodeID int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT inbound_id FROM inbound_nodes WHERE node_id = ? ORDER BY inbound_id`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("store: node inbounds: %w", err)
	}
	defer rows.Close()
	out := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

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

func (s *Store) AllInboundNodes(ctx context.Context) ([]InboundNodeRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT inbound_id, node_id, port FROM inbound_nodes ORDER BY node_id, inbound_id`)
	if err != nil {
		return nil, fmt.Errorf("store: all inbound nodes: %w", err)
	}
	defer rows.Close()
	out := []InboundNodeRow{}
	for rows.Next() {
		var r InboundNodeRow
		if err := rows.Scan(&r.InboundID, &r.NodeID, &r.Port); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// NodeInboundAssignments returns one node's assignments, with
// per-node ports (used by the push builder).
func (s *Store) NodeInboundAssignments(ctx context.Context, nodeID int64) ([]InboundNodeRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT inbound_id, node_id, port FROM inbound_nodes WHERE node_id = ? ORDER BY inbound_id`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("store: node inbound assignments: %w", err)
	}
	defer rows.Close()
	out := []InboundNodeRow{}
	for rows.Next() {
		var r InboundNodeRow
		if err := rows.Scan(&r.InboundID, &r.NodeID, &r.Port); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// InboundNodes returns one inbound's assignments, with per-node ports.
func (s *Store) InboundNodes(ctx context.Context, inboundID int64) ([]InboundNodeRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT inbound_id, node_id, port FROM inbound_nodes WHERE inbound_id = ? ORDER BY node_id`, inboundID)
	if err != nil {
		return nil, fmt.Errorf("store: inbound nodes: %w", err)
	}
	defer rows.Close()
	out := []InboundNodeRow{}
	for rows.Next() {
		var r InboundNodeRow
		if err := rows.Scan(&r.InboundID, &r.NodeID, &r.Port); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SetInboundNodes atomically replaces an inbound's node assignment.
func (s *Store) SetInboundNodes(ctx context.Context, inboundID int64, nodes []InboundAssignment) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM inbound_nodes WHERE inbound_id = ?`, inboundID); err != nil {
		return err
	}
	for _, n := range nodes {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO inbound_nodes (inbound_id, node_id, port) VALUES (?, ?, ?)`,
			inboundID, n.NodeID, n.Port); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// AddNodeToAllInbounds assigns a node to every existing inbound
// (the "add as host for every inbound" checkbox on node creation).
func (s *Store) AddNodeToAllInbounds(ctx context.Context, nodeID int64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO inbound_nodes (inbound_id, node_id)
		 SELECT id, ? FROM inbounds`, nodeID)
	return err
}

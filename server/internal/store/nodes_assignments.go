package store

import (
	"context"
	"fmt"
)

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

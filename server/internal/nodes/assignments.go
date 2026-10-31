package nodes

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"singbox-ui/server/internal/store"
)

// GetInboundNodes returns the remote node ids assigned to an inbound.
func (s *Service) GetInboundNodes(ctx context.Context, inboundID int64) ([]int64, error) {
	if err := s.inboundExists(ctx, inboundID); err != nil {
		return nil, err
	}
	return s.st.InboundNodeIDs(ctx, inboundID)
}

// GetInboundNodeAssignments returns one inbound's assignments with
// per-node ports (port 0 = the inbound's own port).
// GetInboundNodeAssignments returns one inbound's assignments with
// per-node ports (port 0 = the inbound's own port).
func (s *Service) GetInboundNodeAssignments(ctx context.Context, inboundID int64) ([]store.InboundAssignment, error) {
	if err := s.inboundExists(ctx, inboundID); err != nil {
		return nil, err
	}
	rows, err := s.st.InboundNodes(ctx, inboundID)
	if err != nil {
		return nil, err
	}
	out := []store.InboundAssignment{}
	for _, r := range rows {
		out = append(out, store.InboundAssignment{NodeID: r.NodeID, Port: r.Port})
	}
	return out, nil
}

// SetInboundNodes replaces an inbound's remote node assignment.
// Port 0 means "use the inbound's own port on that node". Every
// assignment is validated against the target node's live answer
// before anything is saved: an unreachable node, an agent too old
// for check-port, or an occupied port all reject the whole save
// (nothing is half-saved). A port already held by this same inbound
// on that node is not a conflict.
// SetInboundNodes replaces an inbound's remote node assignment.
// Port 0 means "use the inbound's own port on that node". Every
// assignment is validated against the target node's live answer
// before anything is saved: an unreachable node, an agent too old
// for check-port, or an occupied port all reject the whole save
// (nothing is half-saved). A port already held by this same inbound
// on that node is not a conflict.
func (s *Service) SetInboundNodes(ctx context.Context, inboundID int64, nodes []store.InboundAssignment) ([]store.InboundAssignment, error) {
	ib, err := s.loadInbound(ctx, inboundID)
	if err != nil {
		return nil, err
	}
	known, err := s.st.ListNodes(ctx)
	if err != nil {
		return nil, err
	}
	byID := map[int64]store.NodeRow{}
	for _, n := range known {
		byID[n.ID] = n
	}
	dedup := []store.InboundAssignment{}
	seen := map[int64]bool{}
	for _, n := range nodes {
		if _, ok := byID[n.NodeID]; !ok {
			return nil, errValidation(fmt.Sprintf("unknown node id %d", n.NodeID))
		}
		if n.Port < 0 || n.Port > 65535 {
			return nil, errValidation(fmt.Sprintf("invalid node port %d", n.Port))
		}
		if !seen[n.NodeID] {
			seen[n.NodeID] = true
			dedup = append(dedup, n)
		}
	}
	transport := protocolTransport[ib.Protocol]
	if transport == "" {
		transport = "both"
	}
	for _, n := range dedup {
		node := byID[n.NodeID]
		port := n.Port
		if port <= 0 {
			port = ib.Port
		}
		chk, err := s.client.CheckPort(ctx, node.Address, node.Token, port, transport)
		if err != nil {
			if errors.Is(err, ErrCheckPortUnsupported) {
				return nil, errValidation(fmt.Sprintf("节点 %q 的 agent 版本过旧，不支持端口查询，请升级 agent 后再分配", node.Name))
			}
			return nil, errValidation(fmt.Sprintf("节点 %q 无法联系，无法确认端口 %d 是否可用：%v", node.Name, port, err))
		}
		if chk.Occupied && !(chk.Kind == "managed" && chk.OccupiedBy == ib.Name) {
			return nil, errValidation(fmt.Sprintf("节点 %q 的端口 %d（%s）已被占用：%s", node.Name, port, transport, chk.Reason))
		}
	}
	if err := s.st.SetInboundNodes(ctx, inboundID, dedup); err != nil {
		return nil, err
	}
	return dedup, nil
}

// CheckNodePort proxies a live port check to one node (for the
// frontend's occupancy hints). It reports the agent's answer
// verbatim; reachability failures surface as errors.
// CheckNodePort proxies a live port check to one node (for the
// frontend's occupancy hints). It reports the agent's answer
// verbatim; reachability failures surface as errors.
func (s *Service) CheckNodePort(ctx context.Context, nodeID int64, port int, transport string) (*AgentPortCheck, error) {
	if port < 1 || port > 65535 {
		return nil, errValidation("invalid port")
	}
	switch transport {
	case "", "both":
		transport = "both"
	case "tcp", "udp":
	default:
		return nil, errValidation("invalid transport (tcp|udp|both)")
	}
	node, err := s.st.GetNode(ctx, nodeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errNotFound("node")
		}
		return nil, err
	}
	return s.client.CheckPort(ctx, node.Address, node.Token, port, transport)
}
func (s *Service) loadInbound(ctx context.Context, inboundID int64) (*store.InboundRow, error) {
	rows, err := s.st.ListInbounds(ctx)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		if rows[i].ID == inboundID {
			return &rows[i], nil
		}
	}
	return nil, errNotFound("inbound")
}
func (s *Service) inboundExists(ctx context.Context, inboundID int64) error {
	_, err := s.loadInbound(ctx, inboundID)
	return err
}

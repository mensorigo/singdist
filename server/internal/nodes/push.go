package nodes

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// PushNow forces a background push (manual retry / recovery).
func (s *Service) PushNow(ctx context.Context, id int64) (bool, error) {
	started, err := s.pusher.PushNow(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, errNotFound("node")
		}
		return false, err
	}
	return started, nil
}

// ClearBlock clears the agent's blocked input, then auto-retries the
// push (one click covers clear + retry).
// ClearBlock clears the agent's blocked input, then auto-retries the
// push (one click covers clear + retry).
func (s *Service) ClearBlock(ctx context.Context, id int64) (bool, bool, error) {
	r, err := s.st.GetNode(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, false, errNotFound("node")
		}
		return false, false, err
	}
	cleared, err := s.client.ClearBlock(ctx, r.Address, r.Token)
	if err != nil {
		return false, false, errUpstream(truncate(err.Error(), 200))
	}
	started := false
	if cleared {
		started, _ = s.pusher.PushNow(ctx, id)
	}
	return cleared, started, nil
}

// ProbeStatus fetches the agent status live and records online evidence.
// ProbeStatus fetches the agent status live and records online evidence.
func (s *Service) ProbeStatus(ctx context.Context, id int64) (*AgentStatus, NodeView, error) {
	r, err := s.st.GetNode(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, NodeView{}, errNotFound("node")
		}
		return nil, NodeView{}, err
	}
	st, err := s.client.Status(ctx, r.Address, r.Token)
	if err != nil {
		return nil, NodeView{}, errUpstream(truncate(err.Error(), 200))
	}
	now := time.Now().Unix()
	raw, _ := json.Marshal(st)
	_ = s.st.RecordNodeAgentStatus(ctx, id, string(raw), len(st.Inbounds), st.Version, now)
	fresh, err := s.st.GetNode(ctx, id)
	if err != nil {
		return nil, NodeView{}, err
	}
	assigned, err := s.st.NodeInboundAssignments(ctx, id)
	if err != nil {
		return nil, NodeView{}, err
	}
	inbounds, err := s.st.ListInbounds(ctx)
	if err != nil {
		return nil, NodeView{}, err
	}
	return st, enrichSyncView(toView(*fresh, now), *fresh, now, assigned, inbounds), nil
}

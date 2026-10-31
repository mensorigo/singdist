package nodes

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"singbox-ui/server/internal/store"
)

// Error is a typed API failure (mirrors the other modules).
type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }
func errValidation(msg string) *Error {
	return &Error{Status: 400, Code: "validation", Message: msg}
}
func errNotFound(what string) *Error {
	return &Error{Status: 404, Code: what + "_not_found", Message: what + " not found"}
}
func errConflict(msg string) *Error {
	return &Error{Status: 409, Code: "conflict", Message: msg}
}
func errUpstream(msg string) *Error {
	return &Error{Status: 502, Code: "node_unreachable", Message: msg}
}

// NodeView is the API shape for a node. The token is never returned:
// the edit form sends an empty token to mean "keep the existing one".
// Service owns node business logic.
type Service struct {
	st     *store.Store
	pusher *Pusher
	client *Client
}

func NewService(st *store.Store, pusher *Pusher) *Service {
	return &Service{st: st, pusher: pusher, client: NewClient()}
}

// Service owns node business logic.
// DeleteResult is the panel-side outcome of deleting a node.
type DeleteResult struct {
	Assignments     int
	DeletedInbounds []string
}

// Delete removes a node and its panel-side related data only — it
// never contacts the node's agent, never pushes an empty config, and
// never stops anything on the node. The node's own inbounds keep
// running there; they simply stop appearing in panel subscriptions
// (and in subscriptions newly rendered from templates) because the
// node record, its assignments, and any single-machine inbounds (plus
// those inbounds' template references) are gone. Restoring a deleted
// node means adding it again from scratch (re-pairing if needed).
// An unreachable node deletes just the same: nothing here depends on
// the agent at all.
func (s *Service) List(ctx context.Context) ([]NodeView, error) {
	rows, err := s.st.ListNodes(ctx)
	if err != nil {
		return nil, err
	}
	inbounds, err := s.st.ListInbounds(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	out := []NodeView{}
	for _, r := range rows {
		assigned, err := s.st.NodeInboundAssignments(ctx, r.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, enrichSyncView(toView(r, now), r, now, assigned, inbounds))
	}
	return out, nil
}
func (s *Service) Get(ctx context.Context, id int64) (NodeView, []int64, error) {
	r, err := s.st.GetNode(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return NodeView{}, nil, errNotFound("node")
		}
		return NodeView{}, nil, err
	}
	ids, err := s.st.NodeInboundIDs(ctx, id)
	if err != nil {
		return NodeView{}, nil, err
	}
	assigned, err := s.st.NodeInboundAssignments(ctx, id)
	if err != nil {
		return NodeView{}, nil, err
	}
	inbounds, err := s.st.ListInbounds(ctx)
	if err != nil {
		return NodeView{}, nil, err
	}
	return enrichSyncView(toView(*r, time.Now().Unix()), *r, time.Now().Unix(), assigned, inbounds), ids, nil
}

// DeleteResult is the panel-side outcome of deleting a node.
// Delete removes a node and its panel-side related data only — it
// never contacts the node's agent, never pushes an empty config, and
// never stops anything on the node. The node's own inbounds keep
// running there; they simply stop appearing in panel subscriptions
// (and in subscriptions newly rendered from templates) because the
// node record, its assignments, and any single-machine inbounds (plus
// those inbounds' template references) are gone. Restoring a deleted
// node means adding it again from scratch (re-pairing if needed).
// An unreachable node deletes just the same: nothing here depends on
// the agent at all.
func (s *Service) Delete(ctx context.Context, id int64) (DeleteResult, error) {
	if _, err := s.st.GetNode(ctx, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return DeleteResult{}, errNotFound("node")
		}
		return DeleteResult{}, err
	}
	res, err := s.st.DeleteNodePanelOnly(ctx, id, time.Now().Unix())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return DeleteResult{}, errNotFound("node")
		}
		return DeleteResult{}, err
	}
	return DeleteResult{
		Assignments:     res.Assignments,
		DeletedInbounds: res.DeletedInbounds,
	}, nil
}

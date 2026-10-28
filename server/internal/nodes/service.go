package nodes

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"net"
	"strconv"
	"strings"
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
type NodeView struct {
	ID               int64   `json:"id"`
	Name             string  `json:"name"`
	Address          string  `json:"address"`
	PublicHost       string  `json:"publicHost"`
	TLSMode          string  `json:"tlsMode"`
	UsageRatio       float64 `json:"usageRatio"`
	Online           bool    `json:"online"`
	LastPushAt       int64   `json:"lastPushAt"`
	LastPushResult   string  `json:"lastPushResult"`
	LastError        string  `json:"lastError"`
	LastOnlineAt     int64   `json:"lastOnlineAt"`
	LastInboundCount int     `json:"lastInboundCount"`
	LastVersion      string  `json:"lastVersion"`
	CreatedAt        int64   `json:"createdAt"`
	UpdatedAt        int64   `json:"updatedAt"`
}

func toView(r store.NodeRow, now int64) NodeView {
	return NodeView{
		ID:               r.ID,
		Name:             r.Name,
		Address:          r.Address,
		PublicHost:       r.PublicHost,
		TLSMode:          r.TLSMode,
		UsageRatio:       r.UsageRatio,
		Online:           r.Online(now),
		LastPushAt:       r.LastPushAt,
		LastPushResult:   r.LastPushResult,
		LastError:        r.LastError,
		LastOnlineAt:     r.LastOnlineAt,
		LastInboundCount: r.LastInboundCount,
		LastVersion:      r.LastVersion,
		CreatedAt:        r.CreatedAt,
		UpdatedAt:        r.UpdatedAt,
	}
}

// CreateRequest is the POST /nodes body.
type CreateRequest struct {
	Name             string   `json:"name"`
	Address          string   `json:"address"`
	Token            string   `json:"token"`
	PublicHost       string   `json:"publicHost"`
	TLSMode          string   `json:"tlsMode"`
	UsageRatio       *float64 `json:"usageRatio"`
	AddToAllInbounds bool     `json:"addToAllInbounds"`
}

// UpdateRequest is the PATCH /nodes/:id body. Only non-nil fields are
// applied; an empty token means "keep the existing one".
type UpdateRequest struct {
	Name       *string  `json:"name"`
	Address    *string  `json:"address"`
	Token      *string  `json:"token"`
	PublicHost *string  `json:"publicHost"`
	TLSMode    *string  `json:"tlsMode"`
	UsageRatio *float64 `json:"usageRatio"`
}

// Service owns node business logic.
type Service struct {
	st     *store.Store
	pusher *Pusher
	client *Client
}

func NewService(st *store.Store, pusher *Pusher) *Service {
	return &Service{st: st, pusher: pusher, client: NewClient()}
}

func validateName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 64 {
		return "", errValidation("name must be 1-64 characters")
	}
	return name, nil
}

func validateAddress(address string) (string, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return "", errValidation("address is required")
	}
	_, hostport, err := ParseAddress(address)
	if err != nil {
		return "", errValidation(err.Error())
	}
	_, portStr, err := net.SplitHostPort(hostport)
	if err != nil {
		return "", errValidation("address must be host:port")
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return "", errValidation("address port must be 1-65535")
	}
	return address, nil
}

func validateToken(token string) (string, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", errValidation("token is required")
	}
	if len(token) > 512 {
		return "", errValidation("token is too long")
	}
	return token, nil
}

func validateTLSMode(mode string) (string, error) {
	if mode == "" {
		return "skip-verify", nil
	}
	if mode != "skip-verify" {
		return "", errValidation("tlsMode not supported yet (only skip-verify in v1)")
	}
	return mode, nil
}

func validateRatio(ratio *float64) (float64, error) {
	if ratio == nil {
		return 1.0, nil
	}
	if *ratio <= 0 || math.IsNaN(*ratio) || math.IsInf(*ratio, 0) {
		return 0, errValidation("usageRatio must be greater than 0")
	}
	return *ratio, nil
}

func (s *Service) List(ctx context.Context) ([]NodeView, error) {
	rows, err := s.st.ListNodes(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	out := []NodeView{}
	for _, r := range rows {
		out = append(out, toView(r, now))
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
	return toView(*r, time.Now().Unix()), ids, nil
}

func (s *Service) Create(ctx context.Context, req CreateRequest) (NodeView, error) {
	name, err := validateName(req.Name)
	if err != nil {
		return NodeView{}, err
	}
	address, err := validateAddress(req.Address)
	if err != nil {
		return NodeView{}, err
	}
	token, err := validateToken(req.Token)
	if err != nil {
		return NodeView{}, err
	}
	tlsMode, err := validateTLSMode(req.TLSMode)
	if err != nil {
		return NodeView{}, err
	}
	ratio, err := validateRatio(req.UsageRatio)
	if err != nil {
		return NodeView{}, err
	}
	existing, err := s.st.ListNodes(ctx)
	if err != nil {
		return NodeView{}, err
	}
	for _, e := range existing {
		if e.Name == name {
			return NodeView{}, errConflict("node name already exists")
		}
	}
	now := time.Now().Unix()
	id, err := s.st.CreateNode(ctx, store.NodeRow{
		Name: name, Address: address, Token: token,
		PublicHost: strings.TrimSpace(req.PublicHost),
		TLSMode:    tlsMode, UsageRatio: ratio,
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			return NodeView{}, errConflict("node name already exists")
		}
		return NodeView{}, err
	}
	if req.AddToAllInbounds {
		if err := s.st.AddNodeToAllInbounds(ctx, id); err != nil {
			return NodeView{}, err
		}
	}
	r, err := s.st.GetNode(ctx, id)
	if err != nil {
		return NodeView{}, err
	}
	return toView(*r, now), nil
}

func (s *Service) Update(ctx context.Context, id int64, req UpdateRequest) (NodeView, error) {
	r, err := s.st.GetNode(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return NodeView{}, errNotFound("node")
		}
		return NodeView{}, err
	}
	addrChanged, tokenChanged := false, false
	if req.Name != nil {
		name, err := validateName(*req.Name)
		if err != nil {
			return NodeView{}, err
		}
		if name != r.Name {
			existing, err := s.st.ListNodes(ctx)
			if err != nil {
				return NodeView{}, err
			}
			for _, e := range existing {
				if e.ID != id && e.Name == name {
					return NodeView{}, errConflict("node name already exists")
				}
			}
		}
		r.Name = name
	}
	if req.Address != nil {
		address, err := validateAddress(*req.Address)
		if err != nil {
			return NodeView{}, err
		}
		if address != r.Address {
			addrChanged = true
		}
		r.Address = address
	}
	if req.Token != nil {
		if token := strings.TrimSpace(*req.Token); token != "" {
			token, err := validateToken(token)
			if err != nil {
				return NodeView{}, err
			}
			if token != r.Token {
				tokenChanged = true
			}
			r.Token = token
		}
	}
	if req.PublicHost != nil {
		r.PublicHost = strings.TrimSpace(*req.PublicHost)
	}
	if req.TLSMode != nil {
		tlsMode, err := validateTLSMode(*req.TLSMode)
		if err != nil {
			return NodeView{}, err
		}
		r.TLSMode = tlsMode
	}
	if req.UsageRatio != nil {
		ratio, err := validateRatio(req.UsageRatio)
		if err != nil {
			return NodeView{}, err
		}
		r.UsageRatio = ratio
	}
	now := time.Now().Unix()
	r.UpdatedAt = now
	if err := s.st.UpdateNode(ctx, *r); err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			return NodeView{}, errConflict("node name already exists")
		}
		return NodeView{}, err
	}
	// A node that moved to a different agent (or got a new token) must
	// be pushed fresh: the stored hash belongs to the old agent and
	// must not suppress a push to the new one.
	if addrChanged || tokenChanged {
		if err := s.st.ResetNodePushState(ctx, id, now); err != nil {
			return NodeView{}, err
		}
	}
	fresh, err := s.st.GetNode(ctx, id)
	if err != nil {
		return NodeView{}, err
	}
	return toView(*fresh, now), nil
}

// Delete pushes an empty managed set first (so the agent drops every
// centrally managed inbound; operator passthrough on the node stays),
// then deletes the record. When the node is unreachable the record is
// still deleted, but the caller gets a warning: the node may keep
// serving the old config until its agent is stopped by hand.
func (s *Service) Delete(ctx context.Context, id int64) (string, error) {
	r, err := s.st.GetNode(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", errNotFound("node")
		}
		return "", err
	}
	pushCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	payload := PushPayload{
		Inbounds:   []PushInbound{},
		PublicHost: PublicHostOf(r.PublicHost, r.Address),
	}
	res, err := s.client.Push(pushCtx, r.Address, r.Token, payload)
	warning := ""
	if err != nil || res == nil || (res.Result != "applied" && res.Result != "no_change") {
		warning = "node did not confirm the empty push; it may still serve the old config until its agent is stopped by hand"
	}
	if err := s.st.DeleteNode(ctx, id); err != nil {
		return "", err
	}
	return warning, nil
}

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
	_ = s.st.RecordNodeOnline(ctx, id, len(st.Inbounds), st.Version, now)
	fresh, err := s.st.GetNode(ctx, id)
	if err != nil {
		return nil, NodeView{}, err
	}
	return st, toView(*fresh, now), nil
}

// GetInboundNodes returns the remote node ids assigned to an inbound.
func (s *Service) GetInboundNodes(ctx context.Context, inboundID int64) ([]int64, error) {
	if err := s.inboundExists(ctx, inboundID); err != nil {
		return nil, err
	}
	return s.st.InboundNodeIDs(ctx, inboundID)
}

// SetInboundNodes replaces an inbound's remote node assignment.
func (s *Service) SetInboundNodes(ctx context.Context, inboundID int64, nodeIDs []int64) ([]int64, error) {
	if err := s.inboundExists(ctx, inboundID); err != nil {
		return nil, err
	}
	nodes, err := s.st.ListNodes(ctx)
	if err != nil {
		return nil, err
	}
	known := map[int64]bool{}
	for _, n := range nodes {
		known[n.ID] = true
	}
	dedup := []int64{}
	seen := map[int64]bool{}
	for _, nid := range nodeIDs {
		if !known[nid] {
			return nil, errValidation(fmt.Sprintf("unknown node id %d", nid))
		}
		if !seen[nid] {
			seen[nid] = true
			dedup = append(dedup, nid)
		}
	}
	if err := s.st.SetInboundNodes(ctx, inboundID, dedup); err != nil {
		return nil, err
	}
	return dedup, nil
}

func (s *Service) inboundExists(ctx context.Context, inboundID int64) error {
	rows, err := s.st.ListInbounds(ctx)
	if err != nil {
		return err
	}
	for _, r := range rows {
		if r.ID == inboundID {
			return nil
		}
	}
	return errNotFound("inbound")
}

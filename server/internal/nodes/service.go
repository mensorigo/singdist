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
	Warning          string  `json:"warning,omitempty"` // creation-time probe warning, if any
	CreatedAt        int64   `json:"createdAt"`
	UpdatedAt        int64   `json:"updatedAt"`
}

// protocolTransport mirrors the agent's coreTypeTransports and the
// panel's protocolSpecs: the transports an inbound listens on.
// Unknown protocols map to both (conservative).
var protocolTransport = map[string]string{
	"vless-reality": "tcp",
	"hysteria2":     "udp",
	"tuic":          "udp",
	"shadowsocks":   "both",
	"anytls":        "tcp",
	"trojan":        "tcp",
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
	v := toView(*r, now)
	// Creation-time reachability probe (capped at 3s so a dead node
	// does not stall creation): a failure only warns — the node is
	// still registered and the background poller keeps probing it.
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if st, err := s.client.Status(probeCtx, r.Address, r.Token); err != nil {
		v.Warning = "节点暂时无法联系，创建已保存，后台会继续探测"
	} else {
		v.Online = true
		v.LastInboundCount = len(st.Inbounds)
		v.LastVersion = st.Version
	}
	return v, nil
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

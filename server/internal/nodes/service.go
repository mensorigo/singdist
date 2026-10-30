package nodes

import (
	"context"
	"database/sql"
	"encoding/json"
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
	ID               int64           `json:"id"`
	Name             string          `json:"name"`
	Address          string          `json:"address"`
	PublicHost       string          `json:"publicHost"`
	TLSMode          string          `json:"tlsMode"`
	UsageRatio       float64         `json:"usageRatio"`
	Paused           bool            `json:"paused"`
	PausePending     bool            `json:"pausePending"` // paused but the empty push has not landed on the node yet
	SyncStatus       string          `json:"syncStatus"`   // synced | pending | mismatch | offline | paused | unknown
	SyncDetail       string          `json:"syncDetail"`   // human-readable reason when not synced, "" when synced
	AgentUnitActive  string          `json:"agentUnitActive"`
	AgentClashAlive  bool            `json:"agentClashAlive"`
	AgentStatusAt    int64           `json:"agentStatusAt"`
	AgentInbounds    []StatusInbound `json:"agentInbounds"`
	AgentSystem      AgentSystem     `json:"agentSystem,omitempty"`
	AgentTraffic     *AgentTraffic   `json:"agentTraffic,omitempty"`
	AgentLastResult  string          `json:"agentLastResult"`
	AgentBlockedHash string          `json:"agentBlockedHash"`
	Online           bool            `json:"online"`
	LastPushAt       int64           `json:"lastPushAt"`
	LastPushResult   string          `json:"lastPushResult"`
	LastError        string          `json:"lastError"`
	LastOnlineAt     int64           `json:"lastOnlineAt"`
	LastInboundCount int             `json:"lastInboundCount"`
	LastVersion      string          `json:"lastVersion"`
	Warning          string          `json:"warning,omitempty"` // creation-time probe warning, if any
	CreatedAt        int64           `json:"createdAt"`
	UpdatedAt        int64           `json:"updatedAt"`
}

// protocolTransport mirrors the agent's coreTypeTransports and the
// panel's protocolSpecs: the transports an inbound listens on.
// Unknown protocols map to both (conservative).
var protocolTransport = map[string]string{
	"vless-reality": "tcp",
	"hysteria2":     "udp",
	"tuic":          "udp",
	"anytls":        "tcp",
	"trojan":        "tcp",
}

func toView(r store.NodeRow, now int64) NodeView {
	pausePending := false
	if r.Paused {
		if _, pausedHash, err := BuildPausedPush(r); err == nil {
			pausePending = r.LastPushHash != pausedHash
		}
	}
	return NodeView{
		ID:               r.ID,
		Name:             r.Name,
		Address:          r.Address,
		PublicHost:       r.PublicHost,
		TLSMode:          r.TLSMode,
		UsageRatio:       r.UsageRatio,
		Paused:           r.Paused,
		PausePending:     pausePending,
		SyncStatus:       "unknown",
		AgentInbounds:    []StatusInbound{},
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

// enrichSync fills a NodeView's agent snapshot fields and its sync
// status from the stored snapshot, the stored desired/pushed hashes,
// and the node's panel assignments. "Synced" is strict on purpose:
// online, no push error, desired hash == pushed hash, no agent block,
// the agent's own last result applied/no_change, and every enabled
// panel-assigned inbound present on the agent and listening. Anything
// else is pending (a push is still due), mismatch (something
// disagrees), offline, paused, or unknown (no snapshot yet). Extra
// agent inbounds the panel never assigned are not a mismatch: they
// are the node's own local inbounds, shown on the card as-is.
// Clash and core-unit state are shown as info only: they do not
// decide sync, because inbounds can serve while Clash is down.
func enrichSyncView(v NodeView, r store.NodeRow, now int64, assigned []store.InboundNodeRow, inbounds []store.InboundRow) NodeView {
	if r.AgentStatus != "" {
		var snap AgentStatus
		if err := json.Unmarshal([]byte(r.AgentStatus), &snap); err == nil {
			v.AgentUnitActive = snap.UnitActive
			v.AgentClashAlive = snap.ClashAlive
			v.AgentStatusAt = r.AgentStatusAt
			v.AgentLastResult = snap.LastResult
			v.AgentBlockedHash = snap.BlockedHash
			v.AgentSystem = snap.System
			v.AgentTraffic = snap.Traffic
			v.AgentInbounds = snap.Inbounds
			if v.AgentInbounds == nil {
				v.AgentInbounds = []StatusInbound{}
			}
		}
	}
	if r.Paused {
		v.SyncStatus = "paused"
		return v
	}
	if !r.Online(now) {
		v.SyncStatus = "offline"
		v.SyncDetail = "节点离线"
		return v
	}
	if r.AgentStatus == "" || r.AgentStatusAt == 0 || now-r.AgentStatusAt > store.OnlineWithin {
		v.SyncStatus = "unknown"
		v.SyncDetail = "暂无节点状态快照"
		return v
	}
	if r.LastError != "" {
		v.SyncStatus = "mismatch"
		v.SyncDetail = "推送失败：" + r.LastError
		return v
	}
	switch r.LastPushResult {
	case "applied", "no_change", "":
	default:
		v.SyncStatus = "mismatch"
		v.SyncDetail = "上次推送结果：" + r.LastPushResult
		return v
	}
	if r.DesiredPushHash == "" {
		v.SyncStatus = "unknown"
		v.SyncDetail = "推送状态还没算出来"
		return v
	}
	if r.DesiredPushHash != r.LastPushHash {
		v.SyncStatus = "pending"
		v.SyncDetail = "推送还没落到节点"
		return v
	}
	var snap AgentStatus
	if err := json.Unmarshal([]byte(r.AgentStatus), &snap); err != nil {
		v.SyncStatus = "unknown"
		v.SyncDetail = "节点状态快照损坏"
		return v
	}
	if snap.BlockedHash != "" {
		v.SyncStatus = "mismatch"
		v.SyncDetail = "节点被阻塞，需清除阻塞并重推"
		return v
	}
	switch snap.LastResult {
	case "applied", "no_change":
	default:
		v.SyncStatus = "mismatch"
		v.SyncDetail = "节点上次同步结果：" + snap.LastResult
		return v
	}
	byID := map[int64]store.InboundRow{}
	for _, ib := range inbounds {
		byID[ib.ID] = ib
	}
	byName := map[string]StatusInbound{}
	for _, ai := range snap.Inbounds {
		byName[ai.Name] = ai
	}
	for _, a := range assigned {
		ib, ok := byID[a.InboundID]
		if !ok || !ib.Enabled {
			continue
		}
		port := ib.Port
		if a.Port > 0 {
			port = a.Port
		}
		ai, ok := byName[ib.Name]
		if !ok {
			v.SyncStatus = "mismatch"
			v.SyncDetail = "节点上缺入站：" + ib.Name
			return v
		}
		if ai.Port != port {
			v.SyncStatus = "mismatch"
			v.SyncDetail = "节点入站端口不对：" + ib.Name
			return v
		}
		if ai.Listening == nil || !*ai.Listening {
			v.SyncStatus = "mismatch"
			v.SyncDetail = "节点入站未在监听：" + ib.Name
			return v
		}
	}
	v.SyncStatus = "synced"
	v.SyncDetail = ""
	return v
}

// CreateRequest is the POST /nodes body. PairingCode is a temporary
// pairing field: it is never stored and never audited — when set,
// it is exchanged for a long-term token via the agent first, and
// that token becomes the node's token. Token is then not required.
type CreateRequest struct {
	Name             string   `json:"name"`
	Address          string   `json:"address"`
	Token            string   `json:"token"`
	PairingCode      string   `json:"pairing_code"`
	PublicHost       string   `json:"publicHost"`
	TLSMode          string   `json:"tlsMode"`
	UsageRatio       *float64 `json:"usageRatio"`
	AddToAllInbounds bool     `json:"addToAllInbounds"`
}

// UpdateRequest is the PATCH /nodes/:id body. Only non-nil fields are
// applied; an empty token means "keep the existing one". A set
// PairingCode re-pairs the node: it is exchanged for a fresh token
// that replaces the stored one, and the Token field is ignored.
type UpdateRequest struct {
	Name        *string  `json:"name"`
	Address     *string  `json:"address"`
	Token       *string  `json:"token"`
	PairingCode *string  `json:"pairing_code"`
	PublicHost  *string  `json:"publicHost"`
	TLSMode     *string  `json:"tlsMode"`
	UsageRatio  *float64 `json:"usageRatio"`
	Paused      *bool    `json:"paused"`
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

// truncateRunes caps a derived name at n runes without appending an
// ellipsis (unlike truncate, which is for error snippets).
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
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

// pairForAddress exchanges a pairing code for a long-term token at
// the agent. An old-agent 404 becomes the upgrade message; a
// generic pairing failure becomes a validation error carrying the
// agent's message; anything else (unreachable, bad address) is an
// upstream error.
func (s *Service) pairForAddress(ctx context.Context, address, code string) (string, error) {
	token, err := s.client.Pair(ctx, address, code)
	if err != nil {
		if errors.Is(err, ErrPairingUnsupported) {
			return "", errValidation("节点 agent 版本过旧，不支持配对，请升级 agent 后再试")
		}
		if errors.Is(err, ErrPairingInvalid) {
			return "", errValidation(err.Error())
		}
		return "", errUpstream(truncate(err.Error(), 200))
	}
	return token, nil
}

// RequestPairingCode validates an address and asks the agent to
// generate a pairing code. It returns only ok/retry_after: the code
// itself is never in this response (it goes to the agent log).
func (s *Service) RequestPairingCode(ctx context.Context, address string) (bool, int, error) {
	addr, err := validateAddress(address)
	if err != nil {
		return false, 0, err
	}
	ok, retry, err := s.client.RequestPairingCode(ctx, addr)
	if err != nil {
		if errors.Is(err, ErrPairingUnsupported) {
			return false, 0, errValidation("节点 agent 版本过旧，不支持配对，请升级 agent 后再试")
		}
		return false, 0, errUpstream(truncate(err.Error(), 200))
	}
	return ok, retry, nil
}

func (s *Service) Create(ctx context.Context, req CreateRequest) (NodeView, error) {
	// An empty 备注 is filled from the node's hostname (creation
	// probe) and falls back to the address host part; it never fails
	// creation on its own. A non-empty 备注 keeps the old 1-64
	// validation.
	name := strings.TrimSpace(req.Name)
	nameWasEmpty := name == ""
	if !nameWasEmpty {
		var err error
		name, err = validateName(name)
		if err != nil {
			return NodeView{}, err
		}
	}
	address, err := validateAddress(req.Address)
	if err != nil {
		return NodeView{}, err
	}
	// Pairing wins over a manual token: the code is exchanged first
	// (no old-token probe is involved — there is no old token on a
	// create), and the returned token becomes the node's token.
	// Without a code, the manual token rules are unchanged.
	var token string
	paired := false
	if code := strings.TrimSpace(req.PairingCode); code != "" {
		t, err := s.pairForAddress(ctx, address, code)
		if err != nil {
			return NodeView{}, err
		}
		token, paired = t, true
	} else {
		t, err := validateToken(req.Token)
		if err != nil {
			return NodeView{}, err
		}
		token = t
	}
	tlsMode, err := validateTLSMode(req.TLSMode)
	if err != nil {
		return NodeView{}, err
	}
	ratio, err := validateRatio(req.UsageRatio)
	if err != nil {
		return NodeView{}, err
	}
	// Derive an empty 备注 before insert (the row needs a name for
	// the UNIQUE check). The probe is capped at 3s like the
	// post-insert probe below, and its result is reused for the view
	// so an empty-name creation probes at most once. A failed probe,
	// or an old agent without the hostname field, falls back to the
	// address host part (address validation guarantees a non-empty
	// host, so derivation always yields a name) and still creates
	// the node — only the warning is set.
	var probeStatus *AgentStatus
	probeOK := false
	if nameWasEmpty {
		probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		st, perr := s.client.Status(probeCtx, address, token)
		cancel()
		if perr == nil && st != nil {
			probeStatus, probeOK = st, true
			if h := truncateRunes(strings.TrimSpace(st.Hostname), 64); h != "" {
				name = h
			}
		}
		if name == "" {
			if _, hostport, aerr := ParseAddress(address); aerr == nil {
				if host, _, herr := net.SplitHostPort(hostport); herr == nil {
					name = truncateRunes(host, 64)
				}
			}
		}
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
	// A pairing just proved the agent reachable with the fresh
	// token, so a flaky follow-up probe must not warn (and must
	// not suppress the frontend's post-create import scan): the
	// node is online by construction of the pairing itself.
	if paired {
		v.Online = true
		if probeOK {
			v.LastInboundCount = len(probeStatus.Inbounds)
			v.LastVersion = probeStatus.Version
		}
	} else if nameWasEmpty {
		if probeOK {
			v.Online = true
			v.LastInboundCount = len(probeStatus.Inbounds)
			v.LastVersion = probeStatus.Version
		} else {
			// Same warning as the post-insert probe below, without
			// probing a second time.
			v.Warning = "节点暂时无法联系，创建已保存，后台会继续探测"
		}
	} else {
		// Creation-time reachability probe (capped at 3s so a dead
		// node does not stall creation): a failure only warns — the
		// node is still registered and the background poller keeps
		// probing it.
		probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		if st, err := s.client.Status(probeCtx, r.Address, r.Token); err != nil {
			v.Warning = "节点暂时无法联系，创建已保存，后台会继续探测"
		} else {
			v.Online = true
			v.LastInboundCount = len(st.Inbounds)
			v.LastVersion = st.Version
		}
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
	// Re-pairing first: the code is exchanged against the effective
	// address (the new address when one is given, else the stored
	// one) before any old-token use. The returned token replaces
	// the stored one below, and the manual Token field is ignored.
	var pairedToken string
	if req.PairingCode != nil && strings.TrimSpace(*req.PairingCode) != "" {
		effAddr := r.Address
		if req.Address != nil {
			a, err := validateAddress(*req.Address)
			if err != nil {
				return NodeView{}, err
			}
			effAddr = a
		}
		t, err := s.pairForAddress(ctx, effAddr, strings.TrimSpace(*req.PairingCode))
		if err != nil {
			return NodeView{}, err
		}
		pairedToken = t
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
	if pairedToken != "" {
		if pairedToken != r.Token {
			tokenChanged = true
		}
		r.Token = pairedToken
	} else if req.Token != nil {
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
	// Pausing never resets push state: the hash is kept, so the
	// poller pushes the empty payload on pause (its hash differs)
	// and the normal payload again on enable (when it differs from
	// the paused hash). Flipping the flag only clears a stale push
	// backoff, so the stop/restore push is not delayed by it.
	pauseChanged := req.Paused != nil && *req.Paused != r.Paused
	if req.Paused != nil {
		r.Paused = *req.Paused
	}
	now := time.Now().Unix()
	r.UpdatedAt = now
	if err := s.st.UpdateNode(ctx, *r); err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			return NodeView{}, errConflict("node name already exists")
		}
		return NodeView{}, err
	}
	if pauseChanged {
		if err := s.st.ClearNodeNextRetry(ctx, id); err != nil {
			return NodeView{}, err
		}
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

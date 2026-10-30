// Package render generates subscription content (Clash YAML / sing-box
// JSON) from a subscription, its template's inbounds and the global
// settings, and serves it on the public /sub endpoint. It is a pure
// read path: nothing here writes to the store or the core.
package render

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"singbox-ui/server/internal/settings"
	"singbox-ui/server/internal/store"
	"singbox-ui/server/internal/trojanpass"
)

// Error mirrors the other modules' typed API failure.
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

func errUnavailable(msg string) *Error {
	return &Error{Status: 403, Code: "subscription_unavailable", Message: msg}
}

type Service struct {
	st       *store.Store
	settings *settings.Service
}

func NewService(st *store.Store, settingsSvc *settings.Service) *Service {
	return &Service{st: st, settings: settingsSvc}
}

// --- node model ---------------------------------------------------------------

// Node is one client proxy entry derived from one inbound. Credential
// semantics mirror the domain model: vless/tuic nodes carry the
// SUBSCRIPTION's uuid; password protocols carry the inbound's shared
// password (per-subscription users for them arrive with the sync
// engine).
type Node struct {
	Name      string
	Protocol  string
	Server    string
	Port      int
	InboundID int64 // template inbound this node came from; group mapping only

	UUID string // vless, tuic

	// vless-reality
	Flow      string
	SNI       string
	PublicKey string
	ShortID   string

	// password protocols (hysteria2, tuic, anytls, trojan)
	Password string

	// hysteria2
	UpMbps       int
	DownMbps     int
	Obfs         string
	ObfsPassword string

	// tuic
	Congestion string

}

// Local mirrors of the inbound params JSON (the params blob is the
// contract between the inbounds module and this one).
type realityParams struct {
	SNI       string `json:"sni"`
	PublicKey string `json:"publicKey"`
	ShortID   string `json:"shortId"`
	Flow      string `json:"flow"`
}

type hy2Params struct {
	UpMbps       int    `json:"upMbps"`
	DownMbps     int    `json:"downMbps"`
	Password     string `json:"password"`
	Obfs         string `json:"obfs"`
	ObfsPassword string `json:"obfsPassword"`
}

type tuicParams struct {
	Password          string `json:"password"`
	CongestionControl string `json:"congestionControl"`
	SNI               string `json:"sni"`
}

type passwordSNIParams struct {
	Password string `json:"password"`
	SNI      string `json:"sni"`
}

func (s *Service) buildAllNodes(ctx context.Context, sub store.SubscriptionRow, host string) ([]Node, error) {
	tpl, err := s.st.GetTemplate(ctx, sub.TemplateID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errNotFound("template")
		}
		return nil, err
	}
	var ids []int64
	if err := json.Unmarshal([]byte(tpl.InboundIDs), &ids); err != nil {
		ids = []int64{}
	}
	rows, err := s.st.ListInbounds(ctx)
	if err != nil {
		return nil, err
	}
	byID := map[int64]store.InboundRow{}
	for _, r := range rows {
		byID[r.ID] = r
	}

	nodes := []Node{}
	usedNames := map[string]int{}
	add := func(name string, n Node) {
		usedNames[name]++
		if usedNames[name] > 1 {
			name = fmt.Sprintf("%s-%d", name, usedNames[name])
		}
		n.Name = name
		nodes = append(nodes, n)
	}
	// Local nodes first (historical order and naming, unchanged).
	// A nodes-only inbound (run_local off) has no local endpoint.
	for _, id := range ids {
		ib, ok := byID[id]
		if !ok || !ib.Enabled || !ib.RunLocal {
			continue
		}
		n, ok := nodeForInbound(ib, sub, host, ib.Port)
		if !ok {
			continue
		}
		n.InboundID = id
		add(ib.Name, n)
	}
	// Remote nodes: one endpoint per (online, unpaused node, assigned inbound in
	// this template). Paused nodes are excluded even when online: pausing
	// only hides them from subscriptions, it deletes nothing. The name
	// is node-first, matching the local prefix-first convention; the shared usedNames map means a local
	// name can never collide with a remote name in one subscription.
	// Credentials are identical to local (one subscription UUID works
	// on every node); only the server address changes.
	remoteNodes, err := s.st.ListNodes(ctx)
	if err != nil {
		return nil, err
	}
	if len(remoteNodes) > 0 {
		assignments, err := s.st.AllInboundNodes(ctx)
		if err != nil {
			return nil, err
		}
		// node id -> inbound id -> per-node port override (0 = the
		// inbound's own port).
		byNode := map[int64]map[int64]int{}
		for _, a := range assignments {
			if byNode[a.NodeID] == nil {
				byNode[a.NodeID] = map[int64]int{}
			}
			byNode[a.NodeID][a.InboundID] = a.Port
		}
		now := time.Now().Unix()
		for _, rn := range remoteNodes {
			if rn.Paused || !rn.Online(now) {
				continue
			}
			assigned := byNode[rn.ID]
			if len(assigned) == 0 {
				continue
			}
			remoteHost := remoteHost(rn)
			for _, id := range ids {
				port, ok := assigned[id]
				if !ok {
					continue
				}
				ib, ok := byID[id]
				if !ok || !ib.Enabled {
					continue
				}
				if port <= 0 {
					port = ib.Port
				}
				n, ok := nodeForInbound(ib, sub, remoteHost, port)
				if !ok {
					continue
				}
				n.InboundID = id
				add(rn.Name+"-"+ib.Name, n)
			}
		}
	}
	return nodes, nil
}

// nodeForInbound converts one panel inbound to a client proxy entry.
// The server address (host) is the caller's choice: the local host for
// local nodes, the node's public host for remote nodes. hysteria2 takes
// its SNI from that host (it has no SNI param); the other protocols
// take SNI from their params, identical on every node.
func nodeForInbound(ib store.InboundRow, sub store.SubscriptionRow, host string, port int) (Node, bool) {
	n := Node{Protocol: ib.Protocol, Server: host, Port: port}
	switch ib.Protocol {
	case "vless-reality":
		var p realityParams
		_ = json.Unmarshal([]byte(ib.Params), &p)
		n.UUID = sub.UUID
		n.Flow = p.Flow
		n.SNI = p.SNI
		n.PublicKey = p.PublicKey
		n.ShortID = p.ShortID
	case "hysteria2":
		var p hy2Params
		_ = json.Unmarshal([]byte(ib.Params), &p)
		n.Password = p.Password
		n.UpMbps = p.UpMbps
		n.DownMbps = p.DownMbps
		n.Obfs = p.Obfs
		n.ObfsPassword = p.ObfsPassword
		n.SNI = host
	case "tuic":
		var p tuicParams
		_ = json.Unmarshal([]byte(ib.Params), &p)
		n.UUID = sub.UUID
		n.Password = p.Password
		n.Congestion = p.CongestionControl
		if n.Congestion == "" {
			n.Congestion = "bbr"
		}
		n.SNI = p.SNI
	case "anytls":
		var p passwordSNIParams
		_ = json.Unmarshal([]byte(ib.Params), &p)
		n.Password = p.Password
		n.SNI = p.SNI
	case "trojan":
		var p passwordSNIParams
		_ = json.Unmarshal([]byte(ib.Params), &p)
		n.Password = trojanpass.Password(p.Password, sub.UUID)
		n.SNI = p.SNI
	default:
		return Node{}, false
	}
	return n, true
}

// remoteHost resolves a node's subscription-facing host: the configured
// public host, or the management address host when none is set. It
// mirrors the nodes package's resolution (kept local so render stays
// independent of the nodes module).
func remoteHost(rn store.NodeRow) string {
	if strings.TrimSpace(rn.PublicHost) != "" {
		return strings.TrimSpace(rn.PublicHost)
	}
	addr := strings.TrimSpace(rn.Address)
	addr = strings.TrimPrefix(addr, "http://")
	addr = strings.TrimPrefix(addr, "https://")
	addr = strings.TrimSuffix(addr, "/")
	if h, _, err := net.SplitHostPort(addr); err == nil {
		return h
	}
	return addr
}

// --- status -------------------------------------------------------------------

// statusOf mirrors the subs module's derived status; render keeps its
// own copy so the modules stay independent.
func statusOf(r store.SubscriptionRow, now int64) string {
	switch {
	case !r.Enabled:
		return "disabled"
	case r.ExpireAt > 0 && r.ExpireAt < now:
		return "expired"
	case r.LimitBytes > 0 && r.UsedUp+r.UsedDown >= r.LimitBytes:
		return "over_quota"
	default:
		return "active"
	}
}

// --- rendering ------------------------------------------------------------------

type Result struct {
	Body        string
	ContentType string
	Format      string // clash | singbox

	SubName    string
	ShowInfo   bool
	UsedUp     int64
	UsedDown   int64
	LimitBytes int64
	ExpireAt   int64
	UpdateHrs  int
}

// Render builds the subscription document. format "" resolves to the
// settings default; reqHost supplies the node server address when no
// public host is configured.
func (s *Service) Render(ctx context.Context, sub store.SubscriptionRow, format, reqHost string) (Result, error) {
	cfg, err := s.settings.Get(ctx)
	if err != nil {
		return Result{}, err
	}
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "sing-box" {
		format = "singbox"
	}
	if format == "" {
		format = cfg.LinkFormat
	}
	if format != "clash" && format != "singbox" {
		return Result{}, errValidation("format must be clash or singbox")
	}

	host := cfg.PublicHost
	if host == "" {
		host = stripPort(reqHost)
	}

	nodes, err := s.buildAllNodes(ctx, sub, host)
	if err != nil {
		return Result{}, err
	}

	// Proxy groups for Clash: effective template groups (defaults when
	// the template stores none), each mapped to the generated node
	// names whose inbound is in the group. A group with no generated
	// nodes is skipped, so a group over a disabled/paused inbound
	// simply disappears instead of rendering an empty group.
	var clashGroups []ClashGroup
	if tpl, terr := s.st.GetTemplate(ctx, sub.TemplateID); terr == nil {
		var tplIDs []int64
		_ = json.Unmarshal([]byte(tpl.InboundIDs), &tplIDs)
		for _, g := range store.EffectiveProxyGroups(tpl.Groups, tplIDs) {
			if !g.Enabled {
				continue
			}
			want := map[int64]bool{}
			for _, id := range g.InboundIDs {
				want[id] = true
			}
			names := []string{}
			seenNames := map[string]bool{}
			for _, n := range nodes {
				if want[n.InboundID] && !seenNames[n.Name] {
					seenNames[n.Name] = true
					names = append(names, n.Name)
				}
			}
			if len(names) == 0 {
				continue
			}
			clashGroups = append(clashGroups, ClashGroup{
				Name: g.Name, Type: g.Type, Proxies: names,
				URL: g.URL, Interval: g.Interval, Tolerance: g.Tolerance,
			})
		}
	}
	if len(clashGroups) == 0 {
		// Fallback: never render a subscription without a group.
		all := []string{}
		for _, n := range nodes {
			all = append(all, n.Name)
		}
		if len(all) > 0 {
			clashGroups = []ClashGroup{{Name: "Proxy", Type: "select", Proxies: all}}
		}
	}

	res := Result{
		Format:     format,
		SubName:    sub.Name,
		ShowInfo:   cfg.ShowUserinfo,
		UsedUp:     sub.UsedUp,
		UsedDown:   sub.UsedDown,
		LimitBytes: sub.LimitBytes,
		ExpireAt:   sub.ExpireAt,
		UpdateHrs:  cfg.UpdateIntervalHrs,
	}
	switch format {
	case "clash":
		res.Body = renderClash(nodes, clashGroups, cfg, host)
		res.ContentType = "text/yaml; charset=utf-8"
	case "singbox":
		body, err := renderSingbox(nodes, cfg, host)
		if err != nil {
			return Result{}, err
		}
		res.Body = body
		res.ContentType = "application/json; charset=utf-8"
	}
	return res, nil
}

// RenderByToken is the public /sub path: it enforces the derived
// subscription status before rendering.
func (s *Service) RenderByToken(ctx context.Context, token, format, reqHost string) (Result, error) {
	sub, err := s.st.GetSubscriptionByToken(ctx, token)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Result{}, errNotFound("subscription")
		}
		return Result{}, err
	}
	switch statusOf(*sub, time.Now().Unix()) {
	case "active":
		return s.Render(ctx, *sub, format, reqHost)
	case "disabled":
		return Result{}, errUnavailable("subscription is disabled")
	case "expired":
		return Result{}, errUnavailable("subscription has expired")
	default:
		return Result{}, errUnavailable("subscription is over quota")
	}
}

// RenderByID is the admin preview path: it renders regardless of the
// subscription's status.
func (s *Service) RenderByID(ctx context.Context, id int64, format, reqHost string) (Result, error) {
	sub, err := s.st.GetSubscription(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Result{}, errNotFound("subscription")
		}
		return Result{}, err
	}
	return s.Render(ctx, *sub, format, reqHost)
}

func stripPort(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}

func isIP(host string) bool {
	return net.ParseIP(host) != nil
}

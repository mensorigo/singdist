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

	"singbox-ui/server/internal/store"
	"singbox-ui/server/internal/trojanpass"
)

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

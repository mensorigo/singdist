package inbounds

import (
	"context"
	"crypto/ecdh"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"singbox-ui/server/internal/store"
)

// RunningInbound is one inbound found in the core's live config file.
// Secret material (private keys, passwords, uuids) is extracted for
// comparison and import only and never serialised to the API.
type RunningInbound struct {
	Tag          string `json:"tag"`
	Type         string `json:"type"` // sing-box type: vless | hysteria2 | tuic | anytls | trojan
	Port         int    `json:"port"`
	SNI          string `json:"sni,omitempty"`
	PublicKey    string `json:"publicKey,omitempty"`
	PrivateKey   string `json:"-"`
	ShortID      string `json:"shortId,omitempty"`
	UpMbps       int    `json:"upMbps,omitempty"`
	DownMbps     int    `json:"downMbps,omitempty"`
	Congestion   string `json:"-"`                // tuic congestion_control
	UUID         string `json:"-"`                // tuic first user
	Password     string `json:"-"`                // first user's / inbound password, import+compare only
	Flow         string `json:"-"`                // vless first user's flow
	Obfs         string `json:"-"`                // hysteria2 obfs type
	ObfsPassword string `json:"-"`                // hysteria2 obfs password
}

// coreInbound is one inbound object as it appears in a sing-box core
// config file. It is the shared shape for ReadRunning (whole file)
// and ParseCoreInbounds (a bare inbound array from a node agent), so
// local and node imports parse identically.
type coreInbound struct {
	Type       string `json:"type"`
	Tag        string `json:"tag"`
	ListenPort int    `json:"listen_port"`
	UpMbps     int    `json:"up_mbps"`
	DownMbps   int    `json:"down_mbps"`
	Password   string `json:"password"`           // inbound-level password
	Congestion string `json:"congestion_control"` // tuic
	Obfs       *struct {
		Type     string `json:"type"`
		Password string `json:"password"`
	} `json:"obfs"` // hysteria2
	Users []struct {
		UUID     string `json:"uuid"`
		Password string `json:"password"`
		Flow     string `json:"flow"` // vless per-user flow
	} `json:"users"`
	TLS *struct {
		ServerName string `json:"server_name"`
		Reality    *struct {
			PrivateKey string   `json:"private_key"`
			ShortID    []string `json:"short_id"`
		} `json:"reality"`
	} `json:"tls"`
}

type rawConfig struct {
	Inbounds []coreInbound `json:"inbounds"`
}

// parseCoreInbound maps one core inbound object to a RunningInbound.
// The second return is false when the type is not a panel-managed
// protocol (or empty): direct/block/etc. are dropped, never imported.
func parseCoreInbound(in coreInbound) (RunningInbound, bool) {
	protocol := ProtocolOf(in.Type)
	if _, managed := protocolSpecs[protocol]; !managed || in.Type == "" {
		return RunningInbound{}, false
	}
	r := RunningInbound{
		Tag: in.Tag, Type: in.Type, Port: in.ListenPort,
		UpMbps: in.UpMbps, DownMbps: in.DownMbps,
		Congestion: in.Congestion,
	}
	if in.Obfs != nil {
		r.Obfs = in.Obfs.Type
		r.ObfsPassword = in.Obfs.Password
	}
	if in.TLS != nil {
		r.SNI = in.TLS.ServerName
		if in.TLS.Reality != nil {
			r.PrivateKey = in.TLS.Reality.PrivateKey
			if len(in.TLS.Reality.ShortID) > 0 {
				r.ShortID = in.TLS.Reality.ShortID[0]
			}
			if priv, err := base64.RawURLEncoding.DecodeString(r.PrivateKey); err == nil && len(priv) == 32 {
				if key, err := ecdh.X25519().NewPrivateKey(priv); err == nil {
					r.PublicKey = base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes())
				}
			}
		}
	}
	if len(in.Users) > 0 {
		r.UUID = in.Users[0].UUID
		r.Flow = in.Users[0].Flow
		if r.Password == "" {
			r.Password = in.Users[0].Password
		}
	}
	return r, true
}

// ReadRunning parses the sing-box config file read-only. ok=false means
// the file could not be read or parsed (comparison unavailable).
func ReadRunning(path string) (list []RunningInbound, ok bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var cfg rawConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, false
	}
	out := []RunningInbound{}
	for _, in := range cfg.Inbounds {
		if r, managed := parseCoreInbound(in); managed {
			out = append(out, r)
		}
	}
	return out, true
}

// ParseCoreInbounds parses a bare array of core inbound objects (as
// returned by a node agent's /api/agent/inbounds) into RunningInbound
// records. Unsupported types are dropped silently — the caller only
// ever sees panel-managed protocols. Secret fields stay in the
// json:"-" fields and are never serialised.
func ParseCoreInbounds(data json.RawMessage) ([]RunningInbound, error) {
	var inbounds []coreInbound
	if err := json.Unmarshal(data, &inbounds); err != nil {
		return nil, err
	}
	out := []RunningInbound{}
	for _, in := range inbounds {
		if r, managed := parseCoreInbound(in); managed {
			out = append(out, r)
		}
	}
	return out, nil
}

// ProtocolOf maps a sing-box inbound type to the panel's protocol key.
// Unknown types map to themselves and are filtered out by the registry
// lookup in ReadRunning / ParseCoreInbounds.
func ProtocolOf(runningType string) string {
	switch runningType {
	case "vless":
		return ProtocolVLESSReality
	case "hysteria2":
		return ProtocolHysteria2
	case "tuic":
		return ProtocolTUIC
	case "anytls":
		return ProtocolAnyTLS
	case "trojan":
		return ProtocolTrojan
	}
	return runningType
}

// MapRunning converts a parsed core inbound to its panel protocol +
// params JSON. ok=false when the type is not a panel-managed protocol.
// This is the single entry point for import mapping (local and node):
// params carry the core's values verbatim (no users — those come from
// subscription templates later, never from the imported config).
func MapRunning(r *RunningInbound) (protocol, params string, ok bool) {
	protocol = ProtocolOf(r.Type)
	spec, ok := protocolSpecs[protocol]
	if !ok {
		return "", "", false
	}
	return protocol, spec.importParams(r), true
}

// matchRunning finds the running inbound corresponding to a stored row:
// same protocol family and same listen port.
func matchRunning(row store.InboundRow, running []RunningInbound) *RunningInbound {
	for i := range running {
		if running[i].Port == row.Port && ProtocolOf(running[i].Type) == row.Protocol {
			return &running[i]
		}
	}
	return nil
}

// compareOf compares a stored row's params with its running counterpart
// through the protocol registry and reports the status plus the
// field-level differences (panel value vs running value) when drifted.
func compareOf(row store.InboundRow, m *RunningInbound) (string, []Diff) {
	if m == nil {
		return "pending", nil
	}
	spec, ok := protocolSpecs[row.Protocol]
	if !ok {
		return "drifted", nil
	}
	diffs := spec.compare(row.Params, m)
	if len(diffs) > 0 {
		return "drifted", diffs
	}
	return "synced", nil
}

// nodeAgentSnapshot is the part of a node's stored agent-status
// snapshot that decides one inbound's sync state. It mirrors the
// nodes package's AgentStatus shape without importing it.
type nodeAgentSnapshot struct {
	LastResult  string `json:"lastResult"`
	BlockedHash string `json:"blockedHash"`
	Inbounds    []struct {
		Name      string `json:"name"`
		Port      int    `json:"port"`
		Listening *bool  `json:"listening"`
	} `json:"inbounds"`
}

// nodeInboundStatus decides one node-only inbound's status on one
// node, with the same criteria as the node card's synced check,
// scoped to this single inbound: push succeeded, the pushed content
// matches what the panel wants now, the agent reports this inbound,
// and it is listening. Paused and offline are their own statuses,
// not sync failures, so a paused node's inbounds never read as
// "synced" and never read as "out of sync" either.
func nodeInboundStatus(row store.InboundRow, asg store.InboundNodeRow, n store.NodeRow, now int64) string {
	if n.Paused {
		return "paused"
	}
	if !n.Online(now) {
		return "offline"
	}
	if n.AgentStatus == "" || n.AgentStatusAt == 0 || now-n.AgentStatusAt > store.OnlineWithin {
		return "unknown"
	}
	if n.LastError != "" {
		return "pending"
	}
	if n.LastPushResult != "" && n.LastPushResult != "applied" && n.LastPushResult != "no_change" {
		return "pending"
	}
	if n.DesiredPushHash == "" {
		return "unknown"
	}
	if n.DesiredPushHash != n.LastPushHash {
		return "pending"
	}
	var snap nodeAgentSnapshot
	if err := json.Unmarshal([]byte(n.AgentStatus), &snap); err != nil {
		return "unknown"
	}
	if snap.BlockedHash != "" {
		return "pending"
	}
	if snap.LastResult != "" && snap.LastResult != "applied" && snap.LastResult != "no_change" {
		return "pending"
	}
	wantPort := asg.Port
	if wantPort == 0 {
		wantPort = row.Port
	}
	for _, ib := range snap.Inbounds {
		if ib.Name != row.Name {
			continue
		}
		if ib.Port != wantPort {
			return "drifted"
		}
		if ib.Listening == nil || !*ib.Listening {
			return "not_listening"
		}
		return "synced"
	}
	return "pending"
}

// nodeInboundStatusAll aggregates one node-only inbound across its
// assigned nodes (normally exactly one under the single-select
// model). All nodes synced means synced; otherwise the most severe
// non-synced status wins, so a multi-node inbound is never reported
// synced while any node is behind.
func nodeInboundStatusAll(row store.InboundRow, asgs []store.InboundNodeRow, nodes map[int64]store.NodeRow, now int64) string {
	if len(asgs) == 0 {
		// Node-only but assigned nowhere: it runs on no machine
		// and is pushed nowhere, so it is not in sync anywhere.
		return "pending"
	}
	severity := map[string]int{
		"pending": 0, "drifted": 1, "not_listening": 2,
		"paused": 3, "offline": 4, "unknown": 5, "synced": 6,
	}
	best := "synced"
	for _, asg := range asgs {
		n, ok := nodes[asg.NodeID]
		if !ok {
			if severity["unknown"] < severity[best] {
				best = "unknown"
			}
			continue
		}
		st := nodeInboundStatus(row, asg, n, now)
		if severity[st] < severity[best] {
			best = st
		}
	}
	return best
}

// List returns every stored inbound with its comparison status, plus any
// running inbounds the panel does not manage yet.
func (s *Service) List(ctx context.Context) (ListResult, error) {
	rows, err := s.st.ListInbounds(ctx)
	if err != nil {
		return ListResult{}, err
	}
	running, available := ReadRunning(s.configPath)
	// Node data for node-only inbounds only; a failure here must not
	// break the local list, so node inbounds fall back to unknown.
	now := nowUnix()
	nodesByID := map[int64]store.NodeRow{}
	asgsByInbound := map[int64][]store.InboundNodeRow{}
	if ns, err := s.st.ListNodes(ctx); err == nil {
		for _, n := range ns {
			nodesByID[n.ID] = n
		}
	}
	if asgs, err := s.st.AllInboundNodes(ctx); err == nil {
		for _, a := range asgs {
			asgsByInbound[a.InboundID] = append(asgsByInbound[a.InboundID], a)
		}
	}
	res := ListResult{Items: []View{}, RunningAvailable: available, Extras: []RunningInbound{}}
	matched := map[int]bool{}
	for _, row := range rows {
		v := View{
			ID: row.ID, Name: row.Name, Protocol: row.Protocol, Port: row.Port,
			Params: json.RawMessage(row.Params), Enabled: row.Enabled,
			RunLocal:  row.RunLocal,
			CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
			Status: "unknown",
		}
		if !row.RunLocal {
			// Nodes-only: the local core never runs it, so there
			// is no local state to compare against — and it must
			// not match a different local inbound that happens to
			// share its port. Its status comes from the assigned
			// node's push + agent snapshot instead, scoped to
			// this one inbound.
			v.Status = nodeInboundStatusAll(row, asgsByInbound[row.ID], nodesByID, now)
		} else if available {
			m := matchRunning(row, running)
			if m != nil {
				matched[m.Port] = true
				v.Status, v.Diffs = compareOf(row, m)
			} else {
				v.Status = "pending"
			}
		}
		if !row.Enabled {
			// A parked inbound's drift is not actionable.
			v.Status = "disabled"
			v.Diffs = nil
		}
		res.Items = append(res.Items, v)
	}
	if available {
		for _, r := range running {
			if !matched[r.Port] {
				res.Extras = append(res.Extras, r)
			}
		}
	}
	return res, nil
}

// ImportIfEmpty seeds the store from the running config when the panel
// has no inbounds yet, so the page starts from reality instead of empty.
// Returns how many inbounds were imported.
func (s *Service) ImportIfEmpty(ctx context.Context, log *slog.Logger) int {
	n, err := s.st.CountInbounds(ctx)
	if err != nil || n > 0 {
		return 0
	}
	running, ok := ReadRunning(s.configPath)
	if !ok {
		return 0
	}
	imported := 0
	for _, r := range running {
		name := r.Tag
		if name == "" {
			name = fmt.Sprintf("%s-%d", r.Type, r.Port)
		}
		protocol := ProtocolOf(r.Type)
		spec, ok := protocolSpecs[protocol]
		if !ok {
			continue
		}
		params := spec.importParams(&r)
		if _, err := s.st.CreateInbound(ctx, name, protocol, r.Port, params, true, nowUnix()); err != nil {
			log.Warn("inbounds: import from running config failed", "tag", r.Tag, "err", err)
			continue
		}
		imported++
	}
	if imported > 0 {
		log.Info("inbounds: imported from running config", "count", imported)
	}
	return imported
}

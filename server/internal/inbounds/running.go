package inbounds

import (
	"crypto/ecdh"
	"encoding/base64"
	"encoding/json"
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
	Congestion   string `json:"-"` // tuic congestion_control
	UUID         string `json:"-"` // tuic first user
	Password     string `json:"-"` // first user's / inbound password, import+compare only
	Flow         string `json:"-"` // vless first user's flow
	Obfs         string `json:"-"` // hysteria2 obfs type
	ObfsPassword string `json:"-"` // hysteria2 obfs password
}

// coreInbound is one inbound object as it appears in a sing-box core
// config file. It is the shared shape for ReadRunning (whole file)
// and ParseCoreInbounds (a bare inbound array from a node agent), so
// local and node imports parse identically.
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

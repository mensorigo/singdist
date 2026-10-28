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
	Tag        string `json:"tag"`
	Type       string `json:"type"` // sing-box type: vless | hysteria2 | tuic | shadowsocks | anytls | trojan
	Port       int    `json:"port"`
	SNI        string `json:"sni,omitempty"`
	PublicKey  string `json:"publicKey,omitempty"`
	PrivateKey string `json:"-"`
	ShortID    string `json:"shortId,omitempty"`
	UpMbps     int    `json:"upMbps,omitempty"`
	DownMbps   int    `json:"downMbps,omitempty"`
	Method     string `json:"method,omitempty"` // shadowsocks
	Congestion string `json:"-"`                // tuic congestion_control
	UUID       string `json:"-"`                // tuic first user
	Password   string `json:"-"`                // first user's / inbound password, import+compare only
}

type rawConfig struct {
	Inbounds []struct {
		Type       string `json:"type"`
		Tag        string `json:"tag"`
		ListenPort int    `json:"listen_port"`
		UpMbps     int    `json:"up_mbps"`
		DownMbps   int    `json:"down_mbps"`
		Method     string `json:"method"`     // shadowsocks
		Password   string `json:"password"`   // shadowsocks server key
		Congestion string `json:"congestion_control"` // tuic
		Users      []struct {
			UUID     string `json:"uuid"`
			Password string `json:"password"`
		} `json:"users"`
		TLS *struct {
			ServerName string `json:"server_name"`
			Reality    *struct {
				PrivateKey string   `json:"private_key"`
				ShortID    []string `json:"short_id"`
			} `json:"reality"`
		} `json:"tls"`
	} `json:"inbounds"`
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
		if _, managed := protocolSpecs[protocolOf(in.Type)]; !managed || in.Type == "" {
			continue // direct/block/etc. are not panel-managed inbounds
		}
		r := RunningInbound{
			Tag: in.Tag, Type: in.Type, Port: in.ListenPort,
			UpMbps: in.UpMbps, DownMbps: in.DownMbps,
			Method: in.Method, Congestion: in.Congestion,
		}
		if in.Type == "shadowsocks" {
			r.Password = in.Password
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
			if r.Password == "" {
				r.Password = in.Users[0].Password
			}
		}
		out = append(out, r)
	}
	return out, true
}

// protocolOf maps a sing-box inbound type to the panel's protocol key.
// Unknown types map to themselves and are filtered out by the registry
// lookup in ReadRunning.
func protocolOf(runningType string) string {
	switch runningType {
	case "vless":
		return ProtocolVLESSReality
	case "hysteria2":
		return ProtocolHysteria2
	case "tuic":
		return ProtocolTUIC
	case "shadowsocks":
		return ProtocolShadowsocks
	case "anytls":
		return ProtocolAnyTLS
	case "trojan":
		return ProtocolTrojan
	}
	return runningType
}

// matchRunning finds the running inbound corresponding to a stored row:
// same protocol family and same listen port.
func matchRunning(row store.InboundRow, running []RunningInbound) *RunningInbound {
	for i := range running {
		if running[i].Port == row.Port && protocolOf(running[i].Type) == row.Protocol {
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

// List returns every stored inbound with its comparison status, plus any
// running inbounds the panel does not manage yet.
func (s *Service) List(ctx context.Context) (ListResult, error) {
	rows, err := s.st.ListInbounds(ctx)
	if err != nil {
		return ListResult{}, err
	}
	running, available := ReadRunning(s.configPath)
	res := ListResult{Items: []View{}, RunningAvailable: available, Extras: []RunningInbound{}}
	matched := map[int]bool{}
	for _, row := range rows {
		v := View{
			ID: row.ID, Name: row.Name, Protocol: row.Protocol, Port: row.Port,
			Params: json.RawMessage(row.Params), Enabled: row.Enabled,
			RunLocal: row.RunLocal,
			CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
			Status: "unknown",
		}
		if !row.RunLocal {
			// Nodes-only: the local core never runs it, so there
			// is no local state to compare against — and it must
			// not match a different local inbound that happens to
			// share its port.
			v.Status = "remote"
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
		protocol := protocolOf(r.Type)
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

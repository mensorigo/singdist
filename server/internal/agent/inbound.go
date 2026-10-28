// Package agent is the standalone node agent: it receives inbound
// definitions pushed from the central panel, turns them into a
// sing-box core config with its own inbound logic, and applies that
// config to the local core (check, atomic write, restart with
// rollback, health check). It deliberately has no UI, no
// subscription system, no user database and no certificate store:
// the central panel owns all of those, and this package only depends
// on protocol definitions and validation rules.
//
// Validation here mirrors the panel's rules (same messages, same
// boundaries) with one deliberate difference: the agent never
// invents secrets. The panel auto-generates missing passwords,
// keypairs and UUIDs at create time because it owns the record;
// the agent must reject an incomplete push instead, otherwise the
// central panel and the agent would disagree on the secret and the
// subscription rendered from the central record would be wrong.
// Deterministic defaults that are not secrets (flow, congestion,
// shadowsocks method) are still applied, exactly like the panel.
package agent

import (
	"crypto/ecdh"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	ProtocolVLESSReality = "vless-reality"
	ProtocolHysteria2    = "hysteria2"
	ProtocolTUIC         = "tuic"
	ProtocolShadowsocks  = "shadowsocks"
	ProtocolAnyTLS       = "anytls"
	ProtocolTrojan       = "trojan"
)

// transport is a bitmask of the transports an inbound listens on.
// Two inbounds may share a port only when their transports do not
// overlap (TCP vs UDP): vless on 443/TCP and hy2 on 443/UDP coexist.
type transport uint8

const (
	transportTCP transport = 1 << iota
	transportUDP
)

func (t transport) label() string {
	switch t {
	case transportTCP:
		return "TCP"
	case transportUDP:
		return "UDP"
	default:
		return "TCP+UDP"
	}
}

var protocolTransports = map[string]transport{
	ProtocolVLESSReality: transportTCP,
	ProtocolHysteria2:    transportUDP,
	ProtocolTUIC:         transportUDP,
	ProtocolShadowsocks:  transportTCP | transportUDP,
	ProtocolAnyTLS:       transportTCP,
	ProtocolTrojan:       transportTCP,
}

// User is one core user attached to an inbound. For vless-reality
// and tuic the uuid is the credential; for hysteria2/anytls/trojan
// the password is inbound-level and shared, so only the name is
// used; shadowsocks has no per-user list at all.
type User struct {
	Name string `json:"name"`
	UUID string `json:"uuid,omitempty"`
}

// Inbound is one pushed (or local) inbound definition. Params is
// the protocol's params object, decoded per protocol during
// validation; it is kept as raw JSON so the push payload stays
// protocol-agnostic.
type Inbound struct {
	Name     string          `json:"name"`
	Protocol string          `json:"protocol"`
	Port     int             `json:"port"`
	Enabled  bool            `json:"enabled"`
	Params   json.RawMessage `json:"params"`
	Users    []User          `json:"users,omitempty"`
}

// Cert is the node's TLS certificate for the TLS-terminated
// protocols (hysteria2, tuic, anytls, trojan), pushed by the central
// panel together with the inbounds. The agent never manages
// certificate lifecycle; it only embeds what it was given.
type Cert struct {
	Certificate string `json:"certificate"`
	Key         string `json:"key"`
}

// Push is the body of POST /api/agent/sync. PublicHost is the
// node's public host, used as the hysteria2 TLS server name (the
// panel uses its own public-host setting for the same field).
type Push struct {
	Inbounds   []Inbound `json:"inbounds"`
	PublicHost string    `json:"publicHost,omitempty"`
	Cert       *Cert     `json:"cert,omitempty"`
}

// Error is a typed validation failure (HTTP 400).
type Error struct {
	Message string
}

func (e *Error) Error() string { return e.Message }

func errValidation(msg string) *Error { return &Error{Message: msg} }

// --- params ---------------------------------------------------------------

type realityParams struct {
	SNI        string `json:"sni"`
	PublicKey  string `json:"publicKey"`
	PrivateKey string `json:"privateKey"`
	ShortID    string `json:"shortId"`
	Flow       string `json:"flow"`
}

type hy2Params struct {
	UpMbps       int    `json:"upMbps"`
	DownMbps     int    `json:"downMbps"`
	Password     string `json:"password"`
	Obfs         string `json:"obfs"`
	ObfsPassword string `json:"obfsPassword"`
}

type tuicParams struct {
	UUID              string `json:"uuid"`
	Password          string `json:"password"`
	CongestionControl string `json:"congestionControl"`
	SNI               string `json:"sni"`
}

type ssParams struct {
	Method   string `json:"method"`
	Password string `json:"password"`
}

type passwordSNIParams struct {
	Password string `json:"password"`
	SNI      string `json:"sni"`
}

var ss2022KeyLen = map[string]int{
	"2022-blake3-aes-128-gcm":       16,
	"2022-blake3-aes-256-gcm":       32,
	"2022-blake3-chacha20-poly1305": 32,
}

func validUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
			if !isHex {
				return false
			}
		}
	}
	return true
}

func validRealityKey(s string) bool {
	b, err := base64.RawURLEncoding.DecodeString(s)
	return err == nil && len(b) == 32
}

// validateParams checks one inbound's params against its protocol
// and returns the canonical params JSON (defaults applied). No
// secrets are ever generated here.
func validateParams(protocol string, raw json.RawMessage) (json.RawMessage, error) {
	switch protocol {
	case ProtocolVLESSReality:
		var p realityParams
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, errValidation("invalid reality params")
			}
		}
		p.SNI = strings.TrimSpace(p.SNI)
		if p.SNI == "" {
			return nil, errValidation("reality sni is required")
		}
		if !validRealityKey(p.PrivateKey) || !validRealityKey(p.PublicKey) {
			return nil, errValidation("reality keys must be 32-byte base64url values")
		}
		priv, err := ecdh.X25519().NewPrivateKey(mustDecode(p.PrivateKey))
		if err != nil || base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes()) != p.PublicKey {
			return nil, errValidation("reality public key does not match the private key")
		}
		if p.ShortID != "" {
			if len(p.ShortID) > 16 || len(p.ShortID)%2 != 0 {
				return nil, errValidation("shortId must be even-length hex, at most 16 characters")
			}
			if _, err := hex.DecodeString(p.ShortID); err != nil {
				return nil, errValidation("shortId must be hexadecimal")
			}
		}
		if p.Flow == "" {
			p.Flow = "xtls-rprx-vision"
		}
		b, _ := json.Marshal(p)
		return b, nil
	case ProtocolHysteria2:
		var p hy2Params
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, errValidation("invalid hysteria2 params")
			}
		}
		if p.UpMbps < 0 || p.UpMbps > 100000 || p.DownMbps < 0 || p.DownMbps > 100000 {
			return nil, errValidation("bandwidth must be between 0 and 100000 Mbps")
		}
		if p.Password == "" {
			return nil, errValidation("hysteria2 password is required")
		}
		switch p.Obfs {
		case "":
			p.ObfsPassword = ""
		case "salamander":
			if p.ObfsPassword == "" {
				return nil, errValidation("obfs password is required when obfs is enabled")
			}
		default:
			return nil, errValidation("obfs must be empty or salamander")
		}
		b, _ := json.Marshal(p)
		return b, nil
	case ProtocolTUIC:
		var p tuicParams
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, errValidation("invalid tuic params")
			}
		}
		// The inbound-level uuid is validated but NOT used at
		// generation time (the core users carry subscription uuids);
		// this mirrors the panel's quirk exactly rather than
		// silently "fixing" it here.
		p.SNI = strings.TrimSpace(p.SNI)
		if !validUUID(p.UUID) {
			return nil, errValidation("tuic uuid must be a valid UUID")
		}
		if p.Password == "" {
			return nil, errValidation("tuic password is required")
		}
		if p.CongestionControl == "" {
			p.CongestionControl = "bbr"
		}
		switch p.CongestionControl {
		case "cubic", "new_reno", "bbr":
		default:
			return nil, errValidation("congestionControl must be cubic, new_reno or bbr")
		}
		b, _ := json.Marshal(p)
		return b, nil
	case ProtocolShadowsocks:
		var p ssParams
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, errValidation("invalid shadowsocks params")
			}
		}
		if p.Method == "" {
			p.Method = "2022-blake3-aes-128-gcm"
		}
		if keyLen, is2022 := ss2022KeyLen[p.Method]; is2022 {
			b, err := decodeSSKey(p.Password)
			if err != nil || len(b) != keyLen {
				return nil, errValidation(fmt.Sprintf(
					"shadowsocks %s key must be a %d-byte base64 value", p.Method, keyLen))
			}
			out, _ := json.Marshal(p)
			return out, nil
		}
		// Any other method string is stored verbatim (tier 2); the
		// core's own check at apply time is the final validator
		// for a bogus value (tier 3).
		if p.Password == "" {
			return nil, errValidation("shadowsocks password is required")
		}
		out, _ := json.Marshal(p)
		return out, nil
	case ProtocolAnyTLS, ProtocolTrojan:
		var p passwordSNIParams
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, errValidation("invalid " + protocol + " params")
			}
		}
		p.Password = strings.TrimSpace(p.Password)
		if p.Password == "" {
			return nil, errValidation(protocol + " password is required")
		}
		p.SNI = strings.TrimSpace(p.SNI)
		if strings.ContainsAny(p.SNI, " /") {
			return nil, errValidation("sni must be a host name")
		}
		out, _ := json.Marshal(p)
		return out, nil
	default:
		return nil, errValidation("unsupported protocol")
	}
}

func mustDecode(s string) []byte {
	b, _ := base64.RawURLEncoding.DecodeString(s)
	return b
}

func decodeSSKey(s string) ([]byte, error) {
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.RawStdEncoding.DecodeString(s)
}

// --- inbound set validation -----------------------------------------------

// validateInbounds checks a whole inbound set: names, ports,
// reserved ports, per-protocol params, users, duplicate names and
// per-transport port conflicts. It returns the set with canonical
// params. Reserved ports are supplied by the caller because the
// agent's own management port is configurable.
func validateInbounds(in []Inbound, reserved map[int]string) ([]Inbound, error) {
	out := make([]Inbound, 0, len(in))
	seenNames := map[string]bool{}
	for _, ib := range in {
		name := strings.TrimSpace(ib.Name)
		if name == "" || len([]rune(name)) > 64 {
			return nil, errValidation("name must be 1-64 characters")
		}
		if seenNames[name] {
			return nil, errValidation(fmt.Sprintf("duplicate inbound name %q", name))
		}
		seenNames[name] = true
		if ib.Port < 1 || ib.Port > 65535 {
			return nil, errValidation("port must be between 1 and 65535")
		}
		if what, ok := reserved[ib.Port]; ok {
			return nil, errValidation(fmt.Sprintf("port %d is reserved (%s)", ib.Port, what))
		}
		tr, ok := protocolTransports[ib.Protocol]
		if !ok {
			return nil, errValidation("unsupported protocol")
		}
		params, err := validateParams(ib.Protocol, ib.Params)
		if err != nil {
			return nil, err
		}
		// Users: vless-reality and tuic carry uuid credentials;
		// password protocols only need names; shadowsocks ignores
		// users entirely.
		switch ib.Protocol {
		case ProtocolVLESSReality, ProtocolTUIC:
			for _, u := range ib.Users {
				if strings.TrimSpace(u.Name) == "" {
					return nil, errValidation("user name is required")
				}
				if !validUUID(u.UUID) {
					return nil, errValidation("user uuid must be a valid UUID")
				}
			}
		case ProtocolShadowsocks:
		default:
			for _, u := range ib.Users {
				if strings.TrimSpace(u.Name) == "" {
					return nil, errValidation("user name is required")
				}
			}
		}
		_ = tr
		out = append(out, Inbound{Name: name, Protocol: ib.Protocol, Port: ib.Port, Enabled: ib.Enabled, Params: params, Users: ib.Users})
	}
	// Per-transport port conflicts across the set.
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[i].Port != out[j].Port {
				continue
			}
			ti := protocolTransports[out[i].Protocol]
			tj := protocolTransports[out[j].Protocol]
			if ti&tj != 0 {
				return nil, errValidation(fmt.Sprintf(
					"port %d (%s) is already used by inbound %q", out[i].Port, (ti & tj).label(), out[j].Name))
			}
		}
	}
	return out, nil
}

// needsCert reports whether any enabled inbound needs a TLS
// certificate (the TLS-terminated protocols).
func needsCert(in []Inbound) bool {
	for _, ib := range in {
		if !ib.Enabled {
			continue
		}
		switch ib.Protocol {
		case ProtocolHysteria2, ProtocolTUIC, ProtocolAnyTLS, ProtocolTrojan:
			return true
		}
	}
	return false
}

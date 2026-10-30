package inbounds

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// This file is the multi-protocol registry: every supported protocol
// plugs into the panel through one protocolSpec — how its params are
// validated (with per-protocol error handling and create-time
// auto-generation), which transports it occupies (for port conflicts),
// how a stored record is compared with the running core config, and
// how a running inbound is imported. CRUD, handlers and storage never
// switch on protocol names; they go through this registry.

const (
	ProtocolTUIC        = "tuic"
	ProtocolShadowsocks = "shadowsocks"
	ProtocolAnyTLS      = "anytls"
	ProtocolTrojan      = "trojan"
)

// transport is a bitmask of the transports an inbound listens on.
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

type protocolSpec struct {
	transports   transport
	validate     func(raw json.RawMessage, creating bool) (string, error)
	compare      func(params string, r *RunningInbound) []Diff
	importParams func(r *RunningInbound) string
}

var protocolSpecs = map[string]protocolSpec{
	ProtocolVLESSReality: {
		transports:   transportTCP,
		validate:     validateRealityRaw,
		compare:      compareReality,
		importParams: importReality,
	},
	ProtocolHysteria2: {
		transports:   transportUDP,
		validate:     validateHy2Raw,
		compare:      compareHy2,
		importParams: importHy2,
	},
	ProtocolTUIC: {
		transports:   transportUDP,
		validate:     validateTuicRaw,
		compare:      compareTuic,
		importParams: importTuic,
	},
	ProtocolShadowsocks: {
		transports:   transportTCP | transportUDP,
		validate:     validateShadowsocksRaw,
		compare:      compareShadowsocks,
		importParams: importShadowsocks,
	},
	ProtocolAnyTLS: {
		transports:   transportTCP,
		validate:     validateAnyTLSRaw,
		compare:      compareSNIBased,
		importParams: importAnyTLS,
	},
	ProtocolTrojan: {
		transports:   transportTCP,
		validate:     validateTrojanRaw,
		compare:      compareSNIBased,
		importParams: importTrojan,
	},
}

// --- comparisons ------------------------------------------------------------

func compareReality(params string, m *RunningInbound) []Diff {
	var p RealityParams
	if err := json.Unmarshal([]byte(params), &p); err != nil {
		return nil
	}
	var diffs []Diff
	if p.SNI != m.SNI {
		diffs = append(diffs, Diff{Field: "sni", Panel: p.SNI, Running: m.SNI})
	}
	if p.PublicKey != m.PublicKey {
		diffs = append(diffs, Diff{Field: "publicKey", Panel: p.PublicKey, Running: m.PublicKey})
	}
	if m.ShortID != "" && p.ShortID != m.ShortID {
		diffs = append(diffs, Diff{Field: "shortId", Panel: p.ShortID, Running: m.ShortID})
	}
	return diffs
}

func compareHy2(params string, m *RunningInbound) []Diff {
	var p Hy2Params
	if err := json.Unmarshal([]byte(params), &p); err != nil {
		return nil
	}
	var diffs []Diff
	if p.UpMbps != m.UpMbps {
		diffs = append(diffs, Diff{Field: "upMbps", Panel: fmt.Sprintf("%d", p.UpMbps), Running: fmt.Sprintf("%d", m.UpMbps)})
	}
	if p.DownMbps != m.DownMbps {
		diffs = append(diffs, Diff{Field: "downMbps", Panel: fmt.Sprintf("%d", p.DownMbps), Running: fmt.Sprintf("%d", m.DownMbps)})
	}
	return diffs
}

func compareTuic(params string, m *RunningInbound) []Diff {
	var p TuicParams
	if err := json.Unmarshal([]byte(params), &p); err != nil {
		return nil
	}
	var diffs []Diff
	// Do NOT compare UUIDs: core TUIC users are generated per
	// subscription (uuid = subscription UUID), never from the
	// inbound's own stored UUID, so the panel UUID and the first
	// core user's UUID can never match — comparing them flagged
	// every TUIC inbound as drifted even when fully synced.
	// The inbound UUID is a dormant legacy field, kept for schema
	// stability, not a comparable value.
	if p.SNI != m.SNI {
		diffs = append(diffs, Diff{Field: "sni", Panel: p.SNI, Running: m.SNI})
	}
	// An absent congestion_control in the running config means the core
	// default; only flag an explicit mismatch.
	if m.Congestion != "" && p.CongestionControl != m.Congestion {
		diffs = append(diffs, Diff{Field: "congestion", Panel: p.CongestionControl, Running: m.Congestion})
	}
	return diffs
}

func compareShadowsocks(params string, m *RunningInbound) []Diff {
	var p ShadowsocksParams
	if err := json.Unmarshal([]byte(params), &p); err != nil {
		return nil
	}
	if p.Method != m.Method {
		return []Diff{{Field: "method", Panel: p.Method, Running: m.Method}}
	}
	return nil
}

// compareSNIBased serves the TLS-terminated protocols whose only
// comparable structural field is the TLS server name.
func compareSNIBased(params string, m *RunningInbound) []Diff {
	var p struct {
		SNI string `json:"sni"`
	}
	if err := json.Unmarshal([]byte(params), &p); err != nil {
		return nil
	}
	if p.SNI != m.SNI {
		return []Diff{{Field: "sni", Panel: p.SNI, Running: m.SNI}}
	}
	return nil
}

// --- imports ------------------------------------------------------------------

func importReality(r *RunningInbound) string {
	flow := r.Flow
	if flow == "" {
		flow = "xtls-rprx-vision"
	}
	b, _ := json.Marshal(RealityParams{
		SNI: r.SNI, PublicKey: r.PublicKey, PrivateKey: r.PrivateKey,
		ShortID: r.ShortID, Flow: flow,
	})
	return string(b)
}

func importHy2(r *RunningInbound) string {
	b, _ := json.Marshal(Hy2Params{
		UpMbps: r.UpMbps, DownMbps: r.DownMbps, Password: r.Password,
		Obfs: r.Obfs, ObfsPassword: r.ObfsPassword,
	})
	return string(b)
}

func importTuic(r *RunningInbound) string {
	congestion := r.Congestion
	if congestion == "" {
		congestion = "cubic" // core default when unset
	}
	b, _ := json.Marshal(TuicParams{
		UUID: r.UUID, Password: r.Password, CongestionControl: congestion, SNI: r.SNI,
	})
	return string(b)
}

func importShadowsocks(r *RunningInbound) string {
	b, _ := json.Marshal(ShadowsocksParams{Method: r.Method, Password: r.Password})
	return string(b)
}

func importAnyTLS(r *RunningInbound) string {
	b, _ := json.Marshal(AnyTLSParams{Password: r.Password, SNI: r.SNI})
	return string(b)
}

func importTrojan(r *RunningInbound) string {
	b, _ := json.Marshal(TrojanParams{Password: r.Password, SNI: r.SNI})
	return string(b)
}

// --- TUIC ---------------------------------------------------------------------

// TuicParams are the settings of a TUIC v5 inbound. The server
// certificate comes from the panel's certificate store at sync time.
type TuicParams struct {
	UUID              string `json:"uuid"`
	Password          string `json:"password"`
	CongestionControl string `json:"congestionControl"` // cubic | new_reno | bbr
	SNI               string `json:"sni"`
}

func validateTuic(p *TuicParams, creating bool) error {
	p.SNI = strings.TrimSpace(p.SNI)
	p.UUID = strings.TrimSpace(p.UUID)
	if creating && p.UUID == "" {
		p.UUID = newUUID()
	}
	if !validUUID(p.UUID) {
		return errValidation("tuic uuid must be a valid UUID")
	}
	if creating && p.Password == "" {
		p.Password = randomPassword()
	}
	if p.Password == "" {
		return errValidation("tuic password is required")
	}
	if p.CongestionControl == "" {
		p.CongestionControl = "bbr"
	}
	switch p.CongestionControl {
	case "cubic", "new_reno", "bbr":
	default:
		return errValidation("congestionControl must be cubic, new_reno or bbr")
	}
	return nil
}

func validateTuicRaw(raw json.RawMessage, creating bool) (string, error) {
	var p TuicParams
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &p); err != nil {
			return "", errValidation("invalid tuic params")
		}
	}
	if err := validateTuic(&p, creating); err != nil {
		return "", err
	}
	b, _ := json.Marshal(p)
	return string(b), nil
}

func newUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
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

// --- Shadowsocks ----------------------------------------------------------------

// ShadowsocksParams are the settings of a Shadowsocks inbound. For the
// 2022 methods the password is the base64 server key, whose length is
// fixed per method; legacy methods take a free-form password.
type ShadowsocksParams struct {
	Method   string `json:"method"`
	Password string `json:"password"`
}

// ss2022KeyLen maps each 2022 method to its required key length in bytes.
var ss2022KeyLen = map[string]int{
	"2022-blake3-aes-128-gcm":       16,
	"2022-blake3-aes-256-gcm":       32,
	"2022-blake3-chacha20-poly1305": 32,
}

func generateSSKey(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(b)
}

func decodeSSKey(s string) ([]byte, error) {
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.RawStdEncoding.DecodeString(s)
}

func validateShadowsocks(p *ShadowsocksParams, creating bool) error {
	if p.Method == "" {
		p.Method = "2022-blake3-aes-128-gcm"
	}
	if keyLen, is2022 := ss2022KeyLen[p.Method]; is2022 {
		if creating && p.Password == "" {
			p.Password = generateSSKey(keyLen)
		}
		b, err := decodeSSKey(p.Password)
		if err != nil || len(b) != keyLen {
			return errValidation(fmt.Sprintf(
				"shadowsocks %s key must be a %d-byte base64 value", p.Method, keyLen))
		}
		return nil
	}
	// Legacy methods and any other method string take a free-form
	// password. The dropdown lists the common methods as a
	// convenience, not as the validation boundary: a real protocol
	// method the dropdown does not list (tier 2) must store verbatim
	// here, and the core's own check at sync time is the final
	// validator for a bogus value (tier 3) — it blocks the sync
	// with an error instead of silently dropping the value.
	if creating && p.Password == "" {
		p.Password = randomPassword()
	}
	if p.Password == "" {
		return errValidation("shadowsocks password is required")
	}
	return nil
}

func validateShadowsocksRaw(raw json.RawMessage, creating bool) (string, error) {
	var p ShadowsocksParams
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &p); err != nil {
			return "", errValidation("invalid shadowsocks params")
		}
	}
	if err := validateShadowsocks(&p, creating); err != nil {
		return "", err
	}
	b, _ := json.Marshal(p)
	return string(b), nil
}

// --- AnyTLS / Trojan --------------------------------------------------------------

// AnyTLSParams are the settings of an AnyTLS inbound. The server
// certificate comes from the panel's certificate store at sync time.
type AnyTLSParams struct {
	Password string `json:"password"`
	SNI      string `json:"sni"`
}

// TrojanParams are the settings of a Trojan inbound. The server
// certificate comes from the panel's certificate store at sync time.
type TrojanParams struct {
	Password string `json:"password"`
	SNI      string `json:"sni"`
}

func validatePasswordSNI(protocol, password, sni string, creating bool) (string, error) {
	password = strings.TrimSpace(password)
	if creating && password == "" {
		password = randomPassword()
	}
	if password == "" {
		return "", errValidation(protocol + " password is required")
	}
	sni = strings.TrimSpace(sni)
	if strings.ContainsAny(sni, " /") {
		return "", errValidation("sni must be a host name")
	}
	return password, nil
}

func validateAnyTLSRaw(raw json.RawMessage, creating bool) (string, error) {
	var p AnyTLSParams
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &p); err != nil {
			return "", errValidation("invalid anytls params")
		}
	}
	pw, err := validatePasswordSNI("anytls", p.Password, p.SNI, creating)
	if err != nil {
		return "", err
	}
	p.Password, p.SNI = pw, strings.TrimSpace(p.SNI)
	b, _ := json.Marshal(p)
	return string(b), nil
}

func validateTrojanRaw(raw json.RawMessage, creating bool) (string, error) {
	var p TrojanParams
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &p); err != nil {
			return "", errValidation("invalid trojan params")
		}
	}
	pw, err := validatePasswordSNI("trojan", p.Password, p.SNI, creating)
	if err != nil {
		return "", err
	}
	p.Password, p.SNI = pw, strings.TrimSpace(p.SNI)
	b, _ := json.Marshal(p)
	return string(b), nil
}

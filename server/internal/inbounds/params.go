package inbounds

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// RealityParams are the settings of a vless-reality inbound.
type RealityParams struct {
	SNI        string `json:"sni"`
	PublicKey  string `json:"publicKey"`
	PrivateKey string `json:"privateKey"`
	ShortID    string `json:"shortId"`
	Flow       string `json:"flow"`
}

// Hy2Params are the settings of a hysteria2 inbound. Password is the
// shared connection password (per-subscription users live in the
// subscriptions layer later; this is the inbound-level credential).
// Hy2Params are the settings of a hysteria2 inbound. Password is the
// shared connection password (per-subscription users live in the
// subscriptions layer later; this is the inbound-level credential).
type Hy2Params struct {
	UpMbps       int    `json:"upMbps"`
	DownMbps     int    `json:"downMbps"`
	Password     string `json:"password"`
	Obfs         string `json:"obfs"` // "" | "salamander"
	ObfsPassword string `json:"obfsPassword"`
}

// Diff is one field-level difference between the panel record and the
// running core config.
// GenerateRealityKeypair returns a fresh X25519 key pair and short id in
// the encoding sing-box expects (base64url, raw).
func GenerateRealityKeypair() (RealityParams, error) {
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return RealityParams{}, fmt.Errorf("inbounds: generate reality key: %w", err)
	}
	return RealityParams{
		PublicKey:  base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes()),
		PrivateKey: base64.RawURLEncoding.EncodeToString(priv.Bytes()),
		ShortID:    randomHex(4),
		Flow:       "xtls-rprx-vision",
	}, nil
}
func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}
func randomPassword() string {
	return base64.RawURLEncoding.EncodeToString([]byte(randomHex(12)))[:16]
}

// --- validation -----------------------------------------------------------
func validateNamePort(name string, port int) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 64 {
		return "", errValidation("name must be 1-64 characters")
	}
	if port < 1 || port > 65535 {
		return "", errValidation("port must be between 1 and 65535")
	}
	if what, reserved := ReservedPorts[port]; reserved {
		return "", errValidation(fmt.Sprintf("port %d is reserved (%s)", port, what))
	}
	return name, nil
}
func validRealityKey(s string) bool {
	b, err := base64.RawURLEncoding.DecodeString(s)
	return err == nil && len(b) == 32
}
func validateReality(p *RealityParams, creating bool) error {
	p.SNI = strings.TrimSpace(p.SNI)
	if p.SNI == "" {
		return errValidation("reality sni is required")
	}
	if creating && p.PrivateKey == "" && p.PublicKey == "" {
		kp, err := GenerateRealityKeypair()
		if err != nil {
			return err
		}
		p.PublicKey, p.PrivateKey = kp.PublicKey, kp.PrivateKey
		if p.ShortID == "" {
			p.ShortID = kp.ShortID
		}
	}
	if !validRealityKey(p.PrivateKey) || !validRealityKey(p.PublicKey) {
		return errValidation("reality keys must be 32-byte base64url values")
	}
	// The public key must be the one derived from the private key,
	// otherwise the rendered config would never work.
	priv, err := ecdh.X25519().NewPrivateKey(mustDecode(p.PrivateKey))
	if err != nil || base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes()) != p.PublicKey {
		return errValidation("reality public key does not match the private key")
	}
	if p.ShortID == "" {
		p.ShortID = randomHex(4)
	}
	if len(p.ShortID) > 16 || len(p.ShortID)%2 != 0 {
		return errValidation("shortId must be even-length hex, at most 16 characters")
	}
	if _, err := hex.DecodeString(p.ShortID); err != nil {
		return errValidation("shortId must be hexadecimal")
	}
	if p.Flow == "" {
		p.Flow = "xtls-rprx-vision"
	}
	return nil
}
func mustDecode(s string) []byte {
	b, _ := base64.RawURLEncoding.DecodeString(s)
	return b
}
func validateHy2(p *Hy2Params, creating bool) error {
	if p.UpMbps < 0 || p.UpMbps > 100000 || p.DownMbps < 0 || p.DownMbps > 100000 {
		return errValidation("bandwidth must be between 0 and 100000 Mbps")
	}
	if creating && p.Password == "" {
		p.Password = randomPassword()
	}
	if p.Password == "" {
		return errValidation("hysteria2 password is required")
	}
	switch p.Obfs {
	case "":
		p.ObfsPassword = ""
	case "salamander":
		// Auto-generate on edit too, not only on create: editing
		// an existing inbound from no-obfs to salamander leaves
		// the password empty, and that transition was the case
		// the original creating-only rule missed.
		if p.ObfsPassword == "" {
			p.ObfsPassword = randomPassword()
		}
		if p.ObfsPassword == "" {
			return errValidation("obfs password is required when obfs is enabled")
		}
	default:
		return errValidation("obfs must be empty or salamander")
	}
	return nil
}
func validateRealityRaw(raw json.RawMessage, creating bool) (string, error) {
	var p RealityParams
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &p); err != nil {
			return "", errValidation("invalid reality params")
		}
	}
	if err := validateReality(&p, creating); err != nil {
		return "", err
	}
	b, _ := json.Marshal(p)
	return string(b), nil
}
func validateHy2Raw(raw json.RawMessage, creating bool) (string, error) {
	var p Hy2Params
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &p); err != nil {
			return "", errValidation("invalid hysteria2 params")
		}
	}
	if err := validateHy2(&p, creating); err != nil {
		return "", err
	}
	b, _ := json.Marshal(p)
	return string(b), nil
}

// ValidateParams validates stored-style params for a protocol WITHOUT
// invented. Node import uses this so an unmappable core inbound is
// reported disabled instead of silently completed.
// ValidateParams validates stored-style params for a protocol WITHOUT
// invented. Node import uses this so an unmappable core inbound is
// reported disabled instead of silently completed.
func ValidateParams(protocol string, raw json.RawMessage) (string, error) {
	return normalizeParams(protocol, raw, false)
}

// normalizeParams validates the raw params for a protocol and returns the
// canonical JSON to store. Dispatch goes through the protocol registry.
// normalizeParams validates the raw params for a protocol and returns the
// canonical JSON to store. Dispatch goes through the protocol registry.
func normalizeParams(protocol string, raw json.RawMessage, creating bool) (string, error) {
	spec, ok := protocolSpecs[protocol]
	if !ok {
		return "", errValidation("unsupported protocol")
	}
	return spec.validate(raw, creating)
}

// checkPortConflict enforces per-transport port uniqueness across
// protocols: inbounds sharing a transport (TCP/UDP) may not share a
// port. Same-protocol collisions are also caught here (and backstopped
// by the (protocol, port) unique index, which is partial on
// run_local). Only inbounds that actually run locally participate:
// a nodes-only inbound neither needs a free local port nor blocks
// one, and a run_local flip frees (or claims) its port accordingly.

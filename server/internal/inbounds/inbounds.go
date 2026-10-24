// Package inbounds owns the panel's inbound records: the desired state
// for sing-box inbounds, kept in the store, plus a read-only comparison
// against the core's running configuration. Nothing here writes to the
// core — applying desired state is a separate sync step (later phase).
package inbounds

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"singbox-ui/server/internal/store"
)

const (
	ProtocolVLESSReality = "vless-reality"
	ProtocolHysteria2    = "hysteria2"
)

// ReservedPorts can never be handed to an inbound: they belong to the
// panel itself, SSH, the monitor service and the core's Clash API.
var ReservedPorts = map[int]string{
	2826:  "SSH",
	9090:  "Clash API",
	32434: "旧面板",
	32435: "本面板",
	39100: "监控服务",
}

// Error mirrors the certs package's typed API failure.
type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }

func errValidation(msg string) *Error {
	return &Error{Status: 400, Code: "validation", Message: msg}
}

func errNotFound() *Error {
	return &Error{Status: 404, Code: "inbound_not_found", Message: "inbound not found"}
}

func errConflict(code, msg string) *Error {
	return &Error{Status: 409, Code: code, Message: msg}
}

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
type Hy2Params struct {
	UpMbps       int    `json:"upMbps"`
	DownMbps     int    `json:"downMbps"`
	Password     string `json:"password"`
	Obfs         string `json:"obfs"` // "" | "salamander"
	ObfsPassword string `json:"obfsPassword"`
}

// Diff is one field-level difference between the panel record and the
// running core config.
type Diff struct {
	Field   string `json:"field"` // sni | publicKey | shortId | upMbps | downMbps
	Panel   string `json:"panel"`
	Running string `json:"running"`
}

// View is one inbound as the API returns it: the stored record plus its
// comparison status against the running core config.
type View struct {
	ID        int64           `json:"id"`
	Name      string          `json:"name"`
	Protocol  string          `json:"protocol"`
	Port      int             `json:"port"`
	Params    json.RawMessage `json:"params"`
	Enabled   bool            `json:"enabled"`
	Status    string          `json:"status"` // synced | drifted | pending | unknown | disabled
	Diffs     []Diff          `json:"diffs,omitempty"`
	CreatedAt int64           `json:"createdAt"`
	UpdatedAt int64           `json:"updatedAt"`
}

// ListResult is the GET /api/inbounds payload.
type ListResult struct {
	Items            []View           `json:"items"`
	RunningAvailable bool             `json:"runningAvailable"`
	Extras           []RunningInbound `json:"extras"`
}

type Service struct {
	st         *store.Store
	configPath string
}

func NewService(st *store.Store, singboxConfigPath string) *Service {
	return &Service{st: st, configPath: singboxConfigPath}
}

func nowUnix() int64 { return time.Now().Unix() }

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
		if creating && p.ObfsPassword == "" {
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
// by the (protocol, port) unique index).
func (s *Service) checkPortConflict(ctx context.Context, selfID int64, protocol string, port int) error {
	spec, ok := protocolSpecs[protocol]
	if !ok {
		return errValidation("unsupported protocol")
	}
	rows, err := s.st.ListInbounds(ctx)
	if err != nil {
		return err
	}
	for _, r := range rows {
		if r.ID == selfID || r.Port != port {
			continue
		}
		other, ok := protocolSpecs[r.Protocol]
		if !ok {
			continue
		}
		if overlap := spec.transports & other.transports; overlap != 0 {
			return errConflict("port_conflict", fmt.Sprintf(
				"port %d (%s) is already used by inbound %q", port, overlap.label(), r.Name))
		}
	}
	return nil
}

func conflictErr(err error) error {
	// The SQLite driver reports unique violations against the table
	// columns ("UNIQUE constraint failed: inbounds.protocol, inbounds.port")
	// or against the index name. Port uniqueness is per protocol
	// (TCP vs UDP), enforced by idx_inbounds_proto_port.
	msg := err.Error()
	switch {
	case strings.Contains(msg, "inbounds.port"), strings.Contains(msg, "idx_inbounds_port"), strings.Contains(msg, "idx_inbounds_proto_port"):
		return errConflict("port_conflict", "another inbound of the same protocol already uses this port")
	case strings.Contains(msg, "inbounds.name"), strings.Contains(msg, "idx_inbounds_name"):
		return errConflict("name_conflict", "another inbound already uses this name")
	}
	return err
}

// --- CRUD -----------------------------------------------------------------

type CreateRequest struct {
	Name     string          `json:"name"`
	Protocol string          `json:"protocol"`
	Port     int             `json:"port"`
	Params   json.RawMessage `json:"params"`
}

func (s *Service) Create(ctx context.Context, req CreateRequest) (View, error) {
	name, err := validateNamePort(req.Name, req.Port)
	if err != nil {
		return View{}, err
	}
	params, err := normalizeParams(req.Protocol, req.Params, true)
	if err != nil {
		return View{}, err
	}
	if err := s.checkPortConflict(ctx, 0, req.Protocol, req.Port); err != nil {
		return View{}, err
	}
	id, err := s.st.CreateInbound(ctx, name, req.Protocol, req.Port, params, nowUnix())
	if err != nil {
		return View{}, conflictErr(err)
	}
	return s.viewByID(ctx, id)
}

type UpdateRequest struct {
	Name   string          `json:"name"`
	Port   int             `json:"port"`
	Params json.RawMessage `json:"params"`
}

func (s *Service) Update(ctx context.Context, id int64, req UpdateRequest) (View, error) {
	rows, err := s.st.ListInbounds(ctx)
	if err != nil {
		return View{}, err
	}
	var cur *store.InboundRow
	for i := range rows {
		if rows[i].ID == id {
			cur = &rows[i]
			break
		}
	}
	if cur == nil {
		return View{}, errNotFound()
	}
	name, err := validateNamePort(req.Name, req.Port)
	if err != nil {
		return View{}, err
	}
	params, err := normalizeParams(cur.Protocol, req.Params, false)
	if err != nil {
		return View{}, err
	}
	if err := s.checkPortConflict(ctx, id, cur.Protocol, req.Port); err != nil {
		return View{}, err
	}
	if err := s.st.UpdateInbound(ctx, id, name, req.Port, params, nowUnix()); err != nil {
		return View{}, conflictErr(err)
	}
	return s.viewByID(ctx, id)
}

// SetEnabled parks or un-parks an inbound. A disabled inbound keeps its
// record but is excluded when desired state is applied to the core.
func (s *Service) SetEnabled(ctx context.Context, id int64, enabled bool) (View, error) {
	ok, err := s.st.SetInboundEnabled(ctx, id, enabled, nowUnix())
	if err != nil {
		return View{}, err
	}
	if !ok {
		return View{}, errNotFound()
	}
	return s.viewByID(ctx, id)
}

func (s *Service) Delete(ctx context.Context, id int64) error {
	rows, err := s.st.ListInbounds(ctx)
	if err != nil {
		return err
	}
	found := false
	for _, r := range rows {
		if r.ID == id {
			found = true
			break
		}
	}
	if !found {
		return errNotFound()
	}
	return s.st.DeleteInbound(ctx, id)
}

func (s *Service) viewByID(ctx context.Context, id int64) (View, error) {
	res, err := s.List(ctx)
	if err != nil {
		return View{}, err
	}
	for _, v := range res.Items {
		if v.ID == id {
			return v, nil
		}
	}
	return View{}, errNotFound()
}

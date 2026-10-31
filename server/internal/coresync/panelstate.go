package coresync

import (
	"crypto/ecdh"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"singbox-ui/server/internal/store"
)

type changeAction string

const (
	changeUpdated        changeAction = "updated"
	changeAdded          changeAction = "added"
	changeDeleted        changeAction = "deleted"
	changePassthrough    changeAction = "passthrough"
	changeDroppedIllegal changeAction = "dropped_illegal"
	changeReverted       changeAction = "reverted"
	changeRestoreNote    changeAction = "restore_note"
)

type change struct {
	action changeAction
	item   string
	oldVal string
	newVal string
	detail string
}

// --- import operations ------------------------------------------------------
// Import ops are executed by syncInternal after classification, each
// verified against post-state (a service method can report an error
// after its store write landed, via the view path — the stored row is
// the truth, not the returned error).
type importOp struct {
	kind string // inboundName | inboundPort | inboundParams | inboundCreate | inboundDelete | inboundEnable | subUUID | subRename | settingHost
	// changes are the proposed log rows for this op: kept on success,
	// converted to dropped_illegal/reverted rows on failure, so the
	// log never claims a change that did not land.
	changes []change
	// inbound ops
	inboundID int64
	name      string
	protocol  string
	port      int
	params    string // panel-format params JSON
	enabled   bool
	// subscription ops
	subID int64
	uuid  string
	// setting ops
	host string
}

// --- passthrough specification ----------------------------------------------
// passSpec is the exact set of file values the merged config must
// carry verbatim. Field paths are dot-joined keys inside one object
// (e.g. "tls.alpn", "tls.reality.short_id"); userSet/userDel address
// users by name inside one inbound.
type inboundPass struct {
	whole      any                       // whole inbound object (unmanaged tag): append verbatim
	fieldSet   map[string]any            // field path -> file value
	fieldDel   []string                  // field paths to omit from the generated inbound
	userAppend []any                     // unknown user entries to append
	userSet    map[string]map[string]any // user name -> field -> file value
	userDel    map[string][]string       // user name -> fields to omit
}
type passSpec struct {
	top       map[string]any          // whole top-level keys (dns, ...)
	logSet    map[string]any          // log fields to set/override
	logDel    []string                // log fields to omit
	expSet    map[string]any          // experimental keys to set (except clash_api)
	directSet map[string]any          // extra fields on the direct outbound (field-level)
	outExtra  []any                   // extra outbounds to append (non-direct tags)
	inbounds  map[string]*inboundPass // by tag
}

func (p *passSpec) inbound(tag string) *inboundPass {
	ib, ok := p.inbounds[tag]
	if !ok {
		ib = &inboundPass{
			fieldSet: map[string]any{},
			userSet:  map[string]map[string]any{},
			userDel:  map[string][]string{},
		}
		p.inbounds[tag] = ib
	}
	return ib
}

// --- panel state ------------------------------------------------------------
func newPassSpec() *passSpec {
	return &passSpec{
		top:       map[string]any{},
		logSet:    map[string]any{},
		expSet:    map[string]any{},
		directSet: map[string]any{},
		inbounds:  map[string]*inboundPass{},
	}
}

type panelState struct {
	inbounds   []store.InboundRow
	subs       []store.SubscriptionRow
	publicHost string
	byTag      map[string]*store.InboundRow
	byName     map[string]*store.SubscriptionRow
	byUUID     map[string]*store.SubscriptionRow
	passTags   map[string]bool
}

func buildPanelState(inbounds []store.InboundRow, subs []store.SubscriptionRow, publicHost string, passTags []string) panelState {
	ps := panelState{
		inbounds:   inbounds,
		subs:       subs,
		publicHost: publicHost,
		byTag:      map[string]*store.InboundRow{},
		byName:     map[string]*store.SubscriptionRow{},
		byUUID:     map[string]*store.SubscriptionRow{},
		passTags:   map[string]bool{},
	}
	for i := range inbounds {
		ps.byTag[inbounds[i].Name] = &inbounds[i]
	}
	for i := range subs {
		ps.byName[subs[i].Name] = &subs[i]
		ps.byUUID[subs[i].UUID] = &subs[i]
	}
	for _, t := range passTags {
		ps.passTags[t] = true
	}
	return ps
}

// --- JSON helpers -----------------------------------------------------------
func panelProtocol(fileIb map[string]any) (string, bool) {
	t := asString(fileIb["type"])
	switch t {
	case "hysteria2", "tuic", "anytls", "trojan":
		return t, true
	case "vless":
		tls, _ := asMap(fileIb["tls"])
		reality, _ := asMap(tls["reality"])
		if en, _ := reality["enabled"].(bool); en {
			return "vless-reality", true
		}
		return "", false
	default:
		return "", false
	}
}

// deriveRealityPublicKey returns the public key for a Reality private
// key (base64url, raw). The core config stores only the private key;
// the panel record needs the derived public key for validation.
// deriveRealityPublicKey returns the public key for a Reality private
// key (base64url, raw). The core config stores only the private key;
// the panel record needs the derived public key for validation.
func deriveRealityPublicKey(privateKey string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(privateKey)
	if err != nil || len(raw) != 32 {
		return "", fmt.Errorf("invalid reality private key")
	}
	priv, err := ecdh.X25519().NewPrivateKey(raw)
	if err != nil {
		return "", fmt.Errorf("invalid reality private key")
	}
	return base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes()), nil
}
func validUUIDStr(s string) bool {
	_, err := uuid.Parse(s)
	return err == nil
}
func validSubName(s string) bool {
	s = strings.TrimSpace(s)
	return s != "" && len([]rune(s)) <= 64
}

// mappedParams converts a config inbound object into panel-format
// params, returning (paramsMap, delFields, unmapped). paramsMap holds
// only fields the panel models AND the file sets; delFields holds
// auto-fill panel fields the file omits (the merged config must omit
// them rather than let panel validation invent a value); unmapped
// holds file fields with no panel counterpart (field-level
// passthrough). A nil paramsMap with err means the object cannot be
// represented at all (caller treats it as illegal).
// mappedParams converts a config inbound object into panel-format
// params, returning (paramsMap, delFields, unmapped). paramsMap holds
// only fields the panel models AND the file sets; delFields holds
// auto-fill panel fields the file omits (the merged config must omit
// them rather than let panel validation invent a value); unmapped
// holds file fields with no panel counterpart (field-level
// passthrough). A nil paramsMap with err means the object cannot be
// represented at all (caller treats it as illegal).
func mappedParams(protocol string, fileIb map[string]any) (map[string]any, []string, map[string]any, error) {
	out := map[string]any{}
	var del []string
	unmapped := map[string]any{}
	tls, _ := asMap(fileIb["tls"])

	switch protocol {
	case "vless-reality":
		sni := asString(tls["server_name"])
		out["sni"] = sni
		reality, _ := asMap(tls["reality"])
		priv := asString(reality["private_key"])
		if priv == "" {
			return nil, nil, nil, fmt.Errorf("reality private_key is required")
		}
		pub, err := deriveRealityPublicKey(priv)
		if err != nil {
			return nil, nil, nil, err
		}
		out["privateKey"] = priv
		out["publicKey"] = pub
		if ids, ok := asSlice(reality["short_id"]); ok {
			switch len(ids) {
			case 0:
				del = append(del, "tls.reality.short_id")
			case 1:
				out["shortId"] = asString(ids[0])
			default:
				// A list the panel cannot represent: preserve verbatim.
				unmapped["tls.reality.short_id"] = deepCopyJSON(ids)
			}
		} else if _, present := reality["short_id"]; present {
			return nil, nil, nil, fmt.Errorf("reality short_id is not an array")
		} else {
			del = append(del, "tls.reality.short_id")
		}
		// Handshake server/port: generated from SNI + 443. A different
		// handshake is unmodelled, preserved verbatim.
		if hs, ok := asMap(reality["handshake"]); ok {
			if asString(hs["server"]) != sni || portOf(hs["server_port"]) != 443 {
				unmapped["tls.reality.handshake"] = deepCopyJSON(hs)
			}
		}
		// Flow is inbound-level in the panel but per-user in the
		// config; uniformity is resolved by the caller (users loop).
		// Here we only note extra reality fields as unmapped.
		for k, v := range reality {
			switch k {
			case "enabled", "private_key", "short_id", "handshake":
			default:
				unmapped["tls.reality."+k] = deepCopyJSON(v)
			}
		}
		for k, v := range tls {
			switch k {
			case "enabled", "server_name", "reality":
			default:
				unmapped["tls."+k] = deepCopyJSON(v)
			}
		}
	case "hysteria2":
		// Absent means zero/empty here (generation omits zero and
		// empty obfs), so removal imports cleanly instead of keeping
		// a stale panel value.
		if v, ok := asFloat(fileIb["up_mbps"]); ok {
			out["upMbps"] = int(v)
		} else {
			out["upMbps"] = 0
		}
		if v, ok := asFloat(fileIb["down_mbps"]); ok {
			out["downMbps"] = int(v)
		} else {
			out["downMbps"] = 0
		}
		if obfs, ok := asMap(fileIb["obfs"]); ok {
			if asString(obfs["type"]) == "salamander" {
				out["obfs"] = "salamander"
				out["obfsPassword"] = asString(obfs["password"])
			} else {
				unmapped["obfs"] = deepCopyJSON(obfs)
			}
		} else {
			out["obfs"] = ""
		}
		for k, v := range tls {
			switch k {
			case "enabled", "certificate", "key", "server_name", "alpn":
				if k == "alpn" {
					// Generated alpn is fixed [h3]; anything else is
					// preserved verbatim.
					if !jsonEqual(v, []any{"h3"}) {
						unmapped["tls.alpn"] = deepCopyJSON(v)
					}
				}
			default:
				unmapped["tls."+k] = deepCopyJSON(v)
			}
		}
		// Password + server_name are resolved by the caller (users
		// loop for password uniformity, hy2 group for server_name).
	case "tuic":
		if raw, present := fileIb["congestion_control"]; present {
			if v, ok := raw.(string); ok {
				out["congestionControl"] = v
			} else {
				return nil, nil, nil, fmt.Errorf("tuic congestion_control is not a string")
			}
		} else {
			del = append(del, "congestion_control")
		}
		out["sni"] = asString(tls["server_name"])
		for k, v := range tls {
			switch k {
			case "enabled", "certificate", "key", "server_name", "alpn":
				if k == "alpn" {
					if !jsonEqual(v, []any{"h3"}) {
						unmapped["tls.alpn"] = deepCopyJSON(v)
					}
				}
			default:
				unmapped["tls."+k] = deepCopyJSON(v)
			}
		}
	case "anytls", "trojan":
		out["sni"] = asString(tls["server_name"])
		for k, v := range tls {
			switch k {
			case "enabled", "certificate", "key", "server_name", "alpn":
			default:
				unmapped["tls."+k] = deepCopyJSON(v)
			}
		}
	}
	// Unknown top-level fields on any inbound are preserved verbatim.
	for k, v := range fileIb {
		switch k {
		case "type", "tag", "listen", "listen_port", "users", "tls", "up_mbps", "down_mbps", "obfs", "congestion_control", "method", "password":
		default:
			unmapped[k] = deepCopyJSON(v)
		}
	}
	return out, del, unmapped, nil
}

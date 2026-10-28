// Classification is the sync engine's decision layer: it diffs the
// on-disk config against the last-written baseline and sorts every
// difference into exactly one bucket, under a strict rule — a value
// is only ever imported into panel state when it maps EXACTLY to a
// panel field the UI can itself edit. Anything that cannot be mapped
// with certainty is never guessed into a panel field: unmodelled
// content is carried verbatim (passthrough), invalid content is
// dropped (illegal), and a valid-but-unimportable edit is reverted
// with the panel value kept. When in doubt, the classifier touches
// nothing it cannot identify.
//
// Secrets (uuid, password, private keys) are never written into
// change records; those rows carry empty values and a detail note.

package coresync

import (
	"crypto/ecdh"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"singbox-ui/server/internal/store"

	"github.com/google/uuid"
)

// --- change records ---------------------------------------------------------

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

func newPassSpec() *passSpec {
	return &passSpec{
		top:       map[string]any{},
		logSet:    map[string]any{},
		expSet:    map[string]any{},
		directSet: map[string]any{},
		inbounds:  map[string]*inboundPass{},
	}
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

func parseDoc(b []byte) (map[string]any, error) {
	var root any
	if err := json.Unmarshal(b, &root); err != nil {
		return nil, err
	}
	m, ok := root.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("config is not a JSON object")
	}
	return m, nil
}

func asMap(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}

func asSlice(v any) ([]any, bool) {
	s, ok := v.([]any)
	return s, ok
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func asFloat(v any) (float64, bool) {
	f, ok := v.(float64)
	return f, ok
}

// deepCopyJSON round-trips through JSON: a true deep copy for JSON
// values (maps/slices/scalars), so merging never mutates its inputs.
func deepCopyJSON(v any) any {
	b, _ := json.Marshal(v)
	var out any
	_ = json.Unmarshal(b, &out)
	return out
}

func jsonEqual(a, b any) bool { return reflect.DeepEqual(a, b) }

// valStr renders a JSON value for the change log. Secrets are masked
// by the caller (secret=true forces an empty value).
func valStr(v any, secret bool) string {
	if secret {
		return ""
	}
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}

// secretField reports whether a leaf field name carries a credential.
// publicKey is public by design (clients receive it); uuid, password
// and private keys are never logged.
func secretField(field string) bool {
	switch strings.ToLower(field) {
	case "uuid", "password", "private_key", "privatekey":
		return true
	}
	return false
}

func leafField(path string) string {
	if i := strings.LastIndex(path, "."); i >= 0 {
		return path[i+1:]
	}
	return path
}

// getFieldPath walks a dot-separated path through nested maps,
// reporting whether the leaf exists. Used to tell a file REMOVAL
// (baseline had the field) from file SILENCE (baseline never had
// it — the panel value then stands).
func getFieldPath(obj map[string]any, path string) (any, bool) {
	if obj == nil {
		return nil, false
	}
	cur := obj
	parts := strings.Split(path, ".")
	for i, p := range parts {
		v, ok := cur[p]
		if !ok {
			return nil, false
		}
		if i == len(parts)-1 {
			return v, true
		}
		next, ok := asMap(v)
		if !ok {
			return nil, false
		}
		cur = next
	}
	return nil, false
}

// --- protocol mapping -------------------------------------------------------
// panelProtocol maps a config inbound's type to the panel protocol it
// can be edited as. Plain vless (no Reality) and every other type are
// unmodelled: the panel must not guess them into a row.

func panelProtocol(fileIb map[string]any) (string, bool) {
	t := asString(fileIb["type"])
	switch t {
	case "hysteria2", "tuic", "shadowsocks", "anytls", "trojan":
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
	case "shadowsocks":
		out["method"] = asString(fileIb["method"])
		out["password"] = asString(fileIb["password"])
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

func portOf(v any) int {
	if f, ok := asFloat(v); ok {
		return int(f)
	}
	return 0
}

// --- classification ---------------------------------------------------------

type classifyResult struct {
	imports []importOp
	passes  *passSpec
	changes []change
}

// classify diffs fileDoc against oldDoc (the last-written baseline)
// using panel state for identity matching. It never writes anything;
// import ops and change records are applied by the caller.
//
// Panel-wins rule: the panel is the source of truth for managed
// content. A file edit to managed content (a panel row's port,
// params, users, or the shared hy2 host setting) is REVERTED by
// regeneration — never imported. Only the FIRST import (firstImport,
// when no baseline exists) imports file content into the panel, once.
// Panel-unmanaged data is preserved: whole unmanaged inbounds,
// top-level segments, log.level, non-clash experimental keys, extra
// outbounds, and unknown users inside managed inbounds (kept working
// as passthrough until a template binds them). Extra hand-added
// FIELDS inside a panel-managed inbound are dropped on regen (the
// whole inbound is panel-generated); whole unmanaged segments are
// preserved. A field the panel cannot model at all (tier 1) is
// passthrough only when it lives outside a managed inbound.
func classify(oldDoc, fileDoc map[string]any, panel panelState, firstImport bool) classifyResult {
	res := classifyResult{passes: newPassSpec()}
	oldInbounds := inboundList(oldDoc)
	fileInbounds, bad := inboundListChecked(fileDoc)
	for _, b := range bad {
		res.changes = append(res.changes, change{
			action: changeDroppedIllegal, item: b.item, detail: b.reason,
		})
	}
	oldByTag := map[string]map[string]any{}
	for _, ib := range oldInbounds {
		if tag := asString(ib["tag"]); tag != "" {
			oldByTag[tag] = ib
		}
	}
	fileByTag := map[string]map[string]any{}
	for _, ib := range fileInbounds {
		fileByTag[asString(ib["tag"])] = ib
	}

	classifyTopLevel(oldDoc, fileDoc, panel, &res)
	classifyLog(oldDoc, fileDoc, &res)
	classifyExperimental(oldDoc, fileDoc, &res)
	classifyOutbounds(oldDoc, fileDoc, &res)
	classifyInbounds(oldDoc, fileDoc, oldByTag, fileByTag, panel, firstImport, &res)
	return res
}

// inboundList extracts the inbounds array (empty when absent).
func inboundList(doc map[string]any) []map[string]any {
	raw, ok := asSlice(doc["inbounds"])
	if !ok {
		return nil
	}
	out := []map[string]any{}
	for _, v := range raw {
		if m, ok := asMap(v); ok {
			out = append(out, m)
		}
	}
	return out
}

type badEntry struct{ item, reason string }

// inboundListChecked extracts inbounds, dropping structurally invalid
// entries (missing tag/type, duplicate tags) as illegal instead of
// letting them poison the diff. Those entries never reach the merge.
func inboundListChecked(doc map[string]any) ([]map[string]any, []badEntry) {
	raw, ok := asSlice(doc["inbounds"])
	if !ok {
		if _, present := doc["inbounds"]; present {
			return nil, []badEntry{{"inbounds", "inbounds is not an array"}}
		}
		return nil, nil
	}
	seen := map[string]bool{}
	out := []map[string]any{}
	var bad []badEntry
	for i, v := range raw {
		m, ok := asMap(v)
		if !ok {
			bad = append(bad, badEntry{fmt.Sprintf("inbounds[%d]", i), "inbound is not an object"})
			continue
		}
		tag := asString(m["tag"])
		typ := asString(m["type"])
		if tag == "" {
			bad = append(bad, badEntry{fmt.Sprintf("inbounds[%d]", i), "inbound without tag"})
			continue
		}
		if typ == "" {
			bad = append(bad, badEntry{"inbounds[" + tag + "]", "inbound without type"})
			continue
		}
		if seen[tag] {
			bad = append(bad, badEntry{"inbounds[" + tag + "]", "duplicate tag"})
			continue
		}
		seen[tag] = true
		out = append(out, m)
	}
	return out, bad
}

// classifyTopLevel handles keys outside the panel's fixed skeleton
// (log, experimental, inbounds, outbounds): they are file-owned, so
// additions and changes are preserved verbatim and removals are
// recorded (the merged config simply omits them).
func classifyTopLevel(oldDoc, fileDoc map[string]any, panel panelState, res *classifyResult) {
	skeleton := map[string]bool{"log": true, "experimental": true, "inbounds": true, "outbounds": true}
	for k, fv := range fileDoc {
		if skeleton[k] {
			continue
		}
		// Passthrough is presence-driven, not diff-driven: every
		// file-owned key present in the file must reach the merged
		// config, changed or not, or an unchanged hand-written
		// section would be dropped by the next regeneration. The
		// change log, by contrast, records diffs only.
		res.passes.top[k] = deepCopyJSON(fv)
		ov, inOld := oldDoc[k]
		if !inOld {
			res.changes = append(res.changes, change{action: changePassthrough, item: k, newVal: valStr(fv, false), detail: "文件新增，透传保留"})
		} else if !jsonEqual(ov, fv) {
			res.changes = append(res.changes, change{action: changePassthrough, item: k, oldVal: valStr(ov, false), newVal: valStr(fv, false), detail: "文件改动，透传保留"})
		}
	}
	for k, ov := range oldDoc {
		if skeleton[k] {
			continue
		}
		if _, inFile := fileDoc[k]; !inFile {
			res.changes = append(res.changes, change{action: changePassthrough, item: k, oldVal: valStr(ov, false), detail: "文件删除，透传移除"})
		}
	}
}

// classifyLog: log.level is file-owned (the panel has no setting for
// it); timestamp/output are panel-owned skeleton (file edits to them
// are reverted); every other log field is preserved verbatim.
func classifyLog(oldDoc, fileDoc map[string]any, res *classifyResult) {
	oldLog, _ := asMap(oldDoc["log"])
	fileLog, fileIsMap := asMap(fileDoc["log"])
	if _, present := fileDoc["log"]; present && !fileIsMap {
		res.changes = append(res.changes, change{action: changeDroppedIllegal, item: "log", detail: "log is not an object, panel log block restored"})
		fileLog = nil
	}
	if fileLog == nil {
		return // skeleton restored by generation; no log row
	}
	for field, fv := range fileLog {
		ov, inOld := oldLog[field]
		switch field {
		case "level":
			res.passes.logSet["level"] = deepCopyJSON(fv)
			if !inOld || !jsonEqual(ov, fv) {
				res.changes = append(res.changes, change{action: changePassthrough, item: "log.level", oldVal: valStr(ov, false), newVal: valStr(fv, false), detail: "文件所有，透传保留"})
			}
		case "timestamp", "output":
			if inOld && !jsonEqual(ov, fv) {
				res.changes = append(res.changes, change{action: changeReverted, item: "log." + field, oldVal: valStr(ov, false), newVal: valStr(fv, false), detail: "面板所有，恢复面板值"})
			}
		default:
			res.passes.logSet[field] = deepCopyJSON(fv)
			if !inOld || !jsonEqual(ov, fv) {
				res.changes = append(res.changes, change{action: changePassthrough, item: "log." + field, oldVal: valStr(ov, false), newVal: valStr(fv, false), detail: "文件所有，透传保留"})
			}
		}
	}
	for field, ov := range oldLog {
		if _, inFile := fileLog[field]; inFile {
			continue
		}
		switch field {
		case "level":
			// Level removed: the merged config omits it (panel
			// default info would otherwise silently replace debug).
			res.passes.logDel = append(res.passes.logDel, "level")
			res.changes = append(res.changes, change{action: changePassthrough, item: "log.level", oldVal: valStr(ov, false), detail: "文件删除，透传移除"})
		case "timestamp", "output":
			// Skeleton restore; no log row.
		default:
			res.changes = append(res.changes, change{action: changePassthrough, item: "log." + field, oldVal: valStr(ov, false), detail: "文件删除，透传移除"})
		}
	}
}

// classifyExperimental: clash_api.external_controller is panel-owned;
// every other experimental key is file-owned passthrough.
func classifyExperimental(oldDoc, fileDoc map[string]any, res *classifyResult) {
	oldExp, _ := asMap(oldDoc["experimental"])
	fileExp, fileIsMap := asMap(fileDoc["experimental"])
	if _, present := fileDoc["experimental"]; present && !fileIsMap {
		res.changes = append(res.changes, change{action: changeDroppedIllegal, item: "experimental", detail: "experimental is not an object, panel block restored"})
		fileExp = nil
	}
	if fileExp == nil {
		return
	}
	oldClash, _ := asMap(oldExp["clash_api"])
	fileClash, _ := asMap(fileExp["clash_api"])
	if fileClash != nil {
		if ov, inOld := oldClash["external_controller"]; inOld {
			if fv := fileClash["external_controller"]; !jsonEqual(ov, fv) {
				res.changes = append(res.changes, change{action: changeReverted, item: "experimental.clash_api.external_controller", oldVal: valStr(ov, false), newVal: valStr(fv, false), detail: "面板所有，恢复面板值"})
			}
		}
		for k, v := range fileClash {
			if k == "external_controller" {
				continue
			}
			// clash_api extras are preserved under their own key
			// inside the generated clash_api block.
			res.passes.expSet["clash_api."+k] = deepCopyJSON(v)
			if ov, inOld := oldClash[k]; !inOld || !jsonEqual(ov, v) {
				res.changes = append(res.changes, change{action: changePassthrough, item: "experimental.clash_api." + k, oldVal: valStr(ov, false), newVal: valStr(v, false), detail: "文件所有，透传保留"})
			}
		}
	}
	for k, fv := range fileExp {
		if k == "clash_api" {
			continue
		}
		res.passes.expSet[k] = deepCopyJSON(fv)
		ov, inOld := oldExp[k]
		if !inOld || !jsonEqual(ov, fv) {
			res.changes = append(res.changes, change{action: changePassthrough, item: "experimental." + k, oldVal: valStr(ov, false), newVal: valStr(fv, false), detail: "文件所有，透传保留"})
		}
	}
	// Removed non-clash experimental keys vanish from the merged
	// config automatically (generation only writes clash_api).
	for k, ov := range oldExp {
		if k == "clash_api" {
			continue
		}
		if _, inFile := fileExp[k]; !inFile {
			res.changes = append(res.changes, change{action: changePassthrough, item: "experimental." + k, oldVal: valStr(ov, false), detail: "文件删除，透传移除"})
		}
	}
}

// classifyOutbounds: the direct outbound is panel skeleton; every
// other outbound is file-owned passthrough, matched by tag.
func classifyOutbounds(oldDoc, fileDoc map[string]any, res *classifyResult) {
	oldOut := outboundByTag(oldDoc)
	fileOut := outboundByTag(fileDoc)
	for tag, fv := range fileOut {
		if tag == "direct" {
			// Extra fields on direct are preserved as field-level
			// overrides (appending the whole object would duplicate
			// the tag). Type/tag changes are skeleton, reverted by
			// generation without a log row.
			ov := oldOut["direct"]
			for k, v := range fv {
				if k == "type" || k == "tag" {
					continue
				}
				res.passes.directSet[k] = deepCopyJSON(v)
				if ovv, inOld := ov[k]; !inOld || !jsonEqual(ovv, v) {
					res.changes = append(res.changes, change{action: changePassthrough, item: "outbounds[direct]." + k, oldVal: valStr(ovv, false), newVal: valStr(v, false), detail: "文件所有，透传保留"})
				}
			}
			if ov != nil {
				for k, ovv := range ov {
					if k == "type" || k == "tag" {
						continue
					}
					if _, inFile := fv[k]; !inFile {
						res.changes = append(res.changes, change{action: changePassthrough, item: "outbounds[direct]." + k, oldVal: valStr(ovv, false), detail: "文件删除，透传移除"})
					}
				}
			}
			continue
		}
		ov, inOld := oldOut[tag]
		res.passes.outExtra = append(res.passes.outExtra, deepCopyJSON(fv))
		if !inOld || !jsonEqual(ov, fv) {
			if inOld {
				res.changes = append(res.changes, change{action: changePassthrough, item: "outbounds[" + tag + "]", detail: "文件改动，透传保留"})
			} else {
				res.changes = append(res.changes, change{action: changePassthrough, item: "outbounds[" + tag + "]", detail: "文件新增，透传保留"})
			}
		}
	}
	for tag := range oldOut {
		if tag == "direct" {
			continue
		}
		if _, inFile := fileOut[tag]; !inFile {
			res.changes = append(res.changes, change{action: changePassthrough, item: "outbounds[" + tag + "]", detail: "文件删除，透传移除"})
		}
	}
}

func outboundByTag(doc map[string]any) map[string]map[string]any {
	raw, ok := asSlice(doc["outbounds"])
	if !ok {
		return map[string]map[string]any{}
	}
	out := map[string]map[string]any{}
	for _, v := range raw {
		if m, ok := asMap(v); ok {
			if tag := asString(m["tag"]); tag != "" {
				out[tag] = m
			}
		}
	}
	return out
}

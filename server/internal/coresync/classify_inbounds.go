package coresync

import (
	"encoding/json"
	"fmt"
	"strings"

	"singbox-ui/server/internal/store"
)

// classifyInbounds diffs the inbounds arrays by tag (the panel
// identity: tag == panel name). Panel-wins: a tag owned by a panel
// row is regenerated whole from the row — file edits to it revert,
// never import. A tag the panel does not own is unmanaged: on first
// import it is imported when its protocol is modelled (else
// passthrough); afterwards it is preserved whole as passthrough
// (never imported — the one-time import already happened).
func classifyInbounds(oldDoc, fileDoc map[string]any, oldByTag, fileByTag map[string]map[string]any, panel panelState, firstImport bool, res *classifyResult) {
	hy2ServerNameDecision(fileByTag, oldByTag, panel, firstImport, res)

	for tag, fileIb := range fileByTag {
		if panel.passTags[tag] {
			// Established unmanaged inbound: preserve verbatim.
			// The change log records diffs only.
			res.passes.inbound(tag).whole = deepCopyJSON(fileIb)
			if oldIb, inOld := oldByTag[tag]; !inOld {
				res.changes = append(res.changes, change{action: changePassthrough, item: "inbounds[" + tag + "]", detail: "文件新增，透传保留"})
			} else if !jsonEqual(oldIb, fileIb) {
				res.changes = append(res.changes, change{action: changePassthrough, item: "inbounds[" + tag + "]", detail: "文件改动，透传保留"})
			}
			continue
		}
		oldIb, inOld := oldByTag[tag]
		row, inPanel := panel.byTag[tag]
		if !inOld {
			if inPanel {
				// Panel row created after the baseline shares the
				// tag with a hand-added file copy: the panel row
				// wins (same resolution as first import's row
				// branch — agree converges, differ reverts).
				// A disabled row generates nothing, so an
				// agreeing copy's userAppend is ignored by merge
				// and the file copy is still dropped.
				classifyNewInbound(tag, fileIb, row, panel, res)
			} else if firstImport {
				classifyNewInbound(tag, fileIb, row, panel, res)
			} else {
				// Post-baseline new tag: unmanaged, preserved
				// whole (never imported — the one-time import
				// already happened at install).
				res.passes.inbound(tag).whole = deepCopyJSON(fileIb)
				res.changes = append(res.changes, change{action: changePassthrough, item: "inbounds[" + tag + "]", detail: "文件新增，透传保留（面板无此入站）"})
			}
			continue
		}
		if !inPanel {
			// Row deleted in the panel after the baseline: the panel
			// delete wins. File edits to it are reverted, not
			// imported; an unchanged file copy simply converges
			// when regeneration drops it.
			if !jsonEqual(oldIb, fileIb) {
				res.changes = append(res.changes, change{action: changeReverted, item: "inbounds[" + tag + "]", detail: "面板已删除该入站，文件改动不采纳"})
			}
			continue
		}
		classifyMatchedInbound(tag, oldIb, fileIb, row, panel, res)
	}

	// Deletions: a tag the file no longer carries. A row that still
	// exists in the panel is restored by regeneration (panel-wins);
	// a tag that is neither in the file nor in the panel is already
	// converged (panel delete or passthrough removal) and needs no
	// action.
	for tag, oldIb := range oldByTag {
		if _, inFile := fileByTag[tag]; inFile {
			continue
		}
		if panel.passTags[tag] {
			res.changes = append(res.changes, change{action: changePassthrough, item: "inbounds[" + tag + "]", oldVal: valStr(oldIb, false), detail: "文件删除，透传移除"})
			continue
		}
		row, inPanel := panel.byTag[tag]
		if !inPanel {
			continue
		}
		res.changes = append(res.changes, change{action: changeReverted, item: "inbounds[" + tag + "]", detail: "文件删除，面板恢复：" + inboundSummary(row, oldIb)})
	}
}

func inboundSummary(row *store.InboundRow, oldIb map[string]any) string {
	// Full detail minus secrets: name, protocol, port and the
	// non-secret shape of the old config fragment.
	parts := []string{"名称 " + row.Name, "协议 " + row.Protocol, "端口 " + fmt.Sprintf("%d", row.Port)}
	if oldIb != nil {
		frag, _ := json.Marshal(maskSecrets(oldIb))
		parts = append(parts, "原配置 "+string(frag))
	}
	return strings.Join(parts, "；")
}

// maskSecrets deep-copies a config fragment with credential leaves
// blanked (uuid, password, private keys). Structure stays complete.
func maskSecrets(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, val := range t {
			if secretField(k) {
				out[k] = ""
			} else {
				out[k] = maskSecrets(val)
			}
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = maskSecrets(val)
		}
		return out
	default:
		return v
	}
}

// hy2ServerNameDecision resolves the hysteria2 tls.server_name, which
// is a single panel setting shared by all hy2 inbounds.
//
// Panel-wins (post-baseline): a file server_name that differs from
// the panel setting is REVERTED (regen writes the panel host for all
// hy2 inbounds); no import, no per-inbound passthrough. A file
// REMOVAL of server_name reverts the same way (regen restores it).
//
// First import (no baseline): if the panel setting already has a
// value, it wins as above. If the setting is unset, a UNIFORM file
// value imports into the setting (exactly one value exists); a
// DIVERGENT set has no single panel value, so each inbound keeps its
// own via fieldSet passthrough; an invalid uniform value stays
// per-inbound via fieldSet (the setting's validator would reject it).
//
// Only enabled, panel-managed hy2 inbounds are collected here —
// passTags (unmanaged) and disabled rows are resolved in the
// whole-inbound branch. A server_name EQUAL to the panel host logs
// nothing.
func hy2ServerNameDecision(fileByTag, oldByTag map[string]map[string]any, panel panelState, firstImport bool, res *classifyResult) {
	type hy2Entry struct {
		tag  string
		name string // server_name, "" when absent
	}
	collect := func(byTag map[string]map[string]any) []hy2Entry {
		var entries []hy2Entry
		for tag, ib := range byTag {
			if panel.passTags[tag] {
				continue // unmanaged: its server_name lives in its whole object
			}
			proto, ok := panelProtocol(ib)
			if !ok || proto != "hysteria2" {
				continue
			}
			// Only enabled panel-managed hy2 inbounds: disabled
			// rows revert in their own branch.
			if row, ok := panel.byTag[tag]; !ok || !row.Enabled {
				continue
			}
			tls, _ := asMap(ib["tls"])
			entries = append(entries, hy2Entry{tag: tag, name: asString(tls["server_name"])})
		}
		return entries
	}
	entries := collect(fileByTag)
	if len(entries) == 0 {
		return
	}
	// Baseline gate, same as port/params: a panel-side host edit
	// alone (file == baseline) earns no row — the file just
	// converges. On first import the baseline is empty, so any file
	// value differing from the setting counts as a file value.
	baseByTag := map[string]string{}
	for _, e := range collect(oldByTag) {
		baseByTag[e.tag] = e.name
	}
	revertEntries := func(entries []hy2Entry) {
		for _, e := range entries {
			if e.name == panel.publicHost {
				continue
			}
			if e.name == baseByTag[e.tag] {
				continue // file unchanged; panel-side edit converges silently
			}
			if e.name == "" {
				res.changes = append(res.changes, change{action: changeReverted, item: "inbounds[" + e.tag + "].tls.server_name", oldVal: panel.publicHost, detail: "文件删除，面板值恢复"})
			} else {
				res.changes = append(res.changes, change{action: changeReverted, item: "inbounds[" + e.tag + "].tls.server_name", oldVal: panel.publicHost, newVal: e.name, detail: "文件改动，恢复面板值"})
			}
		}
	}
	if !firstImport {
		// Panel-wins: any file deviation reverts to the setting.
		revertEntries(entries)
		return
	}
	// First import.
	if panel.publicHost != "" {
		// Setting already exists: it wins; divergent file values
		// revert to it (no fieldSet).
		revertEntries(entries)
		return
	}
	uniform := true
	for _, e := range entries[1:] {
		if e.name != entries[0].name {
			uniform = false
			break
		}
	}
	if !uniform {
		// Divergent: no single panel value exists; each inbound
		// keeps its own via fieldSet passthrough.
		for _, e := range entries {
			if e.name == "" {
				continue
			}
			res.passes.inbound(e.tag).fieldSet["tls.server_name"] = e.name
			res.changes = append(res.changes, change{action: changePassthrough, item: "inbounds[" + e.tag + "].tls.server_name", oldVal: panel.publicHost, newVal: e.name, detail: "各入站不一致，透传保留"})
		}
		return
	}
	want := entries[0].name
	if want == "" || want == panel.publicHost {
		return // no SNI signal, or already converged
	}
	if !validPublicHost(want) {
		// Invalid for the setting: keep each file value verbatim.
		for _, e := range entries {
			res.passes.inbound(e.tag).fieldSet["tls.server_name"] = e.name
		}
		res.changes = append(res.changes, change{action: changePassthrough, item: "settings.publicHost", oldVal: panel.publicHost, newVal: want, detail: "设置值非法，透传保留"})
		return
	}
	res.imports = append(res.imports, importOp{kind: "settingHost", host: want, changes: []change{{action: changeUpdated, item: "settings.publicHost", oldVal: panel.publicHost, newVal: want, detail: "文件改动，覆盖面板"}}})
}

// validPublicHost mirrors the settings validation for sub_public_host:
// bare domain or IP, no scheme, path or whitespace, at most 255 chars.
func validPublicHost(host string) bool {
	if len(host) > 255 {
		return false
	}
	if strings.ContainsAny(host, " \t\r\n/") || strings.Contains(host, "://") {
		return false
	}
	return true
}

// classifyNewInbound handles a tag the baseline never had and the
// panel does not already own: create it (same validation as the UI),
// preserve it verbatim when its protocol is unmodelled, or drop it
// when its shape cannot be represented.
// uniformPasswordOf returns the single password shared by all users
// that carry one. present is false when no user carries a password
// field at all; uniform is false when they disagree.
func uniformPasswordOf(users []map[string]any) (pw string, uniform bool, present bool) {
	var u *string
	for _, x := range users {
		raw, ok := x["password"]
		if !ok {
			continue
		}
		present = true
		s := asString(raw)
		if u == nil {
			v := s
			u = &v
		} else if *u != s {
			return *u, false, true
		}
	}
	if u == nil {
		return "", false, false
	}
	return *u, true, true
}

// uniformFlowOf is uniformPasswordOf for the flow field: present
// means at least one user carries a flow key.
func uniformFlowOf(users []map[string]any) (flow string, uniform bool, present bool) {
	var u *string
	for _, x := range users {
		raw, ok := x["flow"]
		if !ok {
			continue
		}
		present = true
		s := asString(raw)
		if u == nil {
			v := s
			u = &v
		} else if *u != s {
			return *u, false, true
		}
	}
	if u == nil {
		return "", false, false
	}
	return *u, true, true
}

// uniformFilePassword returns the single password shared by all file
// users that carry one. Users without a password field are skipped.
// ok is false when no user carries a password or they disagree.
func uniformFilePassword(users []map[string]any) (string, bool) {
	var uniform *string
	for _, u := range users {
		raw, present := u["password"]
		if !present {
			continue
		}
		pws := asString(raw)
		if uniform == nil {
			v := pws
			uniform = &v
		} else if *uniform != pws {
			return "", false
		}
	}
	if uniform == nil {
		return "", false
	}
	return *uniform, true
}

func classifyNewInbound(tag string, fileIb map[string]any, row *store.InboundRow, panel panelState, res *classifyResult) {
	fileUsers := userList(fileIb)
	if row != nil {
		// Panel row created after the baseline AND a hand-added file
		// copy share the tag. If they agree, converged; if they
		// differ, the panel row wins (the UI creation is the
		// authoritative record) and the file copy is reverted.
		proto, ok := panelProtocol(fileIb)
		if !ok || proto != row.Protocol || portOf(fileIb["listen_port"]) != row.Port {
			res.changes = append(res.changes, change{action: changeReverted, item: "inbounds[" + tag + "]", detail: "面板新建后文件同名入站不一致，恢复面板值"})
			return
		}
		params, _, _, err := mappedParams(proto, fileIb)
		if err != nil {
			res.changes = append(res.changes, change{action: changeReverted, item: "inbounds[" + tag + "]", detail: "面板新建后文件同名入站无法解析，恢复面板值"})
			return
		}
		// Resolve the shared password before comparing, or a copy
		// that agrees on sni AND password would still read as
		// different (mappedParams alone carries no password for
		// these protocols) and be wrongly reverted. Same for the
		// vless flow: mappedParams leaves flow to the users loop,
		// so a copy that agrees on everything including a uniform
		// flow would otherwise read as different.
		if proto == "hysteria2" || proto == "trojan" || proto == "anytls" {
			if pw, ok := uniformFilePassword(fileUsers); ok && pw != "" {
				params["password"] = pw
			}
		}
		if proto == "vless-reality" {
			if flow, uniform, present := uniformFlowOf(fileUsers); uniform && present {
				params["flow"] = flow
			}
		}
		var panelParams map[string]any
		_ = json.Unmarshal([]byte(row.Params), &panelParams)
		if !paramsEqual(params, panelParams) {
			res.changes = append(res.changes, change{action: changeReverted, item: "inbounds[" + tag + "]", detail: "面板新建后文件同名入站参数不一致，恢复面板值"})
			return
		}
		// Converged: the file users survive as passthrough until a
		// template binds (same as a fresh create below).
		for _, u := range fileUsers {
			res.passes.inbound(tag).userAppend = append(res.passes.inbound(tag).userAppend, deepCopyJSON(u))
		}
		return
	}
	proto, ok := panelProtocol(fileIb)
	if !ok {
		res.passes.inbound(tag).whole = deepCopyJSON(fileIb)
		res.changes = append(res.changes, change{action: changePassthrough, item: "inbounds[" + tag + "]", detail: "未知协议，透传保留（面板无此协议）"})
		return
	}
	params, _, _, err := mappedParams(proto, fileIb)
	if err != nil {
		res.changes = append(res.changes, change{action: changeDroppedIllegal, item: "inbounds[" + tag + "]", detail: "新建入站无法解析：" + err.Error()})
		return
	}
	// Password protocols: adopt the file's uniform user password as
	// the row password, so the created row carries the file's
	// credential instead of an auto-generated random one (which
	// would be served to bound subs until a later sync converged
	// it). Divergent/absent passwords leave the field unset and
	// the row is created with an auto-generated password; the file
	// users below still preserve connectivity.
	if proto == "hysteria2" || proto == "trojan" || proto == "anytls" {
		if pw, ok := uniformFilePassword(fileUsers); ok && pw != "" {
			params["password"] = pw
		}
	}
	// Users inside a new inbound are NOT auto-bound: subscriptions
	// bind via templates, a panel-side governance decision. They
	// are preserved as passthrough so existing users keep working
	// until a template binds; a bound sub of the same identity
	// then takes over via the users loop.
	for _, u := range fileUsers {
		res.passes.inbound(tag).userAppend = append(res.passes.inbound(tag).userAppend, deepCopyJSON(u))
	}
	detail := "文件新增，写入面板（协议 " + proto + "，端口 " + fmt.Sprintf("%d", portOf(fileIb["listen_port"])) + "）"
	if len(fileUsers) > 0 {
		detail += "；文件中的用户已透传保留，模板绑定后同身份用户由订阅接管"
	}
	res.imports = append(res.imports, importOp{
		kind: "inboundCreate", name: tag, protocol: proto,
		port: portOf(fileIb["listen_port"]), params: marshalMap(params),
		changes: []change{{action: changeAdded, item: "inbounds[" + tag + "]", detail: detail}},
	})
}

func paramsEqual(a, b map[string]any) bool {
	// Compare after normalizing through JSON (numbers unify).
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	var av, bv any
	_ = json.Unmarshal(ab, &av)
	_ = json.Unmarshal(bb, &bv)
	return jsonEqual(av, bv)
}

func marshalMap(m map[string]any) string {
	b, _ := json.Marshal(m)
	return string(b)
}

// classifyMatchedInbound handles a tag present in the baseline and
// owned by an enabled panel row: port, params, and user identity.
// Panel-wins: the panel row is the truth. A file value that differs
// from the panel value is REVERTED by regeneration (no import).
// Extra hand-added fields inside the managed inbound are dropped on
// regen (the whole inbound is panel-generated); unknown users are
// the exception — whole user entries are preserved as passthrough
// (see classifyUsers), not fields. A protocol change is never
// converted in place (the panel cannot change a row's protocol):
// the whole inbound is reverted.
func classifyMatchedInbound(tag string, oldIb, fileIb map[string]any, row *store.InboundRow, panel panelState, res *classifyResult) {
	proto, ok := panelProtocol(fileIb)
	if !ok {
		// The file changed this inbound to an unmodelled protocol.
		// Reverting (panel protocol wins) is the only safe move: the
		// row cannot become a passthrough object while a panel row
		// owns the tag.
		res.changes = append(res.changes, change{action: changeReverted, item: "inbounds[" + tag + "]", detail: "文件改为面板无此协议，恢复面板值"})
		return
	}
	if proto != row.Protocol {
		res.changes = append(res.changes, change{action: changeReverted, item: "inbounds[" + tag + "]", detail: "协议不能原地变更（" + row.Protocol + "→" + proto + "），恢复面板值"})
		return
	}
	if !row.Enabled || !row.RunLocal {
		// Disabled row, or nodes-only row: the file copy is reverted
		// (regen drops it — Generate excludes both). An unchanged
		// file copy converges silently via regen; only a changed
		// copy earns a row. The tag stays panel-owned either way:
		// a nodes-only row must not let a stale local copy linger
		// as passthrough.
		if !jsonEqual(oldIb, fileIb) {
			detail := "面板已停用该入站，文件改动不采纳"
			if row.Enabled {
				detail = "该入站仅跑在节点上，主服务器不跑，文件副本不采纳"
			}
			res.changes = append(res.changes, change{action: changeReverted, item: "inbounds[" + tag + "]", detail: detail})
		}
		return
	}

	// Port: panel wins for the VALUE, but the ROW fires only when
	// the file actually changed: file != baseline AND file !=
	// panel. A panel-side port edit alone (file == baseline) must
	// not read as a file deviation — the file just converges
	// silently (S0).
	if newPort := portOf(fileIb["listen_port"]); newPort != row.Port && newPort != portOf(oldIb["listen_port"]) {
		res.changes = append(res.changes, change{action: changeReverted, item: "inbounds[" + tag + "].listen_port", oldVal: fmt.Sprintf("%d", row.Port), newVal: fmt.Sprintf("%d", newPort), detail: "文件改动，恢复面板值"})
	}

	// Params: mapped file fields against panel params (panel wins
	// for the value). Same baseline gate as the port: a panel-side
	// param edit alone earns no row.
	fileParams, delFields, unmapped, err := mappedParams(proto, fileIb)
	if err != nil {
		res.changes = append(res.changes, change{action: changeReverted, item: "inbounds[" + tag + "]", detail: "参数无法解析，恢复面板值：" + err.Error()})
	} else {
		var panelParams map[string]any
		_ = json.Unmarshal([]byte(row.Params), &panelParams)
		if panelParams == nil {
			panelParams = map[string]any{}
		}
		oldParams, _, _, oldErr := mappedParams(proto, oldIb)
		for k, v := range fileParams {
			if pv, ok := panelParams[k]; !ok || !jsonEqual(pv, v) {
				// Baseline gate: skip when the file value equals
				// the baseline value (panel-side edit, file just
				// converges). An unparseable baseline cannot prove
				// panel-side, so the row fires.
				if oldErr == nil {
					if ov, ok := oldParams[k]; ok && jsonEqual(ov, v) {
						continue
					}
				}
				res.changes = append(res.changes, change{action: changeReverted, item: "inbounds[" + tag + "]." + k, oldVal: valStr(panelParams[k], secretField(k)), newVal: valStr(v, secretField(k)), detail: "文件改动，恢复面板值"})
			}
		}
		for path := range unmapped {
			// Extra hand-added field inside a managed inbound:
			// dropped on regen (whole inbound is panel-generated).
			res.changes = append(res.changes, change{action: changeReverted, item: "inbounds[" + tag + "]." + path, detail: "面板生成整个入站，手加字段不保留"})
		}
		for _, path := range delFields {
			if _, present := getFieldPath(oldIb, path); present {
				res.changes = append(res.changes, change{action: changeReverted, item: "inbounds[" + tag + "]." + path, detail: "文件删除，面板值恢复"})
			}
		}
	}

	// Users: identity only (uuid, rename, unknown, absence). Password
	// and flow uniformity live in the helpers below (panel-wins).
	classifyUsers(tag, oldIb, fileIb, row, panel, res)
}

// classifyUsers resolves user entries inside one matched inbound.
// Panel-wins: a file change to a panel subscription's identity
// (uuid, name) is REVERTED (regen writes the panel values), never
// imported. Unknown users are preserved verbatim (never auto-created
// — a bare user entry lacks template, limit, expiry and token, so a
// subscription cannot be built from it safely); absent users are
// noted, never deleted (regen restores them).
// panelSubByCoreName resolves a core username back to its panel
// subscription for one inbound. Password protocols generate
// coreUserName(sub, tag); vless keeps the plain subscription name,
// which panel.byName already covers, so this is only the renamed
// case. It is an exact map over panel subs, never a parse of the
// name, so hyphenated subscription/inbound names cannot mislead it.
func panelSubByCoreName(tag string, row *store.InboundRow, panel panelState, name string) (*store.SubscriptionRow, bool) {
	switch row.Protocol {
	case "hysteria2", "tuic", "anytls", "trojan":
	default:
		return nil, false
	}
	for i := range panel.subs {
		if coreUserName(panel.subs[i].Name, tag) == name {
			return &panel.subs[i], true
		}
	}
	return nil, false
}

func classifyUsers(tag string, oldIb, fileIb map[string]any, row *store.InboundRow, panel panelState, res *classifyResult) {
	fileUsers := userList(fileIb)
	oldUsers := userList(oldIb)
	oldByName := map[string]map[string]any{}
	oldByUUID := map[string]map[string]any{}
	for _, u := range oldUsers {
		if n := asString(u["name"]); n != "" {
			oldByName[n] = u
		}
		if id := asString(u["uuid"]); id != "" {
			oldByUUID[id] = u
		}
	}
	fileByName := map[string]map[string]any{}
	for _, u := range fileUsers {
		if n := asString(u["name"]); n != "" {
			fileByName[n] = u
		}
	}
	hasUUID := row.Protocol == "vless-reality" || row.Protocol == "tuic"

	// Password uniformity (password protocols) and flow uniformity
	// (vless): panel values win; divergent per-user overrides are
	// dropped by regen (see the helpers).
	if row.Protocol == "hysteria2" || row.Protocol == "tuic" || row.Protocol == "anytls" || row.Protocol == "trojan" {
		passwordUniformity(tag, fileUsers, oldUsers, row, res)
	}
	if row.Protocol == "vless-reality" {
		flowUniformity(tag, fileUsers, oldUsers, row, res)
	}

	for name, fu := range fileByName {
		// Baseline identity first: same name, else same uuid. A
		// file user the baseline never had is file-added, never a
		// rename of something the panel changed meanwhile.
		var bu map[string]any
		if u, ok := oldByName[name]; ok {
			bu = u
		} else if hasUUID {
			if fuuid := asString(fu["uuid"]); fuuid != "" {
				bu = oldByUUID[fuuid]
			}
		}
		if row.Protocol == "trojan" && name == tag {
			// Trojan's single core user (named after the
			// inbound, see generate.go) is panel-owned, not
			// a stranger: regen writes it, so it is never
			// passthrough-appended. Password changes are
			// already covered by passwordUniformity above.
			if bu == nil {
				res.changes = append(res.changes, change{action: changeReverted, item: "inbounds[" + tag + "].users[" + name + "]", detail: "与面板订阅同名，恢复面板值（文件用户不保留）"})
			}
			continue
		}
		if bu == nil {
			// Name collision with a panel sub: panel names are
			// panel-owned identities, so a file user carrying one
			// is never a stranger — even when the baseline never
			// had it (e.g. the sub was created after the
			// baseline). Panel wins: drop it (no append) and say
			// reverted, not passthrough — otherwise merge's name
			// dedupe would drop it silently while the log claims
			// it was preserved.
			if _, ok := panel.byName[name]; ok {
				res.changes = append(res.changes, change{action: changeReverted, item: "inbounds[" + tag + "].users[" + name + "]", detail: "与面板订阅同名，恢复面板值（文件用户不保留）"})
				continue
			}
			if _, ok := panelSubByCoreName(tag, row, panel, name); ok {
				// Same, for a renamed core username: regen
				// writes it, so it is never a stranger to
				// preserve.
				res.changes = append(res.changes, change{action: changeReverted, item: "inbounds[" + tag + "].users[" + name + "]", detail: "与面板订阅同名，恢复面板值（文件用户不保留）"})
				continue
			}
			// Unknown user: preserved verbatim (never
			// auto-created — a bare user entry lacks template,
			// limit, expiry and token, so a subscription cannot
			// be built from it safely).
			res.passes.inbound(tag).userAppend = append(res.passes.inbound(tag).userAppend, deepCopyJSON(fu))
			res.changes = append(res.changes, change{action: changePassthrough, item: "inbounds[" + tag + "].users[" + name + "]", detail: "未知用户，透传保留（不自动建订阅）"})
			continue
		}
		// The panel sub continuing the baseline identity: same
		// baseline name, else same baseline uuid.
		var sub *store.SubscriptionRow
		if s, ok := panel.byName[asString(bu["name"])]; ok {
			sub = s
		} else if s, ok := panelSubByCoreName(tag, row, panel, asString(bu["name"])); ok {
			sub = s
		} else if hasUUID {
			if buuid := asString(bu["uuid"]); buuid != "" {
				if s, ok := panel.byUUID[buuid]; ok {
					sub = s
				}
			}
		}
		if sub == nil {
			// Panel deleted the sub: the file user survives as
			// passthrough. A row fires only when the file also
			// changed it (an unchanged user is content-identical,
			// just sourced differently now).
			res.passes.inbound(tag).userAppend = append(res.passes.inbound(tag).userAppend, deepCopyJSON(fu))
			if !jsonEqual(bu, fu) {
				res.changes = append(res.changes, change{action: changePassthrough, item: "inbounds[" + tag + "].users[" + name + "]", detail: "订阅已在面板删除，透传保留"})
			}
			continue
		}
		if jsonEqual(bu, fu) {
			continue // file unchanged; panel state stands
		}
		// File changed this user: panel wins (revert, no import).
		// Baseline gate, same as port/params: a panel-side identity
		// edit alone (file == baseline) earns no row.
		if hasUUID {
			if fuuid := asString(fu["uuid"]); fuuid != "" && fuuid != sub.UUID && fuuid != asString(bu["uuid"]) {
				res.changes = append(res.changes, change{action: changeReverted, item: "inbounds[" + tag + "].users[" + name + "].uuid", detail: "文件改动，恢复面板值（值已隐藏）"})
			}
		}
		if name != sub.Name && name != asString(bu["name"]) {
			res.changes = append(res.changes, change{action: changeReverted, item: "inbounds[" + tag + "].users[" + name + "].name", oldVal: sub.Name, newVal: name, detail: "文件改动，恢复面板值"})
		}
	}
	for name := range oldByName {
		if _, inFile := fileByName[name]; inFile {
			continue
		}
		// Absent user: note only. The panel restores it on regen;
		// disabling the subscription in the panel is the revoke path.
		if _, ok := panel.byName[name]; ok {
			res.changes = append(res.changes, change{action: changeRestoreNote, item: "inbounds[" + tag + "].users[" + name + "]", detail: "文件缺失该用户，同步后面板会恢复；如需撤销请在面板停用该订阅"})
		} else if _, ok := panelSubByCoreName(tag, row, panel, name); ok {
			res.changes = append(res.changes, change{action: changeRestoreNote, item: "inbounds[" + tag + "].users[" + name + "]", detail: "文件缺失该用户，同步后面板会恢复；如需撤销请在面板停用该订阅"})
		}
	}
}

func userList(ib map[string]any) []map[string]any {
	raw, ok := asSlice(ib["users"])
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

// passwordUniformity resolves file user passwords against the panel
// inbound password. Panel-wins: a uniform file password that differs
// from the panel password is reverted (regen writes the panel
// password for all users); divergent per-user passwords are dropped
// by regen (the panel has one inbound password, not per-user ones).
// No import, no per-user passthrough.
func passwordUniformity(tag string, fileUsers, oldUsers []map[string]any, row *store.InboundRow, res *classifyResult) {
	var panelPw string
	var panelParams map[string]any
	_ = json.Unmarshal([]byte(row.Params), &panelParams)
	if panelParams != nil {
		panelPw = asString(panelParams["password"])
	}
	filePw, fileUniform, filePresent := uniformPasswordOf(fileUsers)
	if !filePresent {
		return // no password signal in the file
	}
	if !fileUniform {
		res.changes = append(res.changes, change{action: changeReverted, item: "inbounds[" + tag + "].users", detail: "各用户密码不一致，面板密码恢复（逐用户覆盖已丢弃）"})
		return
	}
	// Baseline gate: a panel-side password edit alone (file ==
	// baseline uniform) earns no row — the file just converges.
	if basePw, baseUniform, _ := uniformPasswordOf(oldUsers); baseUniform && filePw == basePw {
		return
	}
	if filePw == panelPw {
		return
	}
	res.changes = append(res.changes, change{action: changeReverted, item: "inbounds[" + tag + "].password", detail: "文件改动，恢复面板值（值已隐藏）"})
}

// flowUniformity resolves file user flows against the panel inbound
// flow. Panel-wins: a uniform file flow that differs from the panel
// flow is reverted; divergent per-user flows are dropped by regen.
// A file REMOVAL of flow (baseline had it, panel still has it) is
// reverted (regen restores it); pre-baseline file silence leaves the
// panel flow standing without a row (it is a panel edit taking
// effect, not a file revert).
func flowUniformity(tag string, fileUsers, oldUsers []map[string]any, row *store.InboundRow, res *classifyResult) {
	var panelFlow string
	var panelParams map[string]any
	_ = json.Unmarshal([]byte(row.Params), &panelParams)
	if panelParams != nil {
		panelFlow = asString(panelParams["flow"])
	}
	fileFlow, fileUniform, filePresent := uniformFlowOf(fileUsers)
	_, _, basePresent := uniformFlowOf(oldUsers)
	if !filePresent {
		if basePresent && panelFlow != "" {
			res.changes = append(res.changes, change{action: changeReverted, item: "inbounds[" + tag + "].users.flow", oldVal: panelFlow, detail: "文件删除 flow，面板值恢复"})
		}
		return
	}
	if !fileUniform {
		res.changes = append(res.changes, change{action: changeReverted, item: "inbounds[" + tag + "].users.flow", detail: "各用户 flow 不一致，面板 flow 恢复（逐用户覆盖已丢弃）"})
		return
	}
	// Baseline gate: a panel-side flow edit alone (file == baseline
	// uniform) earns no row — the file just converges.
	if baseFlow, baseUniform, _ := uniformFlowOf(oldUsers); baseUniform && fileFlow == baseFlow {
		return
	}
	if fileFlow == panelFlow {
		return
	}
	res.changes = append(res.changes, change{action: changeReverted, item: "inbounds[" + tag + "].users.flow", oldVal: panelFlow, newVal: fileFlow, detail: "文件改动，恢复面板值"})
}

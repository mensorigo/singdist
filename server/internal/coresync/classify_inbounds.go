package coresync

import (
	"encoding/json"
	"fmt"
	"strings"

	"singbox-ui/server/internal/store"
)

// classifyInbounds diffs the inbounds arrays by tag (the panel
// identity: tag == panel name). It handles, in order: the hy2
// server_name group decision, per-tag new/matched/deleted logic, and
// user identity inside matched inbounds. All import ops are returned
// for the caller to execute and verify; passthrough entries are
// returned for the merge.
func classifyInbounds(oldDoc, fileDoc map[string]any, oldByTag, fileByTag map[string]map[string]any, panel panelState, res *classifyResult) {
	hy2ServerNameDecision(fileByTag, oldByTag, panel, res)

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
			classifyNewInbound(tag, fileIb, row, panel, res)
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

	// Deletions: a tag the file no longer carries. Only a row that
	// still exists in the panel is deleted; a tag that is neither in
	// the file nor in the panel is already converged (panel delete
	// or passthrough removal) and needs no action.
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
		res.imports = append(res.imports, importOp{kind: "inboundDelete", inboundID: row.ID, changes: []change{{
			action: changeDeleted, item: "inbounds[" + tag + "]",
			detail: "文件删除，面板跟删：" + inboundSummary(row, oldIb),
		}}})
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
// is a single panel setting shared by all hy2 inbounds. Only a
// UNIFORM file value may become the setting; divergent values stay
// per-inbound passthrough. An invalid setting value (scheme, path,
// overlong) also falls back to per-inbound passthrough instead of
// failing the whole sync.
func hy2ServerNameDecision(fileByTag, oldByTag map[string]map[string]any, panel panelState, res *classifyResult) {
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
			tls, _ := asMap(ib["tls"])
			entries = append(entries, hy2Entry{tag: tag, name: asString(tls["server_name"])})
		}
		return entries
	}
	entries := collect(fileByTag)
	if len(entries) == 0 {
		return
	}
	// Baseline uniform value: what the file had at last sync. The
	// file imports only when it differs from the BASELINE — a
	// panel-side setting edit must not read as a file change.
	baseEntries := collect(oldByTag)
	baseUniform := len(baseEntries) > 0
	baseWant := ""
	if baseUniform {
		baseWant = baseEntries[0].name
		for _, e := range baseEntries[1:] {
			if e.name != baseWant {
				baseUniform = false
				break
			}
		}
	}
	baseByTag := map[string]string{}
	for _, e := range baseEntries {
		baseByTag[e.tag] = e.name
	}
	uniform := true
	for _, e := range entries[1:] {
		if e.name != entries[0].name {
			uniform = false
			break
		}
	}
	if uniform {
		want := entries[0].name
		if baseUniform && want == baseWant {
			return // file unchanged; the panel setting stands
		}
		if want == panel.publicHost {
			return // already converged
		}
		if validPublicHost(want) {
			res.imports = append(res.imports, importOp{kind: "settingHost", host: want, changes: []change{{action: changeUpdated, item: "settings.publicHost", oldVal: panel.publicHost, newVal: want, detail: "文件改动，覆盖面板"}}})
			return
		}
		// Invalid for the setting: keep each file value verbatim.
		// Rows fire only where the value changed since baseline.
		for _, e := range entries {
			res.passes.inbound(e.tag).fieldSet["tls.server_name"] = e.name
			if bv, ok := baseByTag[e.tag]; !ok || bv != e.name {
				res.changes = append(res.changes, change{action: changePassthrough, item: "inbounds[" + e.tag + "].tls.server_name", newVal: e.name, detail: "设置值非法，透传保留"})
			}
		}
		return
	}
	// Divergent: preserve each file value where it differs from what
	// generation would produce (the current setting). Rows fire
	// only where the value changed since baseline.
	for _, e := range entries {
		if e.name != panel.publicHost {
			res.passes.inbound(e.tag).fieldSet["tls.server_name"] = e.name
			if bv, ok := baseByTag[e.tag]; !ok || bv != e.name {
				res.changes = append(res.changes, change{action: changePassthrough, item: "inbounds[" + e.tag + "].tls.server_name", oldVal: panel.publicHost, newVal: e.name, detail: "各入站不一致，透传保留"})
			}
		}
	}
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
// inboundCopyDiffers reports whether a file inbound copy differs
// from its baseline copy (port or mapped params). Used for the
// disabled-row re-enable decision: an unchanged file copy must not
// resurrect a row the panel just disabled.
func inboundCopyDiffers(oldIb, fileIb map[string]any, proto string) bool {
	if portOf(oldIb["listen_port"]) != portOf(fileIb["listen_port"]) {
		return true
	}
	oldParams, _, _, oldErr := mappedParams(proto, oldIb)
	fileParams, _, _, fileErr := mappedParams(proto, fileIb)
	if oldErr != nil || fileErr != nil {
		return true
	}
	return !paramsEqual(oldParams, fileParams)
}

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

// userFieldMap builds a name -> field-value mapping for users that
// carry the field (users without it count as absent, so gaining or
// losing the field changes the map).
func userFieldMap(users []map[string]any, field string) map[string]string {
	m := map[string]string{}
	for _, u := range users {
		name := asString(u["name"])
		if name == "" {
			continue
		}
		if raw, ok := u[field]; ok {
			m[name] = asString(raw)
		}
	}
	return m
}

func sameUserFieldMap(a, b []map[string]any, field string) bool {
	am, bm := userFieldMap(a, field), userFieldMap(b, field)
	if len(am) != len(bm) {
		return false
	}
	for k, v := range am {
		if bv, ok := bm[k]; !ok || bv != v {
			return false
		}
	}
	return true
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
		// these protocols) and be wrongly reverted.
		if proto == "hysteria2" || proto == "trojan" || proto == "anytls" {
			if pw, ok := uniformFilePassword(fileUsers); ok && pw != "" {
				params["password"] = pw
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
// A protocol change is never converted in place (the panel cannot
// change a row's protocol): the whole inbound is reverted.
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
	if !row.Enabled {
		// Disabled row re-added by the file: enabling is a panel
		// operation, so the file may enable it — but ONLY when the
		// file demonstrably (re-)added it (tag absent from the
		// baseline, or the copy differs from the baseline copy).
		// An unchanged file copy must NOT resurrect a row the panel
		// just disabled; the disable wins and regen drops it.
		if oldIb == nil || inboundCopyDiffers(oldIb, fileIb, proto) {
			res.imports = append(res.imports, importOp{kind: "inboundEnable", inboundID: row.ID, enabled: true, changes: []change{{action: changeUpdated, item: "inbounds[" + tag + "].enabled", oldVal: "停用", newVal: "启用", detail: "文件改动，覆盖面板"}}})
		}
	}

	// Port, field-grouped (validated independently of params). The
	// file's port imports only when it differs from the BASELINE
	// port: comparing against the panel row would revert a
	// panel-side port edit the file never touched.
	if newPort := portOf(fileIb["listen_port"]); newPort != portOf(oldIb["listen_port"]) {
		res.imports = append(res.imports, importOp{kind: "inboundPort", inboundID: row.ID, port: newPort, changes: []change{{action: changeUpdated, item: "inbounds[" + tag + "].listen_port", oldVal: fmt.Sprintf("%d", row.Port), newVal: fmt.Sprintf("%d", newPort), detail: "文件改动，覆盖面板"}}})
	}

	// Params: mapped file fields overlaid on panel params; unmapped
	// fields preserved verbatim; auto-fill absences omitted from the
	// generated object instead of letting validation invent a value.
	// The import decision is BASELINE-driven throughout: a mapped
	// field imports only when the file's value differs from the
	// baseline's, so a panel-side edit to another field (or the
	// same field) is never mistaken for a file change. The merge
	// entries (fieldSet/fieldDel) stay presence-driven — the merged
	// config must carry the file's content even when unchanged —
	// but their log rows fire only on baseline diffs.
	fileParams, delFields, unmapped, err := mappedParams(proto, fileIb)
	if err != nil {
		res.changes = append(res.changes, change{action: changeDroppedIllegal, item: "inbounds[" + tag + "]", detail: "参数无法解析：" + err.Error()})
	} else {
		var panelParams map[string]any
		_ = json.Unmarshal([]byte(row.Params), &panelParams)
		if panelParams == nil {
			panelParams = map[string]any{}
		}
		oldParams, _, _, oldErr := mappedParams(proto, oldIb)
		if oldErr != nil {
			// Baseline unparseable (should not happen for a
			// previously written doc): fall back to panel
			// comparison rather than dropping the file values.
			oldParams = panelParams
		}
		merged := map[string]any{}
		for k, v := range panelParams {
			merged[k] = v
		}
		changedFields := []string{}
		for k, v := range fileParams {
			if ov, inOld := oldParams[k]; !inOld || !jsonEqual(ov, v) {
				merged[k] = v
				changedFields = append(changedFields, k)
			}
		}
		if len(changedFields) > 0 {
			var rows []change
			for _, f := range changedFields {
				rows = append(rows, change{action: changeUpdated, item: "inbounds[" + tag + "]." + f, oldVal: valStr(panelParams[f], secretField(f)), newVal: valStr(fileParams[f], secretField(f)), detail: "文件改动，覆盖面板"})
			}
			res.imports = append(res.imports, importOp{kind: "inboundParams", inboundID: row.ID, params: marshalMap(merged), changes: rows})
		}
		for path, v := range unmapped {
			res.passes.inbound(tag).fieldSet[path] = deepCopyJSON(v)
			if ov, present := getFieldPath(oldIb, path); !present || !jsonEqual(ov, v) {
				res.changes = append(res.changes, change{action: changePassthrough, item: "inbounds[" + tag + "]." + path, newVal: valStr(v, secretField(leafField(path))), detail: "面板无此字段，透传保留"})
			}
		}
		for _, path := range delFields {
			// A del field reaches the merge only when the file
			// REMOVED it since the baseline. An omission that
			// predates the baseline is file silence, not a
			// removal — the panel value stands.
			if _, present := getFieldPath(oldIb, path); present {
				res.passes.inbound(tag).fieldDel = append(res.passes.inbound(tag).fieldDel, path)
				res.changes = append(res.changes, change{action: changePassthrough, item: "inbounds[" + tag + "]." + path, detail: "文件删除，透传移除（面板不自动补值）"})
			}
		}
	}

	// Users: identity only (uuid, rename, unknown, absence). Password
	// and flow uniformity live in the params step via helpers below.
	classifyUsers(tag, oldIb, fileIb, row, panel, res)
}

// classifyUsers resolves user entries inside one matched inbound.
// Matching is by name first, then by uuid (vless/tuic) for renames.
// Unknown users are preserved verbatim; absent users are noted, never
// deleted (a subscription is panel-side state, and absence alone
// cannot distinguish revoke from disable/expire/template change).
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

	// Password uniformity (password protocols): a uniform file
	// password becomes the inbound password via the params update;
	// divergent passwords stay per-user passthrough.
	if row.Protocol == "hysteria2" || row.Protocol == "tuic" || row.Protocol == "anytls" || row.Protocol == "trojan" {
		passwordUniformity(tag, fileUsers, oldUsers, row, res)
	}
	// Flow uniformity (vless): a uniform file flow becomes the
	// inbound flow via the params update; divergent flows stay
	// per-user passthrough; uniformly absent flow omits flow from
	// generated users instead of letting validation invent vision.
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
		if bu == nil {
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
		// File changed this user: import its identity onto the sub.
		if hasUUID {
			if fuuid := asString(fu["uuid"]); fuuid != "" && fuuid != sub.UUID {
				res.imports = append(res.imports, importOp{kind: "subUUID", subID: sub.ID, uuid: fuuid, changes: []change{{action: changeUpdated, item: "inbounds[" + tag + "].users[" + name + "].uuid", detail: "文件改动，覆盖面板（值已隐藏）"}}})
			}
		}
		if name != sub.Name {
			res.imports = append(res.imports, importOp{kind: "subRename", subID: sub.ID, name: name, changes: []change{{action: changeUpdated, item: "inbounds[" + tag + "].users[" + name + "].name", oldVal: sub.Name, newVal: name, detail: "文件改动，覆盖面板（同 UUID 改名）"}}})
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

// passwordUniformity folds file user passwords into the inbound
// password when uniform, else preserves each divergent password on
// its own user entry.
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
	basePw, baseUniform, _ := uniformPasswordOf(oldUsers)
	if !fileUniform {
		// Divergent: preserve each file password on its own user
		// entry. Merge entries are presence-driven (generated users
		// carry the panel password, so without the override the
		// merged config would lose the file's passwords); the row
		// fires only when the per-user mapping changed since the
		// baseline.
		for _, u := range fileUsers {
			name := asString(u["name"])
			pw, present := u["password"]
			if !present || name == "" {
				continue
			}
			if asString(pw) != panelPw {
				if res.passes.inbound(tag).userSet[name] == nil {
					res.passes.inbound(tag).userSet[name] = map[string]any{}
				}
				res.passes.inbound(tag).userSet[name]["password"] = deepCopyJSON(pw)
			}
		}
		if !sameUserFieldMap(fileUsers, oldUsers, "password") {
			res.changes = append(res.changes, change{action: changePassthrough, item: "inbounds[" + tag + "].users", detail: "各用户密码不一致，逐用户透传保留"})
		}
		return
	}
	// Uniform: import only when it differs from the BASELINE uniform
	// value. A panel-side password edit must not read as a file
	// change; an already-converged panel needs no op.
	if baseUniform && filePw == basePw {
		return
	}
	if filePw == panelPw {
		return
	}
	// Fold into the params update: find the pending params op for
	// this inbound, or create one from panel params.
	for i := range res.imports {
		if res.imports[i].kind == "inboundParams" && res.imports[i].inboundID == row.ID {
			var m map[string]any
			_ = json.Unmarshal([]byte(res.imports[i].params), &m)
			if m == nil {
				m = map[string]any{}
			}
			m["password"] = filePw
			res.imports[i].params = marshalMap(m)
			res.imports[i].changes = append(res.imports[i].changes, change{action: changeUpdated, item: "inbounds[" + tag + "].password", detail: "文件改动，覆盖面板（值已隐藏）"})
			return
		}
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(row.Params), &m)
	if m == nil {
		m = map[string]any{}
	}
	m["password"] = filePw
	res.imports = append(res.imports, importOp{kind: "inboundParams", inboundID: row.ID, params: marshalMap(m), changes: []change{{action: changeUpdated, item: "inbounds[" + tag + "].password", detail: "文件改动，覆盖面板（值已隐藏）"}}})
}

// flowUniformity folds a uniform file flow into the inbound flow
// param; divergent or uniformly-absent flows stay per-user. All
// decisions are BASELINE-driven: a panel-side flow edit must not
// read as a file change, and an unchanged file pattern logs nothing.
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
		// All file users lack flow: omit flow from generated users
		// ONLY when the file REMOVED it since baseline (baseline had
		// flow). File silence (baseline also lacked) leaves the panel
		// flow standing. Never let validation invent vision for a
		// deliberate removal.
		if basePresent {
			if panelFlow != "" {
				for _, u := range fileUsers {
					name := asString(u["name"])
					if name == "" {
						continue
					}
					res.passes.inbound(tag).userDel[name] = append(res.passes.inbound(tag).userDel[name], "flow")
				}
			}
			res.changes = append(res.changes, change{action: changePassthrough, item: "inbounds[" + tag + "].users.flow", oldVal: panelFlow, detail: "文件删除 flow，透传移除（面板不自动补 vision）"})
		}
		return
	}
	if !fileUniform {
		// Divergent: per-user userSet/userDel (merge entries are
		// presence-driven); the row fires only when the per-user
		// mapping changed since baseline.
		for _, u := range fileUsers {
			name := asString(u["name"])
			f, present := u["flow"]
			if !present || name == "" {
				continue
			}
			if asString(f) != panelFlow {
				if res.passes.inbound(tag).userSet[name] == nil {
					res.passes.inbound(tag).userSet[name] = map[string]any{}
				}
				res.passes.inbound(tag).userSet[name]["flow"] = deepCopyJSON(f)
			}
		}
		// Users without flow in a divergent set: omit flow for them.
		for _, u := range fileUsers {
			name := asString(u["name"])
			if _, present := u["flow"]; !present && name != "" && panelFlow != "" {
				res.passes.inbound(tag).userDel[name] = append(res.passes.inbound(tag).userDel[name], "flow")
			}
		}
		if !sameUserFieldMap(fileUsers, oldUsers, "flow") {
			res.changes = append(res.changes, change{action: changePassthrough, item: "inbounds[" + tag + "].users.flow", detail: "各用户 flow 不一致，逐用户透传保留"})
		}
		return
	}
	// Uniform present flow: import only when it differs from the
	// baseline uniform value and from the panel flow.
	baseFlow, baseUniform, _ := uniformFlowOf(oldUsers)
	if baseUniform && fileFlow == baseFlow {
		return
	}
	if fileFlow == panelFlow {
		return
	}
	// Fold into the params update: find the pending params op for
	// this inbound, or create one from panel params.
	for i := range res.imports {
		if res.imports[i].kind == "inboundParams" && res.imports[i].inboundID == row.ID {
			var m map[string]any
			_ = json.Unmarshal([]byte(res.imports[i].params), &m)
			if m == nil {
				m = map[string]any{}
			}
			m["flow"] = fileFlow
			res.imports[i].params = marshalMap(m)
			res.imports[i].changes = append(res.imports[i].changes, change{action: changeUpdated, item: "inbounds[" + tag + "].flow", oldVal: panelFlow, newVal: fileFlow, detail: "文件改动，覆盖面板"})
			return
		}
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(row.Params), &m)
	if m == nil {
		m = map[string]any{}
	}
	m["flow"] = fileFlow
	res.imports = append(res.imports, importOp{kind: "inboundParams", inboundID: row.ID, params: marshalMap(m), changes: []change{{action: changeUpdated, item: "inbounds[" + tag + "].flow", oldVal: panelFlow, newVal: fileFlow, detail: "文件改动，覆盖面板"}}})
}

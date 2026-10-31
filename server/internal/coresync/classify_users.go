package coresync

import (
	"encoding/json"

	"singbox-ui/server/internal/store"
)

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
	// Trojan is excluded: its per-subscription passwords are
	// deliberately non-uniform, so uniformity would falsely
	// flag every trojan sync as a revert.
	if row.Protocol == "hysteria2" || row.Protocol == "tuic" || row.Protocol == "anytls" {
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
			// Legacy trojan single core user (named after
			// the inbound, pre per-subscription passwords)
			// is panel-owned, not a stranger: regen drops
			// it, so it is never passthrough-appended.
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

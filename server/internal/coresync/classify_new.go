package coresync

import (
	"encoding/json"
	"fmt"

	"singbox-ui/server/internal/store"
)

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

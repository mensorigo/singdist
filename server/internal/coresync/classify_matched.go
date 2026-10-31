package coresync

import (
	"encoding/json"
	"fmt"

	"singbox-ui/server/internal/store"
)

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

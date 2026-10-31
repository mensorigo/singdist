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

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
	"fmt"
)

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

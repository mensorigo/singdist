package coresync

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"strings"
)

// mergeConfig overlays the classifier's passthrough specification onto
// a panel-generated config. Generated content is never edited in
// place: the input is deep-copied first. A passthrough whole inbound
// is appended only when no generated inbound shares its tag (a panel
// row owns its tag; a passthrough copy of it would duplicate it).
func mergeConfig(generated map[string]any, passes *passSpec) map[string]any {
	merged, _ := deepCopyJSON(generated).(map[string]any)
	if merged == nil {
		merged = map[string]any{}
	}
	for k, v := range passes.top {
		merged[k] = deepCopyJSON(v)
	}
	if len(passes.logSet) > 0 || len(passes.logDel) > 0 {
		logBlock, _ := asMap(merged["log"])
		if logBlock == nil {
			logBlock = map[string]any{}
			merged["log"] = logBlock
		}
		for k, v := range passes.logSet {
			logBlock[k] = deepCopyJSON(v)
		}
		for _, k := range passes.logDel {
			delete(logBlock, k)
		}
	}
	if len(passes.expSet) > 0 {
		exp, _ := asMap(merged["experimental"])
		if exp == nil {
			exp = map[string]any{}
			merged["experimental"] = exp
		}
		for k, v := range passes.expSet {
			if strings.HasPrefix(k, "clash_api.") {
				clash, _ := asMap(exp["clash_api"])
				if clash == nil {
					clash = map[string]any{}
					exp["clash_api"] = clash
				}
				clash[strings.TrimPrefix(k, "clash_api.")] = deepCopyJSON(v)
			} else {
				exp[k] = deepCopyJSON(v)
			}
		}
	}
	outbounds, _ := asSlice(merged["outbounds"])
	if outbounds == nil {
		outbounds = []any{}
	}
	for i, v := range outbounds {
		if m, ok := asMap(v); ok && asString(m["tag"]) == "direct" {
			for k, val := range passes.directSet {
				m[k] = deepCopyJSON(val)
			}
			outbounds[i] = m
		}
	}
	for _, extra := range passes.outExtra {
		outbounds = append(outbounds, deepCopyJSON(extra))
	}
	merged["outbounds"] = outbounds

	inbounds, _ := asSlice(merged["inbounds"])
	if inbounds == nil {
		inbounds = []any{}
	}
	idxByTag := map[string]int{}
	for i, v := range inbounds {
		if m, ok := asMap(v); ok {
			idxByTag[asString(m["tag"])] = i
		}
	}
	for tag, ip := range passes.inbounds {
		if ip.whole != nil {
			if _, exists := idxByTag[tag]; !exists {
				inbounds = append(inbounds, deepCopyJSON(ip.whole))
				idxByTag[tag] = len(inbounds) - 1
			}
			continue
		}
		idx, exists := idxByTag[tag]
		if !exists {
			continue // row deleted after classification; its passthrough dies with it
		}
		gen, _ := asMap(inbounds[idx])
		if gen == nil {
			continue
		}
		for path, val := range ip.fieldSet {
			setFieldPath(gen, path, deepCopyJSON(val))
		}
		for _, path := range ip.fieldDel {
			delFieldPath(gen, path)
		}
		users := userList(gen)
		hadUsers := gen["users"] != nil
		for name, fields := range ip.userSet {
			for _, u := range users {
				if asString(u["name"]) == name {
					for f, val := range fields {
						u[f] = deepCopyJSON(val)
					}
				}
			}
		}
		for name, fields := range ip.userDel {
			for _, u := range users {
				if asString(u["name"]) == name {
					for _, f := range fields {
						delete(u, f)
					}
				}
			}
		}
		for _, appendU := range ip.userAppend {
			if m, ok := asMap(deepCopyJSON(appendU)); ok {
				users = append(users, m)
			}
		}
		// userList returns []map[string]any; writing it back would change
		// the slice's Go type, and reflect.DeepEqual is type-strict, so a
		// semantically identical doc would compare unequal (every steady
		// sync would rewrite). Convert back to []any; leave the key
		// untouched when the generated inbound had no users and none were
		// appended.
		if hadUsers || len(ip.userAppend) > 0 {
			out := make([]any, len(users))
			for i, u := range users {
				out[i] = u
			}
			gen["users"] = out
		}
		inbounds[idx] = gen
	}
	merged["inbounds"] = inbounds

	return merged
}

func setFieldPath(obj map[string]any, path string, val any) {
	parts := strings.Split(path, ".")
	cur := obj
	for _, p := range parts[:len(parts)-1] {
		next, _ := asMap(cur[p])
		if next == nil {
			next = map[string]any{}
			cur[p] = next
		}
		cur = next
	}
	cur[parts[len(parts)-1]] = val
}

func delFieldPath(obj map[string]any, path string) {
	parts := strings.Split(path, ".")
	cur := obj
	for _, p := range parts[:len(parts)-1] {
		next, ok := asMap(cur[p])
		if !ok {
			return
		}
		cur = next
	}
	delete(cur, parts[len(parts)-1])
}

// marshalDoc renders a config doc exactly like Generate does
// (indented JSON plus a trailing newline) and returns its hash.
func marshalDoc(doc map[string]any) ([]byte, string) {
	b, _ := json.MarshalIndent(doc, "", "  ")
	b = append(b, '\n')
	sum := sha256.Sum256(b)
	return b, hex.EncodeToString(sum[:])
}

// semanticEqual compares two parsed docs structurally: key order and
// whitespace never matter, so a hand-formatted file that matches the
// desired config semantically is in sync (no rewrite, no restart).
func semanticEqual(a, b map[string]any) bool {
	return reflect.DeepEqual(a, b)
}

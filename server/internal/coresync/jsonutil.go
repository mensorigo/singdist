package coresync

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

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
func portOf(v any) int {
	if f, ok := asFloat(v); ok {
		return int(f)
	}
	return 0
}

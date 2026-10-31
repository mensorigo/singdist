package coresync

import (
	"encoding/json"
)

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

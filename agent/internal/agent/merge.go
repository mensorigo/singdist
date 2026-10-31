package agent

import (
	"fmt"
)

// mergeDesired merges pushed + local inbounds. Pushed wins by
// name; a local inbound colliding on port+transport with a pushed
// one is shadowed (not deleted: its file is untouched).
func mergeDesired(pushed, local []Inbound) (desired []Inbound, notes []string) {
	byName := map[string]bool{}
	for _, ib := range pushed {
		byName[ib.Name] = true
		desired = append(desired, ib)
	}
	for _, ib := range local {
		if byName[ib.Name] {
			notes = append(notes, fmt.Sprintf("local inbound %q shadowed by pushed inbound of the same name", ib.Name))
			continue
		}
		conflict := ""
		for _, p := range pushed {
			if p.Port == ib.Port && protocolTransports[p.Protocol]&protocolTransports[ib.Protocol] != 0 {
				conflict = p.Name
				break
			}
		}
		if conflict != "" {
			notes = append(notes, fmt.Sprintf("local inbound %q shadowed: port %d conflicts with pushed inbound %q", ib.Name, ib.Port, conflict))
			continue
		}
		byName[ib.Name] = true
		desired = append(desired, ib)
	}
	return desired, notes
}

// mergeWithTarget folds target-file passthrough into the desired
// doc: unmanaged top-level keys (anything the agent does not
// generate: not log/experimental/inbounds/outbounds) and target
// inbounds that are neither desired nor in the last applied doc
// (i.e. added outside the agent) are preserved. A target inbound
// that was in the last applied doc but is no longer desired was
// deleted by the central panel and is dropped, not preserved.
// mergeWithTarget folds target-file passthrough into the desired
// doc: unmanaged top-level keys (anything the agent does not
// generate: not log/experimental/inbounds/outbounds) and target
// inbounds that are neither desired nor in the last applied doc
// (i.e. added outside the agent) are preserved. A target inbound
// that was in the last applied doc but is no longer desired was
// deleted by the central panel and is dropped, not preserved.
func (a *Agent) mergeWithTarget(desiredDoc []byte, desiredHash string, targetBytes []byte, targetMissing bool, notes []string) ([]byte, string, []string) {
	if targetMissing {
		return desiredDoc, desiredHash, append(notes, "target file missing, recreated from desired config")
	}
	fileParsed, perr := parseDoc(targetBytes)
	if perr != nil {
		return desiredDoc, desiredHash, append(notes, "target file was not valid JSON, replaced with desired config")
	}
	desiredParsed, _ := parseDoc(desiredDoc)
	lastParsed, _ := parseDoc([]byte(a.state.LastDoc))
	lastTags := docInboundTags(lastParsed)
	desiredNames := map[string]bool{}
	for _, ib := range docInbounds(desiredParsed) {
		if tag, ok := ib["tag"].(string); ok {
			desiredNames[tag] = true
		}
	}
	merged := desiredParsed
	for k, v := range fileParsed {
		switch k {
		case "log", "experimental", "inbounds", "outbounds":
			// Agent-managed keys: desired wins.
		default:
			merged[k] = v
		}
	}
	mergedInbounds := docInbounds(desiredParsed)
	for _, ib := range docInbounds(fileParsed) {
		tag, _ := ib["tag"].(string)
		if tag == "" || desiredNames[tag] || lastTags[tag] {
			continue
		}
		mergedInbounds = append(mergedInbounds, ib)
		notes = append(notes, fmt.Sprintf("target-file inbound %q preserved as passthrough", tag))
	}
	merged["inbounds"] = mergedInbounds
	doc, hash := marshalDoc(merged)
	return doc, hash, notes
}

// Status is the agent's live report for the central panel.

package agent

import (
	"fmt"
	"os"
)

// CoreInbounds reads the node's CURRENT core config file and returns
// its inbounds verbatim (raw sing-box JSON objects, secrets included).
// This is the import source for the central panel: the file holds
// everything the core currently runs — panel-managed inbounds plus
// hand-added passthrough inbounds — while the last applied doc holds
// only the managed subset. The panel filters (supported protocols,
// already-managed names) and maps these objects to panel inbounds;
// the agent deliberately does no mapping itself, so no field is lost
// in translation and future panel protocols need no agent change.
//
// The endpoint requires the node token like every other management
// endpoint, and travels the same TLS channel as pushes (which carry
// secrets in the other direction). A missing or unparseable target
// file is an error, not an empty list: silently reporting "nothing
// to import" for a broken node would hide the real problem.
func (a *Agent) CoreInbounds() ([]map[string]any, error) {
	b, err := os.ReadFile(a.cfg.Target)
	if err != nil {
		return nil, fmt.Errorf("read core config: %w", err)
	}
	parsed, err := parseDoc(b)
	if err != nil {
		return nil, fmt.Errorf("parse core config: %w", err)
	}
	return docInbounds(parsed), nil
}

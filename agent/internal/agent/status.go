package agent

import (
	"context"
	"os"
)

// Status is the agent's live report for the central panel.
type Status struct {
	UnitActive  string           `json:"unitActive"` // active|failed|inactive|activating|unknown|"" (no unit)
	ClashAlive  bool             `json:"clashAlive"`
	Version     string           `json:"version,omitempty"`
	Hostname    string           `json:"hostname,omitempty"` // os.Hostname(); "" when unavailable
	LastHash    string           `json:"lastHash"`
	LastSyncAt  int64            `json:"lastSyncAt"`
	LastResult  string           `json:"lastResult"`
	BlockedHash string           `json:"blockedHash,omitempty"`
	Inbounds    []InboundState   `json:"inbounds"`
	System      SystemStats      `json:"system,omitempty"`
	Traffic     *TrafficSnapshot `json:"traffic,omitempty"`
}

func (a *Agent) Status(ctx context.Context) Status {
	hostname, _ := os.Hostname() // "" on error; the panel falls back to the address host
	st := Status{
		Version:     Version,
		Hostname:    hostname,
		LastHash:    a.state.LastHash,
		LastSyncAt:  a.state.LastSyncAt,
		LastResult:  a.state.LastResult,
		BlockedHash: a.state.BlockedHash,
		Inbounds:    a.statusInbounds(),
	}
	if a.engine.UnitProbe != nil {
		st.UnitActive = a.engine.UnitProbe.State(ctx)
	}
	if a.engine.Clash != nil {
		st.ClashAlive = a.engine.Clash.Alive(ctx)
	}
	st.System = readSystemStats()
	if a.traffic != nil {
		snap := a.traffic.Snapshot()
		st.Traffic = &snap
	}
	return st
}

// statusInbounds derives inbound states from the last applied doc:
// managed inbounds from the doc itself, listening from /proc, and
// passthrough inbounds (in the file but not in the last doc) marked
// as such so the central panel can tell hand-added inbounds apart.
// statusInbounds derives inbound states from the last applied doc:
// managed inbounds from the doc itself, listening from /proc, and
// passthrough inbounds (in the file but not in the last doc) marked
// as such so the central panel can tell hand-added inbounds apart.
func (a *Agent) statusInbounds() []InboundState {
	out := []InboundState{}
	if a.state.LastDoc == "" {
		return out
	}
	lastParsed, err := parseDoc([]byte(a.state.LastDoc))
	if err != nil {
		return out
	}
	listening := ListeningPorts()
	for _, ib := range docInbounds(lastParsed) {
		name, _ := ib["tag"].(string)
		typ, _ := ib["type"].(string)
		port := 0
		if f, ok := ib["listen_port"].(float64); ok {
			port = int(f)
		}
		var l *bool
		if port > 0 {
			var v bool
			if typ == "hysteria2" || typ == "tuic" {
				v = listening.UDP[port]
			} else {
				v = listening.TCP[port]
			}
			l = &v
		}
		out = append(out, InboundState{Name: name, Protocol: singboxTypeToProtocol(typ), Port: port, Source: "managed", Listening: l})
	}
	return out
}

// singboxTypeToProtocol maps a core inbound type back to the panel
// protocol name. vless here is always vless-reality: the agent
// only generates Reality TLS for vless inbounds.
// singboxTypeToProtocol maps a core inbound type back to the panel
// protocol name. vless here is always vless-reality: the agent
// only generates Reality TLS for vless inbounds.
func singboxTypeToProtocol(typ string) string {
	if typ == "vless" {
		return ProtocolVLESSReality
	}
	return typ
}

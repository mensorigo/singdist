package agent

import (
	"fmt"
	"os"
)

// coreTypeTransports maps a sing-box inbound "type" (as it appears in
// the core config file) to the transports it listens on. Unknown
// types are treated as both: the port is in use by something in the
// config even if we cannot name its transport, so a conservative
// answer is the honest one.
var coreTypeTransports = map[string]transport{
	"vless":       transportTCP,
	"hysteria2":   transportUDP,
	"tuic":        transportUDP,
	"shadowsocks": transportTCP | transportUDP,
	"anytls":      transportTCP,
	"trojan":      transportTCP,
}

// PortCheck is the answer to "is this port free on this machine, for
// this transport?" Reason names the blocker and is empty when free.
// Kind is "" when free, else one of reserved | managed | listening;
// OccupiedBy is the managed inbound's tag when Kind is managed, so a
// caller can tell "already running this same inbound" (not a
// conflict) apart from "taken by something else" (a conflict).
// The check is a read-only observation — it opens no connections
// and never changes state — and the push-time bind remains the
// final authority: a "free" answer is not a reservation.
type PortCheck struct {
	Port       int    `json:"port"`
	Transport  string `json:"transport"` // tcp | udp | both
	Occupied   bool   `json:"occupied"`
	Reason     string `json:"reason,omitempty"`
	Kind       string `json:"kind,omitempty"`
	OccupiedBy string `json:"occupiedBy,omitempty"`
}

// ParseCheckTransport parses the ?transport= query value. "" means
// both (conservative: occupied if either transport is taken).
func ParseCheckTransport(s string) (transport, string, bool) {
	switch s {
	case "", "both", "tcp+udp", "tcp,udp":
		return transportTCP | transportUDP, "both", true
	case "tcp":
		return transportTCP, "tcp", true
	case "udp":
		return transportUDP, "udp", true
	default:
		return 0, "", false
	}
}

// CheckPort reports whether port is usable on this machine for the
// given transport. Sources, in order: reserved ports (policy, any
// transport — matching push validation, which rejects a reserved
// port regardless of transport), managed inbounds in the current
// core config (named), then live listeners from /proc. A missing
// or unparseable core config simply skips the managed source; the
// listener check still applies.
func (a *Agent) CheckPort(port int, tr transport, label string) PortCheck {
	out := PortCheck{Port: port, Transport: label}
	if what, ok := a.cfg.reservedPorts()[port]; ok {
		out.Occupied = true
		out.Reason = fmt.Sprintf("保留端口（%s）", what)
		out.Kind = "reserved"
		return out
	}
	if doc, err := os.ReadFile(a.cfg.Target); err == nil {
		if parsed, err := parseDoc(doc); err == nil {
			for _, ib := range docInbounds(parsed) {
				p := 0
				if f, ok := ib["listen_port"].(float64); ok {
					p = int(f)
				}
				if p != port {
					continue
				}
				typ, _ := ib["type"].(string)
				ibTr, known := coreTypeTransports[typ]
				if !known {
					ibTr = transportTCP | transportUDP
				}
				if ibTr&tr != 0 {
					tag, _ := ib["tag"].(string)
					if tag == "" {
						tag = typ
					}
					out.Occupied = true
					out.Reason = fmt.Sprintf("已被入站 %q 占用", tag)
					out.Kind = "managed"
					out.OccupiedBy = tag
					return out
				}
			}
		}
	}
	listening := ListeningPorts()
	if tr&transportTCP != 0 && listening.TCP[port] {
		out.Occupied = true
		out.Reason = "已有程序在监听（TCP）"
		out.Kind = "listening"
		return out
	}
	if tr&transportUDP != 0 && listening.UDP[port] {
		out.Occupied = true
		out.Reason = "已有程序在监听（UDP）"
		out.Kind = "listening"
		return out
	}
	return out
}

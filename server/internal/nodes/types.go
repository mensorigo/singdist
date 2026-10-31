package nodes

import (
	"encoding/json"
)

type PushUser struct {
	Name string `json:"name"`
	UUID string `json:"uuid,omitempty"`
}

// PushInbound is one inbound definition pushed to an agent.
// PushInbound is one inbound definition pushed to an agent.
type PushInbound struct {
	Name     string          `json:"name"`
	Protocol string          `json:"protocol"`
	Port     int             `json:"port"`
	Enabled  bool            `json:"enabled"`
	Params   json.RawMessage `json:"params"`
	Users    []PushUser      `json:"users,omitempty"`
}

// PushCert is the PEM certificate embedded for TLS inbounds.
// PushCert is the PEM certificate embedded for TLS inbounds.
type PushCert struct {
	Certificate string `json:"certificate"`
	Key         string `json:"key"`
}

// PushPayload is the POST /api/agent/sync body.
// PushPayload is the POST /api/agent/sync body.
type PushPayload struct {
	Inbounds   []PushInbound `json:"inbounds"`
	PublicHost string        `json:"publicHost,omitempty"`
	Cert       *PushCert     `json:"cert,omitempty"`
}

// PushResult is the agent's sync response.
// PushResult is the agent's sync response.
type PushResult struct {
	Result string   `json:"result"`
	Hash   string   `json:"hash"`
	Detail string   `json:"detail"`
	Notes  []string `json:"notes,omitempty"`
}

// StatusInbound is one inbound in the agent's status report.
// StatusInbound is one inbound in the agent's status report.
type StatusInbound struct {
	Name      string `json:"name"`
	Protocol  string `json:"protocol"`
	Port      int    `json:"port"`
	Source    string `json:"source"`
	Listening *bool  `json:"listening,omitempty"`
}

// AgentStatus is the GET /api/agent/status response.
// AgentStatus is the GET /api/agent/status response.
type AgentSystem struct {
	CpuPct    float64 `json:"cpuPct"`
	Load1     float64 `json:"load1"`
	Load5     float64 `json:"load5"`
	Load15    float64 `json:"load15"`
	MemTotal  int64   `json:"memTotal"`
	MemUsed   int64   `json:"memUsed"`
	MemPct    float64 `json:"memPct"`
	DiskTotal int64   `json:"diskTotal"`
	DiskUsed  int64   `json:"diskUsed"`
	DiskPct   float64 `json:"diskPct"`
	Available bool    `json:"available"`
}
type AgentTrafficEntry struct {
	Inbound string `json:"inbound"`
	User    string `json:"user,omitempty"`
	Up      int64  `json:"up"`
	Down    int64  `json:"down"`
}
type AgentTraffic struct {
	Up                  int64               `json:"up"`
	Down                int64               `json:"down"`
	UnassignedUp        int64               `json:"unassignedUp"`
	UnassignedDown      int64               `json:"unassignedDown"`
	ByInbound           []AgentTrafficEntry `json:"byInbound"`
	ByUser              []AgentTrafficEntry `json:"byUser"`
	UnassignedByInbound []AgentTrafficEntry `json:"unassignedByInbound,omitempty"`
	UpdatedAt           int64               `json:"updatedAt"`
}
type AgentStatus struct {
	UnitActive  string          `json:"unitActive"`
	ClashAlive  bool            `json:"clashAlive"`
	Version     string          `json:"version,omitempty"`
	Hostname    string          `json:"hostname,omitempty"` // absent on agents older than 0.1.4
	LastHash    string          `json:"lastHash"`
	LastSyncAt  int64           `json:"lastSyncAt"`
	LastResult  string          `json:"lastResult"`
	BlockedHash string          `json:"blockedHash,omitempty"`
	Inbounds    []StatusInbound `json:"inbounds"`
	System      AgentSystem     `json:"system,omitempty"`
	Traffic     *AgentTraffic   `json:"traffic,omitempty"`
}

// --- address parsing --------------------------------------------------------

// ParseAddress splits a node address into scheme and host:port. A bare
// host:port means https (the agent serves TLS by default); an explicit
// http:// prefix selects plaintext (tests and dev agents with TLS off).
// AgentPortCheck mirrors the agent's check-port answer. Kind is ""
// when free, else reserved | managed | listening; OccupiedBy is the
// managed inbound's tag when Kind is managed.
type AgentPortCheck struct {
	Port       int    `json:"port"`
	Transport  string `json:"transport"`
	Occupied   bool   `json:"occupied"`
	Reason     string `json:"reason"`
	Kind       string `json:"kind"`
	OccupiedBy string `json:"occupiedBy"`
}

// CheckPort asks the agent whether a port is free on its machine for
// a transport (tcp | udp | both). A 404 maps to
// ErrCheckPortUnsupported (the agent predates the endpoint).

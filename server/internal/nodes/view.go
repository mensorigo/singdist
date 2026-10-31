package nodes

import (
	"encoding/json"

	"singbox-ui/server/internal/store"
)

// NodeView is the API shape for a node. The token is never returned:
// the edit form sends an empty token to mean "keep the existing one".
type NodeView struct {
	ID               int64           `json:"id"`
	Name             string          `json:"name"`
	Address          string          `json:"address"`
	PublicHost       string          `json:"publicHost"`
	TLSMode          string          `json:"tlsMode"`
	UsageRatio       float64         `json:"usageRatio"`
	Paused           bool            `json:"paused"`
	PausePending     bool            `json:"pausePending"` // paused but the empty push has not landed on the node yet
	SyncStatus       string          `json:"syncStatus"`   // synced | pending | mismatch | offline | paused | unknown
	SyncDetail       string          `json:"syncDetail"`   // human-readable reason when not synced, "" when synced
	AgentUnitActive  string          `json:"agentUnitActive"`
	AgentClashAlive  bool            `json:"agentClashAlive"`
	AgentStatusAt    int64           `json:"agentStatusAt"`
	AgentInbounds    []StatusInbound `json:"agentInbounds"`
	AgentSystem      AgentSystem     `json:"agentSystem,omitempty"`
	AgentTraffic     *AgentTraffic   `json:"agentTraffic,omitempty"`
	AgentLastResult  string          `json:"agentLastResult"`
	AgentBlockedHash string          `json:"agentBlockedHash"`
	Online           bool            `json:"online"`
	LastPushAt       int64           `json:"lastPushAt"`
	LastPushResult   string          `json:"lastPushResult"`
	LastError        string          `json:"lastError"`
	LastOnlineAt     int64           `json:"lastOnlineAt"`
	LastInboundCount int             `json:"lastInboundCount"`
	LastVersion      string          `json:"lastVersion"`
	Warning          string          `json:"warning,omitempty"` // creation-time probe warning, if any
	CreatedAt        int64           `json:"createdAt"`
	UpdatedAt        int64           `json:"updatedAt"`
}

// protocolTransport mirrors the agent's coreTypeTransports and the
// panel's protocolSpecs: the transports an inbound listens on.
// Unknown protocols map to both (conservative).
var protocolTransport = map[string]string{
	"vless-reality": "tcp",
	"hysteria2":     "udp",
	"tuic":          "udp",
	"anytls":        "tcp",
	"trojan":        "tcp",
}

func toView(r store.NodeRow, now int64) NodeView {
	pausePending := false
	if r.Paused {
		if _, pausedHash, err := BuildPausedPush(r); err == nil {
			pausePending = r.LastPushHash != pausedHash
		}
	}
	return NodeView{
		ID:               r.ID,
		Name:             r.Name,
		Address:          r.Address,
		PublicHost:       r.PublicHost,
		TLSMode:          r.TLSMode,
		UsageRatio:       r.UsageRatio,
		Paused:           r.Paused,
		PausePending:     pausePending,
		SyncStatus:       "unknown",
		AgentInbounds:    []StatusInbound{},
		Online:           r.Online(now),
		LastPushAt:       r.LastPushAt,
		LastPushResult:   r.LastPushResult,
		LastError:        r.LastError,
		LastOnlineAt:     r.LastOnlineAt,
		LastInboundCount: r.LastInboundCount,
		LastVersion:      r.LastVersion,
		CreatedAt:        r.CreatedAt,
		UpdatedAt:        r.UpdatedAt,
	}
}

// enrichSync fills a NodeView's agent snapshot fields and its sync
// status from the stored snapshot, the stored desired/pushed hashes,
// and the node's panel assignments. "Synced" is strict on purpose:
// online, no push error, desired hash == pushed hash, no agent block,
// the agent's own last result applied/no_change, and every enabled
// panel-assigned inbound present on the agent and listening. Anything
// else is pending (a push is still due), mismatch (something
// disagrees), offline, paused, or unknown (no snapshot yet). Extra
// agent inbounds the panel never assigned are not a mismatch: they
// are the node's own local inbounds, shown on the card as-is.
// Clash and core-unit state are shown as info only: they do not
// decide sync, because inbounds can serve while Clash is down.
// enrichSync fills a NodeView's agent snapshot fields and its sync
// status from the stored snapshot, the stored desired/pushed hashes,
// and the node's panel assignments. "Synced" is strict on purpose:
// online, no push error, desired hash == pushed hash, no agent block,
// the agent's own last result applied/no_change, and every enabled
// panel-assigned inbound present on the agent and listening. Anything
// else is pending (a push is still due), mismatch (something
// disagrees), offline, paused, or unknown (no snapshot yet). Extra
// agent inbounds the panel never assigned are not a mismatch: they
// are the node's own local inbounds, shown on the card as-is.
// Clash and core-unit state are shown as info only: they do not
// decide sync, because inbounds can serve while Clash is down.
func enrichSyncView(v NodeView, r store.NodeRow, now int64, assigned []store.InboundNodeRow, inbounds []store.InboundRow) NodeView {
	if r.AgentStatus != "" {
		var snap AgentStatus
		if err := json.Unmarshal([]byte(r.AgentStatus), &snap); err == nil {
			v.AgentUnitActive = snap.UnitActive
			v.AgentClashAlive = snap.ClashAlive
			v.AgentStatusAt = r.AgentStatusAt
			v.AgentLastResult = snap.LastResult
			v.AgentBlockedHash = snap.BlockedHash
			v.AgentSystem = snap.System
			v.AgentTraffic = snap.Traffic
			v.AgentInbounds = snap.Inbounds
			if v.AgentInbounds == nil {
				v.AgentInbounds = []StatusInbound{}
			}
		}
	}
	if r.Paused {
		v.SyncStatus = "paused"
		return v
	}
	if !r.Online(now) {
		v.SyncStatus = "offline"
		v.SyncDetail = "节点离线"
		return v
	}
	if r.AgentStatus == "" || r.AgentStatusAt == 0 || now-r.AgentStatusAt > store.OnlineWithin {
		v.SyncStatus = "unknown"
		v.SyncDetail = "暂无节点状态快照"
		return v
	}
	if r.LastError != "" {
		v.SyncStatus = "mismatch"
		v.SyncDetail = "推送失败：" + r.LastError
		return v
	}
	switch r.LastPushResult {
	case "applied", "no_change", "":
	default:
		v.SyncStatus = "mismatch"
		v.SyncDetail = "上次推送结果：" + r.LastPushResult
		return v
	}
	if r.DesiredPushHash == "" {
		v.SyncStatus = "unknown"
		v.SyncDetail = "推送状态还没算出来"
		return v
	}
	if r.DesiredPushHash != r.LastPushHash {
		v.SyncStatus = "pending"
		v.SyncDetail = "推送还没落到节点"
		return v
	}
	var snap AgentStatus
	if err := json.Unmarshal([]byte(r.AgentStatus), &snap); err != nil {
		v.SyncStatus = "unknown"
		v.SyncDetail = "节点状态快照损坏"
		return v
	}
	if snap.BlockedHash != "" {
		v.SyncStatus = "mismatch"
		v.SyncDetail = "节点被阻塞，需清除阻塞并重推"
		return v
	}
	switch snap.LastResult {
	case "applied", "no_change":
	default:
		v.SyncStatus = "mismatch"
		v.SyncDetail = "节点上次同步结果：" + snap.LastResult
		return v
	}
	byID := map[int64]store.InboundRow{}
	for _, ib := range inbounds {
		byID[ib.ID] = ib
	}
	byName := map[string]StatusInbound{}
	for _, ai := range snap.Inbounds {
		byName[ai.Name] = ai
	}
	for _, a := range assigned {
		ib, ok := byID[a.InboundID]
		if !ok || !ib.Enabled {
			continue
		}
		port := ib.Port
		if a.Port > 0 {
			port = a.Port
		}
		ai, ok := byName[ib.Name]
		if !ok {
			v.SyncStatus = "mismatch"
			v.SyncDetail = "节点上缺入站：" + ib.Name
			return v
		}
		if ai.Port != port {
			v.SyncStatus = "mismatch"
			v.SyncDetail = "节点入站端口不对：" + ib.Name
			return v
		}
		if ai.Listening == nil || !*ai.Listening {
			v.SyncStatus = "mismatch"
			v.SyncDetail = "节点入站未在监听：" + ib.Name
			return v
		}
	}
	v.SyncStatus = "synced"
	v.SyncDetail = ""
	return v
}

package inbounds

import (
	"encoding/json"

	"singbox-ui/server/internal/store"
)

// nodeAgentSnapshot is the part of a node's stored agent-status
// snapshot that decides one inbound's sync state. It mirrors the
// nodes package's AgentStatus shape without importing it.
type nodeAgentSnapshot struct {
	LastResult  string `json:"lastResult"`
	BlockedHash string `json:"blockedHash"`
	Inbounds    []struct {
		Name      string `json:"name"`
		Port      int    `json:"port"`
		Listening *bool  `json:"listening"`
	} `json:"inbounds"`
}

// nodeInboundStatus decides one node-only inbound's status on one
// node, with the same criteria as the node card's synced check,
// scoped to this single inbound: push succeeded, the pushed content
// matches what the panel wants now, the agent reports this inbound,
// and it is listening. Paused and offline are their own statuses,
// not sync failures, so a paused node's inbounds never read as
// "synced" and never read as "out of sync" either.
// nodeInboundStatus decides one node-only inbound's status on one
// node, with the same criteria as the node card's synced check,
// scoped to this single inbound: push succeeded, the pushed content
// matches what the panel wants now, the agent reports this inbound,
// and it is listening. Paused and offline are their own statuses,
// not sync failures, so a paused node's inbounds never read as
// "synced" and never read as "out of sync" either.
func nodeInboundStatus(row store.InboundRow, asg store.InboundNodeRow, n store.NodeRow, now int64) string {
	if n.Paused {
		return "paused"
	}
	if !n.Online(now) {
		return "offline"
	}
	if n.AgentStatus == "" || n.AgentStatusAt == 0 || now-n.AgentStatusAt > store.OnlineWithin {
		return "unknown"
	}
	if n.LastError != "" {
		return "pending"
	}
	if n.LastPushResult != "" && n.LastPushResult != "applied" && n.LastPushResult != "no_change" {
		return "pending"
	}
	if n.DesiredPushHash == "" {
		return "unknown"
	}
	if n.DesiredPushHash != n.LastPushHash {
		return "pending"
	}
	var snap nodeAgentSnapshot
	if err := json.Unmarshal([]byte(n.AgentStatus), &snap); err != nil {
		return "unknown"
	}
	if snap.BlockedHash != "" {
		return "pending"
	}
	if snap.LastResult != "" && snap.LastResult != "applied" && snap.LastResult != "no_change" {
		return "pending"
	}
	wantPort := asg.Port
	if wantPort == 0 {
		wantPort = row.Port
	}
	for _, ib := range snap.Inbounds {
		if ib.Name != row.Name {
			continue
		}
		if ib.Port != wantPort {
			return "drifted"
		}
		if ib.Listening == nil || !*ib.Listening {
			return "not_listening"
		}
		return "synced"
	}
	return "pending"
}

// assigned nodes (normally exactly one under the single-select
// non-synced status wins, so a multi-node inbound is never reported
// synced while any node is behind.
// assigned nodes (normally exactly one under the single-select
// non-synced status wins, so a multi-node inbound is never reported
// synced while any node is behind.
func nodeInboundStatusAll(row store.InboundRow, asgs []store.InboundNodeRow, nodes map[int64]store.NodeRow, now int64) string {
	if len(asgs) == 0 {
		// Node-only but assigned nowhere: it runs on no machine
		// and is pushed nowhere, so it is not in sync anywhere.
		return "pending"
	}
	severity := map[string]int{
		"pending": 0, "drifted": 1, "not_listening": 2,
		"paused": 3, "offline": 4, "unknown": 5, "synced": 6,
	}
	best := "synced"
	for _, asg := range asgs {
		n, ok := nodes[asg.NodeID]
		if !ok {
			if severity["unknown"] < severity[best] {
				best = "unknown"
			}
			continue
		}
		st := nodeInboundStatus(row, asg, n, now)
		if severity[st] < severity[best] {
			best = st
		}
	}
	return best
}

// List returns every stored inbound with its comparison status, plus any
// running inbounds the panel does not manage yet.

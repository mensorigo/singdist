package inbounds

import (
	"encoding/json"
	"testing"
	"time"

	"singbox-ui/server/internal/store"
)

func nodeRowForTest(t *testing.T, paused bool, onlineAt, statusAt int64, desired, last, lastResult, lastError, agentLastResult, blocked string, agentInbounds []struct {
	Name      string `json:"name"`
	Port      int    `json:"port"`
	Listening *bool  `json:"listening"`
}) store.NodeRow {
	t.Helper()
	snap, err := json.Marshal(struct {
		LastResult  string      `json:"lastResult"`
		BlockedHash string      `json:"blockedHash"`
		Inbounds    interface{} `json:"inbounds"`
	}{agentLastResult, blocked, agentInbounds})
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	row := store.NodeRow{
		ID: 1, Name: "n1", Paused: paused, LastPushHash: last,
		LastPushResult: lastResult, LastError: lastError,
		LastOnlineAt: onlineAt, DesiredPushHash: desired,
		AgentStatus: string(snap), AgentStatusAt: statusAt,
	}
	if agentInbounds == nil {
		row.AgentStatus = ""
		row.AgentStatusAt = 0
	}
	return row
}

func TestNodeInboundStatus(t *testing.T) {
	now := time.Now().Unix()
	listening := true
	notListening := false
	row := store.InboundRow{ID: 10, Name: "v1", Protocol: "vless-reality", Port: 443, Enabled: true}
	asg := store.InboundNodeRow{InboundID: 10, NodeID: 1, Port: 0}
	nodes := func(n store.NodeRow) map[int64]store.NodeRow { return map[int64]store.NodeRow{1: n} }

	listeningIn := []struct {
		Name      string `json:"name"`
		Port      int    `json:"port"`
		Listening *bool  `json:"listening"`
	}{{Name: "v1", Port: 443, Listening: &listening}}
	notListeningIn := []struct {
		Name      string `json:"name"`
		Port      int    `json:"port"`
		Listening *bool  `json:"listening"`
	}{{Name: "v1", Port: 443, Listening: &notListening}}

	cases := []struct {
		name string
		node store.NodeRow
		want string
	}{
		{"synced", nodeRowForTest(t, false, now, now, "h1", "h1", "applied", "", "applied", "", listeningIn), "synced"},
		{"not listening", nodeRowForTest(t, false, now, now, "h1", "h1", "applied", "", "applied", "", notListeningIn), "not_listening"},
		{"pending desired differs", nodeRowForTest(t, false, now, now, "h2", "h1", "applied", "", "applied", "", listeningIn), "pending"},
		{"blocked", nodeRowForTest(t, false, now, now, "h1", "h1", "applied", "", "applied", "bhash", listeningIn), "pending"},
		{"push error", nodeRowForTest(t, false, now, now, "h1", "h1", "error", "boom", "applied", "", listeningIn), "pending"},
		{"paused", nodeRowForTest(t, true, now, now, "h1", "h1", "applied", "", "applied", "", listeningIn), "paused"},
		{"offline", nodeRowForTest(t, false, now-1000, now-1000, "h1", "h1", "applied", "", "applied", "", listeningIn), "offline"},
		{"no snapshot", nodeRowForTest(t, false, now, now, "h1", "h1", "applied", "", "applied", "", nil), "unknown"},
		{"missing on agent", nodeRowForTest(t, false, now, now, "h1", "h1", "applied", "", "applied", "", []struct {
			Name      string `json:"name"`
			Port      int    `json:"port"`
			Listening *bool  `json:"listening"`
		}{{Name: "other", Port: 444, Listening: &listening}}), "pending"},
		{"port differs", nodeRowForTest(t, false, now, now, "h1", "h1", "applied", "", "applied", "", []struct {
			Name      string `json:"name"`
			Port      int    `json:"port"`
			Listening *bool  `json:"listening"`
		}{{Name: "v1", Port: 444, Listening: &listening}}), "drifted"},
	}
	for _, tc := range cases {
		if got := nodeInboundStatusAll(row, []store.InboundNodeRow{asg}, nodes(tc.node), now); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
	if got := nodeInboundStatusAll(row, nil, nodes(nodeRowForTest(t, false, now, now, "h1", "h1", "applied", "", "applied", "", listeningIn)), now); got != "pending" {
		t.Errorf("no assignment: got %q, want pending", got)
	}
}

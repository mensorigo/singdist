package nodes

import (
	"encoding/json"
	"testing"
	"time"

	"singbox-ui/server/internal/store"
)

func syncTestRow(t *testing.T, paused bool, onlineAt, statusAt int64, desired, last, lastResult, lastError, agentLastResult, blocked string, agentInbounds []StatusInbound) store.NodeRow {
	t.Helper()
	snap, err := json.Marshal(AgentStatus{
		UnitActive: "active", ClashAlive: true, Version: "0.2.0",
		LastResult: agentLastResult, BlockedHash: blocked, Inbounds: agentInbounds,
	})
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	row := store.NodeRow{
		ID: 1, Name: "n1", Address: "127.0.0.1:32435", Token: "tok",
		Paused: paused, LastPushHash: last, LastPushResult: lastResult,
		LastError: lastError, LastOnlineAt: onlineAt, LastInboundCount: len(agentInbounds),
		LastVersion: "0.2.0", DesiredPushHash: desired,
		AgentStatus: string(snap), AgentStatusAt: statusAt,
	}
	if agentInbounds == nil {
		row.AgentStatus = ""
		row.AgentStatusAt = 0
	}
	return row
}

func TestEnrichSyncView(t *testing.T) {
	now := time.Now().Unix()
	listening := true
	notListening := false
	agentIn := []StatusInbound{
		{Name: "v1", Protocol: "vless-reality", Port: 443, Listening: &listening},
	}
	inbounds := []store.InboundRow{
		{ID: 10, Name: "v1", Protocol: "vless-reality", Port: 443, Enabled: true},
	}
	assigned := []store.InboundNodeRow{{InboundID: 10, NodeID: 1, Port: 0}}

	synced := syncTestRow(t, false, now, now, "h1", "h1", "applied", "", "applied", "", agentIn)
	if v := enrichSyncView(toView(synced, now), synced, now, assigned, inbounds); v.SyncStatus != "synced" || v.SyncDetail != "" {
		t.Fatalf("synced: got %q/%q, want synced/empty", v.SyncStatus, v.SyncDetail)
	} else if len(v.AgentInbounds) != 1 || v.AgentInbounds[0].Listening == nil || !*v.AgentInbounds[0].Listening {
		t.Fatalf("agent inbounds in view: %+v, want 1 listening", v.AgentInbounds)
	}

	notListeningIn := []StatusInbound{
		{Name: "v1", Protocol: "vless-reality", Port: 443, Listening: &notListening},
	}
	row := syncTestRow(t, false, now, now, "h1", "h1", "applied", "", "applied", "", notListeningIn)
	if v := enrichSyncView(toView(row, now), row, now, assigned, inbounds); v.SyncStatus != "mismatch" {
		t.Fatalf("not listening: got %q, want mismatch (%q)", v.SyncStatus, v.SyncDetail)
	}

	row = syncTestRow(t, false, now, now, "h2", "h1", "applied", "", "applied", "", agentIn)
	if v := enrichSyncView(toView(row, now), row, now, assigned, inbounds); v.SyncStatus != "pending" {
		t.Fatalf("desired != pushed: got %q, want pending", v.SyncStatus)
	}

	row = syncTestRow(t, false, now, now, "h1", "h1", "applied", "", "applied", "blockedhash", agentIn)
	if v := enrichSyncView(toView(row, now), row, now, assigned, inbounds); v.SyncStatus != "mismatch" {
		t.Fatalf("blocked: got %q, want mismatch", v.SyncStatus)
	}

	row = syncTestRow(t, false, now, now, "h1", "h1", "error", "boom", "applied", "", agentIn)
	if v := enrichSyncView(toView(row, now), row, now, assigned, inbounds); v.SyncStatus != "mismatch" {
		t.Fatalf("push error: got %q, want mismatch", v.SyncStatus)
	}

	row = syncTestRow(t, false, now-1000, now-1000, "h1", "h1", "applied", "", "applied", "", agentIn)
	if v := enrichSyncView(toView(row, now), row, now, assigned, inbounds); v.SyncStatus != "offline" {
		t.Fatalf("offline: got %q, want offline", v.SyncStatus)
	}

	row = syncTestRow(t, true, now, now, "h1", "h1", "applied", "", "applied", "", agentIn)
	if v := enrichSyncView(toView(row, now), row, now, assigned, inbounds); v.SyncStatus != "paused" {
		t.Fatalf("paused: got %q, want paused", v.SyncStatus)
	}

	row = syncTestRow(t, false, now, now, "h1", "h1", "applied", "", "applied", "", nil)
	if v := enrichSyncView(toView(row, now), row, now, assigned, inbounds); v.SyncStatus != "unknown" {
		t.Fatalf("no snapshot: got %q, want unknown", v.SyncStatus)
	}

	// Paused push applied: sync is still reported as paused, never
	// as synced, on the card (covered by the paused case above).
}

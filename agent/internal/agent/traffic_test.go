package agent

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestTrafficCollectorMatchesLogToClash(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "sing-box.log")
	// Log lines: from-line then to-line, same sing-box connection id.
	lines := "+0000 2026-10-30 06:51:23 INFO [123 0ms] inbound/vless[vless-reality]: inbound connection from 1.2.3.4:21612\n" +
		// Slow connection: elapsed is "1.11s", not "1110ms" — the
		// old \d+ms regex dropped exactly these to-lines.
		"+0000 2026-10-30 06:51:24 INFO [123 1.11s] inbound/vless[vless-reality]: [test] inbound connection to example.com:443\n"
	if err := os.WriteFile(logPath, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	// Fake Clash API: one active connection from the same source.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"connections":[{"id":"abc","upload":100,"download":200,"metadata":{"sourceIP":"1.2.3.4","sourcePort":"21612"}}]}`))
	}))
	defer srv.Close()

	tc := newTrafficCollector(srv.URL, logPath, filepath.Join(dir, "traffic.json"))
	tc.tailLog()
	tc.pollClash()
	snap := tc.Snapshot()
	if snap.Up != 100 || snap.Down != 200 {
		t.Fatalf("totals = %d/%d, want 100/200", snap.Up, snap.Down)
	}
	if len(snap.ByInbound) != 1 || snap.ByInbound[0].Inbound != "vless-reality" {
		t.Fatalf("byInbound = %+v", snap.ByInbound)
	}
	if len(snap.ByUser) != 1 || snap.ByUser[0].User != "test" {
		t.Fatalf("byUser = %+v", snap.ByUser)
	}
	// Second poll with larger bytes: only the delta counts.
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"connections":[{"id":"abc","upload":150,"download":260,"metadata":{"sourceIP":"1.2.3.4","sourcePort":"21612"}}]}`))
	}))
	defer srv2.Close()
	tc.clashAddr = srv2.URL
	tc.pollClash()
	snap = tc.Snapshot()
	if snap.Up != 150 || snap.Down != 260 {
		t.Fatalf("after delta totals = %d/%d, want 150/260", snap.Up, snap.Down)
	}
	// Connection gone: totals stay, active entry removed.
	srv3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"connections":[]}`))
	}))
	defer srv3.Close()
	tc.clashAddr = srv3.URL
	tc.pollClash()
	snap = tc.Snapshot()
	if snap.Up != 150 || snap.Down != 260 {
		t.Fatalf("after close totals = %d/%d, want 150/260", snap.Up, snap.Down)
	}
	if got := readSystemStats(); got.MemTotal <= 0 && got.Load1 == 0 {
		t.Logf("system stats unavailable in this environment (ok in tests): %+v", got)
	}
}

func TestAgentActivePersistsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "traffic.json")
	c := newTrafficCollector("", "", path)
	c.active["conn-1"] = &activeConn{inbound: "vless-reality", user: "test", assigned: true, lastUp: 100, lastDown: 200}
	c.mu.Lock()
	c.saveLocked()
	c.mu.Unlock()
	c2 := newTrafficCollector("", "", path)
	ac, ok := c2.active["conn-1"]
	if !ok || ac.inbound != "vless-reality" || ac.user != "test" || !ac.assigned || ac.lastUp != 100 || ac.lastDown != 200 {
		t.Fatalf("conn-1 restored = %+v, ok=%v, want assigned test/100/200", ac, ok)
	}
}

func TestClashTagAndUnassignedBreakdown(t *testing.T) {
	if got := clashTag("vless/vless-reality"); got != "vless-reality" {
		t.Fatalf("clashTag vless = %q, want vless-reality", got)
	}
	if got := clashTag("hysteria2"); got != "" {
		t.Fatalf("clashTag no slash = %q, want empty", got)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "traffic.json")
	c := newTrafficCollector("", "", path)
	c.unByInb["vless-reality"] = &TrafficEntry{Inbound: "vless-reality", Up: 7, Down: 8}
	c.unUp, c.unDown = 7, 8
	snap := c.Snapshot()
	if len(snap.UnassignedByInbound) != 1 || snap.UnassignedByInbound[0].Up != 7 {
		t.Fatalf("unassigned breakdown = %+v, want 1 entry 7 up", snap.UnassignedByInbound)
	}
}

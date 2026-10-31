package traffic

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"singbox-ui/server/internal/store"
)

func TestLocalCollectorPoll(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(context.Background(), filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	// Insert a connection record matching the fake Clash connection.
	err = st.InsertCoreLogsWithFilePos(context.Background(), []store.CoreLogRow{
		{Ts: time.Now().UnixMilli(), Kind: "connection", Source: "1.2.3.4:21612", Tag: "vless-reality", User: "test"},
	}, "", 0, 0)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"connections":[{"id":"abc","upload":100,"download":200,"metadata":{"sourceIP":"1.2.3.4","sourcePort":"21612"}}]}`))
	}))
	defer srv.Close()
	c := New(srv.URL, st, filepath.Join(dir, "traffic.json"))
	ctx := context.Background()
	c.poll(ctx)
	snap := c.Snapshot()
	t.Logf("snapshot: %+v byInb=%+v byUser=%+v un=%d/%d", snap.Up, snap.ByInbound, snap.ByUser, snap.UnassignedUp, snap.UnassignedDown)
	if snap.Up != 100 || snap.Down != 200 {
		t.Fatalf("totals = %d/%d, want 100/200", snap.Up, snap.Down)
	}
}

func TestCollectorActivePersistsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "traffic.json")
	c := New("", nil, path)
	c.active["conn-1"] = &activeConn{inbound: "vless-reality", user: "test", assigned: true, lastUp: 100, lastDown: 200}
	c.active["conn-2"] = &activeConn{assigned: false, lastUp: 50, lastDown: 60}
	c.byKey["vless-reality\x00test"] = &Entry{Inbound: "vless-reality", User: "test", Up: 100, Down: 200}
	c.mu.Lock()
	c.saveLocked()
	c.mu.Unlock()
	// A fresh collector (as after a panel restart) must restore
	// both the attribution and the baseline, so the same live
	// connection is neither double-counted nor lost to unassigned.
	c2 := New("", nil, path)
	ac, ok := c2.active["conn-1"]
	if !ok || ac.inbound != "vless-reality" || ac.user != "test" || !ac.assigned || ac.lastUp != 100 || ac.lastDown != 200 {
		t.Fatalf("conn-1 restored = %+v, ok=%v, want assigned test/100/200", ac, ok)
	}
	if ac2, ok := c2.active["conn-2"]; !ok || ac2.assigned || ac2.lastUp != 50 {
		t.Fatalf("conn-2 restored = %+v, ok=%v, want unassigned/50", ac2, ok)
	}
}

func TestMatchRecordPicksClosestToStart(t *testing.T) {
	records := []store.ConnectionRecord{
		{Source: "1.2.3.4:1111", Tag: "vless-reality", User: "old", Ts: 1000},
		{Source: "1.2.3.4:1111", Tag: "vless-reality", User: "new", Ts: 5000},
	}
	if m := matchRecord(records, "1.2.3.4:1111", 5100); m == nil || m.User != "new" {
		t.Fatalf("closest to 5100 = %+v, want new", m)
	}
	if m := matchRecord(records, "1.2.3.4:1111", 100000); m != nil {
		t.Fatalf("far from all records = %+v, want nil (reused port rejected)", m)
	}
	if m := matchRecord(records, "9.9.9.9:2222", 5000); m != nil {
		t.Fatalf("other source = %+v, want nil", m)
	}
}


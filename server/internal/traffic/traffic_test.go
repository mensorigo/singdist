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

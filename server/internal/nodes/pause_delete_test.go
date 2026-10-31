package nodes

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"singbox-ui/server/internal/store"
)

func boolPtr(b bool) *bool { return &b }

func TestPausedDefaultAndToggle(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	svc := NewService(st, nil)
	now := time.Now().Unix()
	id, err := st.CreateNode(ctx, store.NodeRow{
		Name: "pause-node", Address: "127.0.0.1:32435", Token: "tok",
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	row, err := st.GetNode(ctx, id)
	if err != nil || row.Paused {
		t.Fatalf("default paused: row=%v err=%v, want Paused=false", row, err)
	}
	v, err := svc.Update(ctx, id, UpdateRequest{Paused: boolPtr(true)})
	if err != nil {
		t.Fatalf("Update paused=true: %v", err)
	}
	if !v.Paused {
		t.Fatalf("view paused: got false, want true")
	}
	row, _ = st.GetNode(ctx, id)
	if !row.Paused {
		t.Fatalf("stored paused: got false, want true")
	}
	if row.LastPushHash != "" {
		t.Fatalf("push hash after pause: got %q, want unchanged empty", row.LastPushHash)
	}
	list, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, n := range list {
		if n.ID == id {
			found = true
			if !n.Paused {
				t.Fatalf("list view paused: got false, want true")
			}
		}
	}
	if !found {
		t.Fatalf("list: node %d missing", id)
	}
	v, err = svc.Update(ctx, id, UpdateRequest{Paused: boolPtr(false)})
	if err != nil || v.Paused {
		t.Fatalf("Update paused=false: view=%v err=%v, want Paused=false", v, err)
	}
}

func TestPusherPushesEmptyForPaused(t *testing.T) {
	ctx := context.Background()
	var calls atomic.Int32
	var bodies []string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/agent/sync", func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"result":"applied","hash":"h","detail":"ok"}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	st := openTestStore(t)
	now := time.Now().Unix()
	id, err := st.CreateNode(ctx, store.NodeRow{
		Name: "paused-push", Address: srv.URL, Token: "tok",
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	row, err := st.GetNode(ctx, id)
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	// Simulate a previously pushed normal payload.
	if err := st.RecordNodePushSuccess(ctx, id, "old-normal-hash", "applied", now); err != nil {
		t.Fatalf("RecordNodePushSuccess: %v", err)
	}
	row, _ = st.GetNode(ctx, id)
	row.Paused = true
	if err := st.UpdateNode(ctx, *row); err != nil {
		t.Fatalf("UpdateNode paused: %v", err)
	}
	fresh, err := st.GetNode(ctx, id)
	if err != nil {
		t.Fatalf("GetNode fresh: %v", err)
	}
	// PausePending is true while the empty push has not landed.
	if v := toView(*fresh, now); !v.PausePending {
		t.Fatalf("PausePending before push: got false, want true")
	}
	p := NewPusher(st, nil, slog.Default(), "")
	res, err := p.push(ctx, *fresh, false)
	if err != nil || res != "applied" {
		t.Fatalf("push paused: got res=%q err=%v, want applied/nil", res, err)
	}
	if calls.Load() != 1 {
		t.Fatalf("agent calls: got %d, want 1 (paused push must reach the agent)", calls.Load())
	}
	var got PushPayload
	if err := json.Unmarshal([]byte(bodies[0]), &got); err != nil {
		t.Fatalf("push body JSON: %v", err)
	}
	if len(got.Inbounds) != 0 {
		t.Fatalf("paused payload inbounds: got %d, want 0", len(got.Inbounds))
	}
	after, err := st.GetNode(ctx, id)
	if err != nil {
		t.Fatalf("GetNode after: %v", err)
	}
	_, pausedHash, err := BuildPausedPush(*fresh)
	if err != nil {
		t.Fatalf("BuildPausedPush: %v", err)
	}
	if after.LastPushHash != pausedHash {
		t.Fatalf("push hash after paused push: got %q, want paused hash %q", after.LastPushHash, pausedHash)
	}
	if v := toView(*after, now); v.PausePending {
		t.Fatalf("PausePending after push: got true, want false")
	}
	// A second non-forced poll is no_change without another call.
	res, err = p.push(ctx, *after, false)
	if err != nil || res != "no_change" {
		t.Fatalf("second push paused: got res=%q err=%v, want no_change/nil", res, err)
	}
	if calls.Load() != 1 {
		t.Fatalf("agent calls after no_change: got %d, want still 1", calls.Load())
	}
	// Manual push on a paused node starts (it forces the empty push).
	started, err := p.PushNow(ctx, id)
	if err != nil || !started {
		t.Fatalf("PushNow paused: got started=%v err=%v, want true/nil", started, err)
	}
	// Give the background goroutine a moment to run.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if calls.Load() >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if calls.Load() < 2 {
		t.Fatalf("agent calls after PushNow: got %d, want >= 2", calls.Load())
	}
}

func TestDeletePanelOnly(t *testing.T) {
	ctx := context.Background()
	var calls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	st := openTestStore(t)
	svc := NewService(st, nil)
	now := time.Now().Unix()
	mkInbound := func(name string, port int, runLocal bool) int64 {
		t.Helper()
		id, err := st.CreateInbound(ctx, name, "trojan", port,
			`{"password":"pw","sni":"example.com"}`, runLocal, now)
		if err != nil {
			t.Fatalf("CreateInbound %s: %v", name, err)
		}
		return id
	}
	ibSingle := mkInbound("node-only", 14431, false)
	ibShared := mkInbound("shared", 14432, false)
	ibLocal := mkInbound("local-plus", 14433, true)
	ibOther := mkInbound("other-only", 14434, false)
	nodeA, err := st.CreateNode(ctx, store.NodeRow{
		Name: "node-a", Address: srv.URL, Token: "tok",
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("CreateNode A: %v", err)
	}
	nodeB, err := st.CreateNode(ctx, store.NodeRow{
		Name: "node-b", Address: srv.URL, Token: "tok",
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("CreateNode B: %v", err)
	}
	assign := func(inboundID, nodeID int64, others ...int64) {
		t.Helper()
		nodes := []store.InboundAssignment{{NodeID: nodeID, Port: 0}}
		for _, o := range others {
			nodes = append(nodes, store.InboundAssignment{NodeID: o, Port: 0})
		}
		if err := st.SetInboundNodes(ctx, inboundID, nodes); err != nil {
			t.Fatalf("SetInboundNodes %d: %v", inboundID, err)
		}
	}
	assign(ibSingle, nodeA)
	assign(ibShared, nodeA, nodeB)
	assign(ibLocal, nodeA)
	assign(ibOther, nodeB)
	tpl1, err := st.CreateTemplate(ctx, "tpl1",
		fmt.Sprintf("[%d,%d,%d,%d]", ibSingle, ibShared, ibLocal, ibOther), "", now)
	if err != nil {
		t.Fatalf("CreateTemplate tpl1: %v", err)
	}
	tpl2, err := st.CreateTemplate(ctx, "tpl2",
		fmt.Sprintf("[%d]", ibSingle), "", now)
	if err != nil {
		t.Fatalf("CreateTemplate tpl2: %v", err)
	}

	res, err := svc.Delete(ctx, nodeA)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if res.Assignments != 3 {
		t.Fatalf("assignments: got %d, want 3", res.Assignments)
	}
	if len(res.DeletedInbounds) != 1 || res.DeletedInbounds[0] != "node-only" {
		t.Fatalf("deleted inbounds: got %v, want [node-only]", res.DeletedInbounds)
	}
	if calls.Load() != 0 {
		t.Fatalf("agent calls: got %d, want 0 (delete must be panel-only)", calls.Load())
	}
	if _, err := st.GetNode(ctx, nodeA); err == nil {
		t.Fatalf("node row: still present, want deleted")
	}
	ids, err := st.NodeInboundIDs(ctx, nodeA)
	if err != nil || len(ids) != 0 {
		t.Fatalf("node assignments: got %v err=%v, want empty", ids, err)
	}
	remaining, err := st.ListInbounds(ctx)
	if err != nil {
		t.Fatalf("ListInbounds: %v", err)
	}
	byID := map[int64]bool{}
	for _, r := range remaining {
		byID[r.ID] = true
	}
	if byID[ibSingle] {
		t.Fatalf("single-machine inbound: still present, want deleted")
	}
	for _, id := range []int64{ibShared, ibLocal, ibOther} {
		if !byID[id] {
			t.Fatalf("inbound %d: missing, want kept", id)
		}
	}
	checkTpl := func(tplID int64, want []int64) {
		t.Helper()
		tpl, err := st.GetTemplate(ctx, tplID)
		if err != nil {
			t.Fatalf("GetTemplate %d: %v", tplID, err)
		}
		var got []int64
		if err := json.Unmarshal([]byte(tpl.InboundIDs), &got); err != nil {
			t.Fatalf("template %d JSON: %v", tplID, err)
		}
		if len(got) != len(want) {
			t.Fatalf("template %d: got %v, want %v", tplID, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("template %d: got %v, want %v", tplID, got, want)
			}
		}
	}
	checkTpl(tpl1, []int64{ibShared, ibLocal, ibOther})
	checkTpl(tpl2, []int64{})
	if _, err := svc.Delete(ctx, nodeA); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("delete missing node: got err=%v, want not-found", err)
	}
}

package render

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"singbox-ui/server/internal/store"
)

// A paused node is excluded from subscription rendering even when
// online; enabling restores its entry. The node's own config is never
// touched here — rendering is a pure read path.
func TestBuildAllNodesExcludesPaused(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer func() { _ = st.Close() }()
	now := time.Now().Unix()
	ibID, err := st.CreateInbound(ctx, "remote-ss", "shadowsocks", 18383,
		`{"method":"aes-256-gcm","password":"pw"}`, false, now)
	if err != nil {
		t.Fatalf("CreateInbound: %v", err)
	}
	tplID, err := st.CreateTemplate(ctx, "tpl", fmt.Sprintf("[%d]", ibID), "", now)
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}
	nodeID, err := st.CreateNode(ctx, store.NodeRow{
		Name: "edge", Address: "127.0.0.1:32435", Token: "tok",
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	if err := st.SetInboundNodes(ctx, ibID,
		[]store.InboundAssignment{{NodeID: nodeID, Port: 0}}); err != nil {
		t.Fatalf("SetInboundNodes: %v", err)
	}
	if err := st.RecordNodeOnline(ctx, nodeID, 1, "0.2.0", now); err != nil {
		t.Fatalf("RecordNodeOnline: %v", err)
	}
	svc := NewService(st, nil)
	sub := store.SubscriptionRow{TemplateID: tplID, Name: "sub1", UUID: "uuid-1"}
	nodes, err := svc.buildAllNodes(ctx, sub, "panel.example")
	if err != nil {
		t.Fatalf("buildAllNodes enabled: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Name != "edge-remote-ss" {
		t.Fatalf("enabled nodes: got %v, want one edge-remote-ss", nodes)
	}
	row, err := st.GetNode(ctx, nodeID)
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	row.Paused = true
	if err := st.UpdateNode(ctx, *row); err != nil {
		t.Fatalf("UpdateNode paused: %v", err)
	}
	nodes, err = svc.buildAllNodes(ctx, sub, "panel.example")
	if err != nil {
		t.Fatalf("buildAllNodes paused: %v", err)
	}
	if len(nodes) != 0 {
		t.Fatalf("paused nodes: got %d, want 0", len(nodes))
	}
	row.Paused = false
	if err := st.UpdateNode(ctx, *row); err != nil {
		t.Fatalf("UpdateNode enabled: %v", err)
	}
	nodes, err = svc.buildAllNodes(ctx, sub, "panel.example")
	if err != nil {
		t.Fatalf("buildAllNodes re-enabled: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("re-enabled nodes: got %d, want 1", len(nodes))
	}
}

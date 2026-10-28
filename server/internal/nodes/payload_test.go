package nodes

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"

	"singbox-ui/server/internal/certs"
	"singbox-ui/server/internal/store"
)

func seedPayloadStore(t *testing.T) (*store.Store, *certs.Manager, store.NodeRow, int64, int64) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(ctx, filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	cm, err := certs.NewManager(dir)
	if err != nil {
		t.Fatalf("certs.NewManager: %v", err)
	}

	now := int64(1700000000)
	vlessID, err := st.CreateInbound(ctx, "v1", "vless-reality", 443,
		`{"sni":"example.com","publicKey":"k","privateKey":"p","shortId":"","flow":"xtls-rprx-vision"}`, now)
	if err != nil {
		t.Fatalf("CreateInbound vless: %v", err)
	}
	ssID, err := st.CreateInbound(ctx, "ss1", "shadowsocks", 8388,
		`{"method":"2022-blake3-aes-128-gcm","password":"pw"}`, now)
	if err != nil {
		t.Fatalf("CreateInbound ss: %v", err)
	}
	tplID, err := st.CreateTemplate(ctx, "all", `[`+strconv.FormatInt(vlessID, 10)+`,`+strconv.FormatInt(ssID, 10)+`]`, now)
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}
	if _, err := st.CreateSubscription(ctx, store.SubscriptionRow{
		Name: "sub1", UUID: "11111111-2222-3333-4444-555555555555",
		Token: "tok1", TemplateID: tplID, Enabled: true, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	nodeID, err := st.CreateNode(ctx, store.NodeRow{
		Name: "tokyo", Address: "203.0.113.7:32435", Token: "ntok",
		PublicHost: "node.example.com", CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	node, err := st.GetNode(ctx, nodeID)
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if err := st.SetInboundNodes(ctx, vlessID, []int64{nodeID}); err != nil {
		t.Fatalf("SetInboundNodes vless: %v", err)
	}
	if err := st.SetInboundNodes(ctx, ssID, []int64{nodeID}); err != nil {
		t.Fatalf("SetInboundNodes ss: %v", err)
	}
	return st, cm, *node, vlessID, ssID
}

func TestBuildPushUsersAndHash(t *testing.T) {
	ctx := context.Background()
	st, cm, node, _, _ := seedPayloadStore(t)

	p1, h1, err := BuildPush(ctx, st, cm, node)
	if err != nil {
		t.Fatalf("BuildPush: %v", err)
	}
	if len(p1.Inbounds) != 2 {
		t.Fatalf("BuildPush: got %d inbounds, want 2", len(p1.Inbounds))
	}
	if p1.PublicHost != "node.example.com" {
		t.Fatalf("BuildPush publicHost: got %q, want node.example.com", p1.PublicHost)
	}
	if p1.Cert != nil {
		t.Fatalf("BuildPush cert: got non-nil, want nil (no TLS-cert inbound)")
	}
	// vless carries the subscription user; shadowsocks carries none.
	var vless, ss *PushInbound
	for i := range p1.Inbounds {
		switch p1.Inbounds[i].Name {
		case "v1":
			vless = &p1.Inbounds[i]
		case "ss1":
			ss = &p1.Inbounds[i]
		}
	}
	if vless == nil || ss == nil {
		t.Fatalf("BuildPush: missing inbounds: %+v", p1.Inbounds)
	}
	if len(vless.Users) != 1 || vless.Users[0].Name != "sub1" ||
		vless.Users[0].UUID != "11111111-2222-3333-4444-555555555555" {
		t.Fatalf("vless users: got %+v, want [{sub1 uuid}]", vless.Users)
	}
	if len(ss.Users) != 0 {
		t.Fatalf("ss users: got %+v, want none", ss.Users)
	}

	// Hash stability: same state -> same hash.
	_, h2, err := BuildPush(ctx, st, cm, node)
	if err != nil {
		t.Fatalf("BuildPush again: %v", err)
	}
	if h1 != h2 {
		t.Fatalf("hash unstable: %q vs %q", h1, h2)
	}
}

func TestBuildPushUnassignedAndDisabled(t *testing.T) {
	ctx := context.Background()
	st, cm, node, vlessID, _ := seedPayloadStore(t)
	now := int64(1700000000)

	// An unassigned inbound must not appear in the payload.
	if _, err := st.CreateInbound(ctx, "orphan", "trojan", 8443,
		`{"password":"pw","sni":""}`, now); err != nil {
		t.Fatalf("CreateInbound orphan: %v", err)
	}
	p, _, err := BuildPush(ctx, st, cm, node)
	if err != nil {
		t.Fatalf("BuildPush: %v", err)
	}
	for _, ib := range p.Inbounds {
		if ib.Name == "orphan" {
			t.Fatalf("BuildPush: unassigned inbound leaked into payload")
		}
	}

	// Disabling the subscription drops its users from every inbound.
	subs, err := st.ListSubscriptions(ctx)
	if err != nil {
		t.Fatalf("ListSubscriptions: %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("ListSubscriptions: got %d, want 1", len(subs))
	}
	if _, err := st.SetSubscriptionEnabled(ctx, subs[0].ID, false, now); err != nil {
		t.Fatalf("SetSubscriptionEnabled: %v", err)
	}
	p, _, err = BuildPush(ctx, st, cm, node)
	if err != nil {
		t.Fatalf("BuildPush after disable: %v", err)
	}
	for _, ib := range p.Inbounds {
		if ib.Name == "v1" && len(ib.Users) != 0 {
			t.Fatalf("vless users after disable: got %+v, want none", ib.Users)
		}
	}

	// Unassigning the vless inbound removes it from the payload.
	if err := st.SetInboundNodes(ctx, vlessID, nil); err != nil {
		t.Fatalf("SetInboundNodes empty: %v", err)
	}
	p, _, err = BuildPush(ctx, st, cm, node)
	if err != nil {
		t.Fatalf("BuildPush after unassign: %v", err)
	}
	if len(p.Inbounds) != 1 || p.Inbounds[0].Name != "ss1" {
		t.Fatalf("BuildPush after unassign: got %+v, want only ss1", p.Inbounds)
	}
}

func TestBuildPushCertRequired(t *testing.T) {
	ctx := context.Background()
	st, cm, node, _, _ := seedPayloadStore(t)
	now := int64(1700000000)

	hy2ID, err := st.CreateInbound(ctx, "hy1", "hysteria2", 8443,
		`{"password":"pw","upMbps":0,"downMbps":0,"obfs":"","obfsPassword":""}`, now)
	if err != nil {
		t.Fatalf("CreateInbound hy2: %v", err)
	}
	if err := st.SetInboundNodes(ctx, hy2ID, []int64{node.ID}); err != nil {
		t.Fatalf("SetInboundNodes hy2: %v", err)
	}

	// No active certificate yet: the push must fail loudly, not silently
	// ship a TLS inbound without a cert.
	if _, _, err := BuildPush(ctx, st, cm, node); err == nil {
		t.Fatalf("BuildPush without cert: expected error, got nil")
	}

	if _, err := cm.GenerateSelfSigned([]string{"node.example.com"}, 365, "test", true); err != nil {
		t.Fatalf("GenerateSelfSigned: %v", err)
	}
	p, _, err := BuildPush(ctx, st, cm, node)
	if err != nil {
		t.Fatalf("BuildPush with cert: %v", err)
	}
	if p.Cert == nil || p.Cert.Certificate == "" || p.Cert.Key == "" {
		t.Fatalf("BuildPush cert: got %+v, want embedded cert+key", p.Cert)
	}
}

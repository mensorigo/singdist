package traffic

import (
	"context"
	"path/filepath"
	"testing"

	"singbox-ui/server/internal/nodes"
	"singbox-ui/server/internal/store"
)

func TestSummaryMapsVlessUserToSubscription(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(context.Background(), filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	// Minimal rows via direct SQL-like helpers: use the store's
	// public creation paths if available, else skip.
	_ = ctx
	_ = st
	_ = nodes.NewService
	// This test is a placeholder for the mapping function itself,
	// which is the only logic that decides per-subscription totals.
	subs := []store.SubscriptionRow{{Name: "test"}, {Name: "test2"}}
	if got := coreUserToSubscription("test", "vless-reality", "vless-reality", subs); got != "test" {
		t.Fatalf("vless test -> %q, want test", got)
	}
	if got := coreUserToSubscription("test-hy2", "hy2", "hysteria2", subs); got != "test" {
		t.Fatalf("hy2 test-hy2 -> %q, want test", got)
	}
	if got := coreUserToSubscription("trojan", "trojan", "trojan", subs); got != "" {
		t.Fatalf("trojan single -> %q, want empty (legacy shared)", got)
	}
	if got := coreUserToSubscription("test-trojan", "trojan", "trojan", subs); got != "test" {
		t.Fatalf("trojan test-trojan -> %q, want test", got)
	}
}

func TestSyncUsageWritesCollectorTotals(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := int64(1700000000)
	if _, err := st.CreateInbound(ctx, "vless-reality", "vless-reality", 443,
		`{"sni":"example.com","publicKey":"k","privateKey":"p","shortId":"","flow":""}`, true, now); err != nil {
		t.Fatal(err)
	}
	tplID, err := st.CreateTemplate(ctx, "tpl", "[1]", "", now)
	if err != nil {
		t.Fatal(err)
	}
	subID, err := st.CreateSubscription(ctx, store.SubscriptionRow{
		Name: "test", UUID: "11111111-2222-3333-4444-555555555555",
		Token: "tok", TemplateID: tplID,
	})
	if err != nil {
		t.Fatal(err)
	}
	c := New("http://127.0.0.1:9", st, "")
	c.byKey["vless-reality\x00test"] = &Entry{Inbound: "vless-reality", User: "test", Up: 100, Down: 200}
	c.byInb["vless-reality"] = &Entry{Inbound: "vless-reality", Up: 100, Down: 200}
	svc := NewSummaryService(st, nodes.NewService(st, nil), c)
	n, err := svc.SyncUsage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("SyncUsage updated %d, want 1", n)
	}
	row, err := st.GetSubscription(ctx, subID)
	if err != nil {
		t.Fatal(err)
	}
	if row.UsedUp != 100 || row.UsedDown != 200 {
		t.Fatalf("usage = %d/%d, want 100/200", row.UsedUp, row.UsedDown)
	}
	// Second run with unchanged totals writes nothing.
	n, err = svc.SyncUsage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("SyncUsage second run updated %d, want 0", n)
	}
}

func TestSoleSubscriberUnassignedAllocated(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := int64(1700000000)
	if _, err := st.CreateInbound(ctx, "vless-reality", "vless-reality", 443,
		`{"sni":"example.com","publicKey":"k","privateKey":"p","shortId":"","flow":""}`, true, now); err != nil {
		t.Fatal(err)
	}
	tplID, err := st.CreateTemplate(ctx, "tpl", "[1]", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateSubscription(ctx, store.SubscriptionRow{
		Name: "test", UUID: "11111111-2222-3333-4444-555555555555", Token: "tok", TemplateID: tplID,
	}); err != nil {
		t.Fatal(err)
	}
	c := New("http://127.0.0.1:9", st, "")
	c.byKey["vless-reality\x00test"] = &Entry{Inbound: "vless-reality", User: "test", Up: 100, Down: 200}
	c.byInb["vless-reality"] = &Entry{Inbound: "vless-reality", Up: 100, Down: 200}
	c.unByInb["vless-reality"] = &Entry{Inbound: "vless-reality", Up: 10, Down: 20}
	c.unUp, c.unDown = 10, 20
	svc := NewSummaryService(st, nodes.NewService(st, nil), c)
	sum, err := svc.Summary(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sum.BySubscription) != 1 {
		t.Fatalf("subscriptions = %d, want 1", len(sum.BySubscription))
	}
	got := sum.BySubscription[0]
	if got.Up != 100 || got.UnassignedUp != 10 || got.EstimatedUp != 110 || got.EstimatedDown != 220 {
		t.Fatalf("test = %+v, want Up 100 UnassignedUp 10 Estimated 110/220", got)
	}
}

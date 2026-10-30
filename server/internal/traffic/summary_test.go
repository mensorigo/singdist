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
		t.Fatalf("trojan single -> %q, want empty (shared)", got)
	}
}

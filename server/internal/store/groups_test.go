package store

import "testing"

func TestEffectiveProxyGroupsDefaults(t *testing.T) {
	g := EffectiveProxyGroups("", []int64{1, 2})
	if len(g) != 3 || g[0].Name != "Proxy" || g[1].Name != "Auto" || g[2].Name != "Fallback" {
		t.Fatalf("defaults = %+v", g)
	}
	if len(g[1].InboundIDs) != 2 || g[1].Type != "url-test" || g[1].URL == "" {
		t.Fatalf("auto group = %+v", g[1])
	}
}

func TestValidateProxyGroupsRejectsOutsideInbound(t *testing.T) {
	_, _, err := ValidateProxyGroups([]ProxyGroup{{Name: "X", Type: "select", InboundIDs: []int64{9}, Enabled: true}}, []int64{1})
	if err == nil {
		t.Fatal("expected error for inbound outside template")
	}
	_, _, err = ValidateProxyGroups([]ProxyGroup{{Name: "X", Type: "select", InboundIDs: []int64{1}, Enabled: true}}, []int64{1})
	if err != nil {
		t.Fatalf("valid groups rejected: %v", err)
	}
}

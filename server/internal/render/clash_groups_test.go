package render

import (
	"strings"
	"testing"

	"singbox-ui/server/internal/settings"
)

func TestRenderClashGroups(t *testing.T) {
	nodes := []Node{
		{Name: "a", Protocol: "trojan", Server: "h", Port: 443, Password: "p", InboundID: 1},
		{Name: "b", Protocol: "trojan", Server: "h", Port: 8443, Password: "p", InboundID: 2},
	}
	groups := []ClashGroup{
		{Name: "Proxy", Type: "select", Proxies: []string{"a", "b"}},
		{Name: "Auto", Type: "url-test", Proxies: []string{"a"}, URL: "https://www.gstatic.com/generate_204", Interval: 300, Tolerance: 50},
		{Name: "Fallback", Type: "fallback", Proxies: []string{"b"}, URL: "https://www.gstatic.com/generate_204", Interval: 300},
	}
	out := renderClash(nodes, groups, settings.View{}, "h")
	for _, want := range []string{"type: url-test", "type: fallback", "tolerance: 50", "MATCH,Proxy"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Count(out, "name: \"Auto\"") != 1 {
		t.Fatalf("Auto group not rendered once:\n%s", out)
	}
}

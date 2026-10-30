package store

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ProxyGroup is one Clash proxy-group inside a template. InboundIDs are
// template inbound ids; the render step maps them to generated node
// names (local + node endpoints), so a group over one inbound can yield
// several node entries. Groups are overlapping sets, not partitions:
// the same inbound may appear in many groups, and Global mode in the
// client simply uses every node the template generates.
type ProxyGroup struct {
	Name       string  `json:"name"`
	Type       string  `json:"type"` // select | url-test | fallback | load-balance
	InboundIDs []int64 `json:"inboundIds"`
	Enabled    bool    `json:"enabled"`
	URL        string  `json:"url,omitempty"`
	Interval   int     `json:"interval,omitempty"`
	Tolerance  int     `json:"tolerance,omitempty"`
}

const (
	DefaultGroupTestURL       = "https://www.gstatic.com/generate_204"
	DefaultGroupTestInterval  = 300
	DefaultGroupTestTolerance = 50
)

// DefaultProxyGroups returns the three default groups over all of the
// template's inbounds, in display order.
func DefaultProxyGroups(templateInboundIDs []int64) []ProxyGroup {
	ids := append([]int64{}, templateInboundIDs...)
	return []ProxyGroup{
		{Name: "Proxy", Type: "select", InboundIDs: ids, Enabled: true},
		{Name: "Auto", Type: "url-test", InboundIDs: append([]int64{}, ids...), Enabled: true, URL: DefaultGroupTestURL, Interval: DefaultGroupTestInterval, Tolerance: DefaultGroupTestTolerance},
		{Name: "Fallback", Type: "fallback", InboundIDs: append([]int64{}, ids...), Enabled: true, URL: DefaultGroupTestURL, Interval: DefaultGroupTestInterval, Tolerance: DefaultGroupTestTolerance},
	}
}

// EffectiveProxyGroups parses a stored groups JSON blob. Empty, blank
// or unparseable storage means the defaults (pre-existing templates
// keep working with no data migration); disabled groups are kept in
// the list so the UI can re-enable them, and the render step skips
// them. Groups referencing inbounds no longer in the template are
// dropped at the group level only when the whole group is empty after
// filtering — the render step intersects with generated nodes anyway.
func EffectiveProxyGroups(raw string, templateInboundIDs []int64) []ProxyGroup {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return DefaultProxyGroups(templateInboundIDs)
	}
	var groups []ProxyGroup
	if err := json.Unmarshal([]byte(raw), &groups); err != nil || len(groups) == 0 {
		return DefaultProxyGroups(templateInboundIDs)
	}
	return groups
}

// ValidateProxyGroups checks a client-supplied group list against the
// template's inbound ids. Names are unique case-insensitively and
// 1-64 runes; every referenced inbound must belong to the template.
func ValidateProxyGroups(groups []ProxyGroup, templateInboundIDs []int64) ([]ProxyGroup, string, error) {
	allowed := map[int64]bool{}
	for _, id := range templateInboundIDs {
		allowed[id] = true
	}
	seen := map[string]bool{}
	out := []ProxyGroup{}
	for _, g := range groups {
		name := strings.TrimSpace(g.Name)
		if name == "" || len([]rune(name)) > 64 {
			return nil, "", fmt.Errorf("group name must be 1-64 characters")
		}
		if seen[strings.ToLower(name)] {
			return nil, "", fmt.Errorf("duplicate group name %q", name)
		}
		seen[strings.ToLower(name)] = true
		switch g.Type {
		case "select", "url-test", "fallback", "load-balance":
		default:
			return nil, "", fmt.Errorf("group %q has unknown type %q", name, g.Type)
		}
		idsSeen := map[int64]bool{}
		ids := []int64{}
		for _, id := range g.InboundIDs {
			if !allowed[id] {
				return nil, "", fmt.Errorf("group %q references inbound %d outside the template", name, id)
			}
			if !idsSeen[id] {
				idsSeen[id] = true
				ids = append(ids, id)
			}
		}
		ng := ProxyGroup{Name: name, Type: g.Type, InboundIDs: ids, Enabled: g.Enabled, URL: strings.TrimSpace(g.URL), Interval: g.Interval, Tolerance: g.Tolerance}
		if ng.Type != "select" {
			if ng.URL == "" {
				ng.URL = DefaultGroupTestURL
			}
			if ng.Interval <= 0 {
				ng.Interval = DefaultGroupTestInterval
			}
			if ng.Tolerance < 0 {
				ng.Tolerance = DefaultGroupTestTolerance
			}
			if ng.Type == "url-test" && ng.Tolerance == 0 {
				ng.Tolerance = DefaultGroupTestTolerance
			}
		}
		out = append(out, ng)
	}
	b, _ := json.Marshal(out)
	return out, string(b), nil
}

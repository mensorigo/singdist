package render

import (
	"encoding/json"

	"singbox-ui/server/internal/settings"
)

// renderSingbox emits a sing-box client configuration (current config
// format): DNS with a fallback-driven remote server, one selector
// group named "Proxy", and route rules mirroring the Clash output
// (STUN policy, server direct, private/CN direct via rule sets).
func renderSingbox(nodes []Node, cfg settings.View, host string) (string, error) {
	remoteServer, localServer := "dns.cloudflare.com", "dns.alidns.com"
	if cfg.DNSFallback == "domestic" {
		remoteServer = "dns.alidns.com"
	}

	dns := map[string]any{
		"servers": []any{
			map[string]any{"type": "https", "tag": "remote", "server": remoteServer},
			map[string]any{"type": "https", "tag": "local", "server": localServer},
			map[string]any{"type": "udp", "tag": "bootstrap", "server": "223.5.5.5"},
		},
		"final": "remote",
	}
	if cfg.DNSRespectRules {
		dns["rules"] = []any{
			map[string]any{"rule_set": []string{"geosite-cn", "geosite-private"}, "server": "local"},
		}
	}

	outbounds := []any{}
	names := []string{}
	for _, n := range nodes {
		outbounds = append(outbounds, singboxOutbound(n, cfg))
		names = append(names, n.Name)
	}
	selector := map[string]any{
		"type": "selector", "tag": "Proxy",
		"outbounds": append(names, "direct"),
	}
	outbounds = append(outbounds, selector, map[string]any{"type": "direct", "tag": "direct"})

	rules := []any{}
	switch cfg.WebRTCMode {
	case "proxy":
		rules = append(rules, map[string]any{"network": []string{"udp"}, "port": []int{3478, 19302}, "outbound": "Proxy"})
	case "reject":
		rules = append(rules, map[string]any{"network": []string{"udp"}, "port": []int{3478, 19302}, "action": "reject"})
	}
	if host != "" {
		if isIP(host) {
			rules = append(rules, map[string]any{"ip_cidr": []string{host + "/32"}, "outbound": "direct"})
		} else {
			rules = append(rules, map[string]any{"domain": []string{host}, "outbound": "direct"})
		}
	}
	rules = append(rules,
		map[string]any{"ip_is_private": true, "outbound": "direct"},
		map[string]any{"rule_set": []string{"geosite-cn", "geoip-cn"}, "outbound": "direct"},
	)

	doc := map[string]any{
		"log":       map[string]any{"level": "info"},
		"dns":       dns,
		"outbounds": outbounds,
		"route": map[string]any{
			"rules": rules,
			"rule_set": []any{
				map[string]any{
					"type": "remote", "tag": "geosite-cn", "format": "binary",
					"url":             "https://raw.githubusercontent.com/SagerNet/sing-geosite/rule-set/geosite-cn.srs",
					"download_detour": "Proxy",
				},
				map[string]any{
					"type": "remote", "tag": "geosite-private", "format": "binary",
					"url":             "https://raw.githubusercontent.com/SagerNet/sing-geosite/rule-set/geosite-private.srs",
					"download_detour": "Proxy",
				},
				map[string]any{
					"type": "remote", "tag": "geoip-cn", "format": "binary",
					"url":             "https://raw.githubusercontent.com/SagerNet/sing-geoip/rule-set/geoip-cn.srs",
					"download_detour": "Proxy",
				},
			},
			"final": "Proxy",
		},
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out) + "\n", nil
}

func singboxOutbound(n Node, cfg settings.View) map[string]any {
	base := map[string]any{"tag": n.Name, "server": n.Server, "server_port": n.Port}
	switch n.Protocol {
	case "vless-reality":
		base["type"] = "vless"
		base["uuid"] = n.UUID
		if n.Flow != "" {
			base["flow"] = n.Flow
		}
		base["tls"] = map[string]any{
			"enabled":     true,
			"server_name": n.SNI,
			"utls":        map[string]any{"enabled": true, "fingerprint": "chrome"},
			"reality":     map[string]any{"enabled": true, "public_key": n.PublicKey, "short_id": n.ShortID},
		}
	case "hysteria2":
		base["type"] = "hysteria2"
		base["password"] = n.Password
		if n.UpMbps > 0 {
			base["up_mbps"] = n.UpMbps
		}
		if n.DownMbps > 0 {
			base["down_mbps"] = n.DownMbps
		}
		if n.Obfs == "salamander" && n.ObfsPassword != "" {
			base["obfs"] = map[string]any{"type": "salamander", "password": n.ObfsPassword}
		}
		base["tls"] = map[string]any{
			"enabled": true, "server_name": n.SNI, "alpn": []string{"h3"},
			"insecure": cfg.SkipCertVerify,
		}
	case "tuic":
		base["type"] = "tuic"
		base["uuid"] = n.UUID
		base["password"] = n.Password
		base["congestion_control"] = n.Congestion
		base["tls"] = map[string]any{
			"enabled": true, "server_name": n.SNI, "alpn": []string{"h3"},
			"insecure": cfg.SkipCertVerify,
		}
	case "anytls":
		base["type"] = "anytls"
		base["password"] = n.Password
		base["tls"] = map[string]any{
			"enabled": true, "server_name": n.SNI,
			"insecure": cfg.SkipCertVerify,
		}
	case "trojan":
		base["type"] = "trojan"
		base["password"] = n.Password
		base["tls"] = map[string]any{
			"enabled": true, "server_name": n.SNI,
			"insecure": cfg.SkipCertVerify,
		}
	}
	return base
}

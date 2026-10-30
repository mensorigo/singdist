package render

import (
	"fmt"
	"strconv"
	"strings"

	"singbox-ui/server/internal/settings"
)

var (
	dnsDomestic = []string{"https://dns.alidns.com/dns-query", "https://doh.pub/dns-query"}
	dnsForeign  = []string{"https://dns.cloudflare.com/dns-query", "https://dns.google/dns-query"}
)

// ClashGroup is one rendered proxy-group: name, Clash type, and the
// generated node names it contains. URL/interval/tolerance apply to
// url-test, fallback and load-balance groups; select groups ignore them.
type ClashGroup struct {
	Name      string
	Type      string
	Proxies   []string
	URL       string
	Interval  int
	Tolerance int
}

// renderClash emits the Clash (Meta) YAML document. The layout mirrors
// what mainstream panels (and the previous panel) produce: header,
// fake-ip DNS with a fallback set chosen by settings, the template's
// proxy groups (Proxy select first, then Auto/Fallback/custom), STUN
// policy rules, private/CN direct, MATCH last (to Proxy when present,
// else to the first group).
func renderClash(nodes []Node, groups []ClashGroup, cfg settings.View, host string) string {
	fallback := dnsForeign
	if cfg.DNSFallback == "domestic" {
		fallback = dnsDomestic
	}

	var b strings.Builder
	b.WriteString("mixed-port: 7890\nallow-lan: false\nmode: rule\nlog-level: info\nipv6: false\n\n")

	// --- DNS ---
	b.WriteString("dns:\n  enable: true\n  ipv6: false\n  enhanced-mode: fake-ip\n  fake-ip-range: 198.18.0.1/16\n")
	if cfg.DNSRespectRules {
		b.WriteString("  respect-rules: true\n")
	}
	b.WriteString("  default-nameserver:\n    - 223.5.5.5\n    - 119.29.29.29\n")
	writeList(&b, "  nameserver:", fallback, "    - ")
	b.WriteString("  proxy-server-nameserver:\n    - https://dns.alidns.com/dns-query\n    - 223.5.5.5\n")
	b.WriteString("  nameserver-policy:\n")
	writeList(&b, "    "+strconv.Quote("geosite:cn,private")+":", dnsDomestic, "      - ")
	writeList(&b, "    "+strconv.Quote("geosite:geolocation-!cn")+":", fallback, "      - ")
	b.WriteString("\n")

	// --- proxies ---
	b.WriteString("proxies:\n")
	for _, n := range nodes {
		writeClashProxy(&b, n, cfg)
	}
	b.WriteString("\n")

	// --- groups ---
	b.WriteString("proxy-groups:\n")
	for _, g := range groups {
		fmt.Fprintf(&b, "  - name: %s\n    type: %s\n", strconv.Quote(g.Name), g.Type)
		if g.Type == "url-test" || g.Type == "fallback" || g.Type == "load-balance" {
			url := g.URL
			if url == "" {
				url = "https://www.gstatic.com/generate_204"
			}
			interval := g.Interval
			if interval <= 0 {
				interval = 300
			}
			fmt.Fprintf(&b, "    url: %s\n    interval: %d\n", url, interval)
			if g.Type == "url-test" {
				tolerance := g.Tolerance
				if tolerance <= 0 {
					tolerance = 50
				}
				fmt.Fprintf(&b, "    tolerance: %d\n", tolerance)
			}
			if g.Type == "load-balance" {
				b.WriteString("    strategy: round-robin\n")
			}
		}
		b.WriteString("    proxies:\n")
		for _, name := range g.Proxies {
			fmt.Fprintf(&b, "      - %s\n", name)
		}
		if g.Type == "select" {
			b.WriteString("      - DIRECT\n")
		}
	}
	b.WriteString("\n")

	// --- rules ---
	b.WriteString("rules:\n")
	switch cfg.WebRTCMode {
	case "proxy":
		b.WriteString("  - AND,((NETWORK,UDP),(DST-PORT,3478)),Proxy\n  - AND,((NETWORK,UDP),(DST-PORT,19302)),Proxy\n")
	case "reject":
		b.WriteString("  - AND,((NETWORK,UDP),(DST-PORT,3478)),REJECT\n  - AND,((NETWORK,UDP),(DST-PORT,19302)),REJECT\n")
	}
	if host != "" {
		if isIP(host) {
			fmt.Fprintf(&b, "  - IP-CIDR,%s/32,DIRECT,no-resolve\n", host)
		} else {
			fmt.Fprintf(&b, "  - DOMAIN,%s,DIRECT\n", host)
		}
	}
	match := ""
	for _, g := range groups {
		if g.Name == "Proxy" {
			match = "Proxy"
			break
		}
	}
	if match == "" && len(groups) > 0 {
		match = groups[0].Name
	}
	if match == "" {
		match = "DIRECT"
	}
	b.WriteString("  - GEOSITE,private,DIRECT\n  - GEOIP,private,DIRECT,no-resolve\n  - GEOSITE,cn,DIRECT\n  - GEOIP,CN,DIRECT\n")
	fmt.Fprintf(&b, "  - MATCH,%s\n", match)
	return b.String()
}

func writeList(b *strings.Builder, header string, items []string, prefix string) {
	b.WriteString(header + "\n")
	for _, it := range items {
		b.WriteString(prefix + it + "\n")
	}
}

func writeClashProxy(b *strings.Builder, n Node, cfg settings.View) {
	q := strconv.Quote
	fmt.Fprintf(b, "  - name: %s\n", q(n.Name))
	switch n.Protocol {
	case "vless-reality":
		fmt.Fprintf(b, "    type: vless\n    server: %s\n    port: %d\n    uuid: %s\n    network: tcp\n    tls: true\n    udp: true\n",
			n.Server, n.Port, n.UUID)
		if n.Flow != "" {
			fmt.Fprintf(b, "    flow: %s\n", n.Flow)
		}
		fmt.Fprintf(b, "    servername: %s\n    client-fingerprint: chrome\n    reality-opts:\n      public-key: %s\n",
			n.SNI, n.PublicKey)
		if n.ShortID != "" {
			fmt.Fprintf(b, "      short-id: %s\n", n.ShortID)
		}
	case "hysteria2":
		fmt.Fprintf(b, "    type: hysteria2\n    server: %s\n    port: %d\n    password: %s\n",
			n.Server, n.Port, n.Password)
		if n.UpMbps > 0 {
			fmt.Fprintf(b, "    up: \"%d Mbps\"\n", n.UpMbps)
		}
		if n.DownMbps > 0 {
			fmt.Fprintf(b, "    down: \"%d Mbps\"\n", n.DownMbps)
		}
		if n.Obfs == "salamander" && n.ObfsPassword != "" {
			fmt.Fprintf(b, "    obfs: salamander\n    obfs-password: %s\n", n.ObfsPassword)
		}
		b.WriteString("    alpn:\n      - h3\n")
		fmt.Fprintf(b, "    sni: %s\n", n.SNI)
		if cfg.SkipCertVerify {
			b.WriteString("    skip-cert-verify: true\n")
		}
	case "tuic":
		fmt.Fprintf(b, "    type: tuic\n    server: %s\n    port: %d\n    uuid: %s\n    password: %s\n    congestion-controller: %s\n",
			n.Server, n.Port, n.UUID, n.Password, n.Congestion)
		b.WriteString("    alpn:\n      - h3\n")
		if n.SNI != "" {
			fmt.Fprintf(b, "    sni: %s\n", n.SNI)
		}
		if cfg.SkipCertVerify {
			b.WriteString("    skip-cert-verify: true\n")
		}
		b.WriteString("    udp-relay-mode: native\n")
	case "anytls":
		fmt.Fprintf(b, "    type: anytls\n    server: %s\n    port: %d\n    password: %s\n",
			n.Server, n.Port, n.Password)
		if n.SNI != "" {
			fmt.Fprintf(b, "    sni: %s\n", n.SNI)
		}
		if cfg.SkipCertVerify {
			b.WriteString("    skip-cert-verify: true\n")
		}
	case "trojan":
		fmt.Fprintf(b, "    type: trojan\n    server: %s\n    port: %d\n    password: %s\n",
			n.Server, n.Port, n.Password)
		if n.SNI != "" {
			fmt.Fprintf(b, "    sni: %s\n", n.SNI)
		}
		if cfg.SkipCertVerify {
			b.WriteString("    skip-cert-verify: true\n")
		}
	}
	b.WriteString("\n")
}

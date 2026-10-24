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

// renderClash emits the Clash (Meta) YAML document. The layout mirrors
// what mainstream panels (and the previous panel) produce: header,
// fake-ip DNS with a fallback set chosen by settings, one select group
// named "Proxy", STUN policy rules, private/CN direct, MATCH last.
func renderClash(nodes []Node, cfg settings.View, host string) string {
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
	b.WriteString("proxy-groups:\n  - name: \"Proxy\"\n    type: select\n    proxies:\n")
	for _, n := range nodes {
		fmt.Fprintf(&b, "      - %s\n", n.Name)
	}
	b.WriteString("      - DIRECT\n\n")

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
	b.WriteString("  - GEOSITE,private,DIRECT\n  - GEOIP,private,DIRECT,no-resolve\n  - GEOSITE,cn,DIRECT\n  - GEOIP,CN,DIRECT\n  - MATCH,Proxy\n")
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
	case "shadowsocks":
		fmt.Fprintf(b, "    type: ss\n    server: %s\n    port: %d\n    cipher: %s\n    password: %s\n    udp: true\n",
			n.Server, n.Port, n.Method, n.Password)
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

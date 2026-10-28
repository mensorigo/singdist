package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// generate builds the full sing-box config from the agent's desired
// state. The skeleton mirrors the panel's shape (log, clash_api
// for health checks, direct outbound); inbounds come from the
// merged desired set, users from the push. TLS protocols embed the
// pushed certificate; the agent never sources a certificate itself.
func generate(desired []Inbound, publicHost string, cert *Cert, logPath, clashAddr string) (doc []byte, hash string, err error) {
	genInbounds := []any{}
	for _, ib := range desired {
		if !ib.Enabled {
			continue
		}
		gen, err := genInbound(ib, publicHost, cert)
		if err != nil {
			return nil, "", err
		}
		if gen != nil {
			genInbounds = append(genInbounds, gen)
		}
	}

	logBlock := map[string]any{"level": "info", "timestamp": true}
	if logPath != "" {
		logBlock["output"] = logPath
	}
	root := map[string]any{
		"log": logBlock,
		"experimental": map[string]any{
			"clash_api": map[string]any{"external_controller": clashAddr},
		},
		"inbounds": genInbounds,
		"outbounds": []any{
			map[string]any{"type": "direct", "tag": "direct"},
		},
	}
	doc, err = json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, "", err
	}
	doc = append(doc, '\n')
	sum := sha256.Sum256(doc)
	return doc, hex.EncodeToString(sum[:]), nil
}

func genInbound(ib Inbound, publicHost string, cert *Cert) (map[string]any, error) {
	base := map[string]any{
		"tag":         ib.Name,
		"listen":      "::",
		"listen_port": ib.Port,
	}
	tlsCert := func(serverName string, alpn []string) map[string]any {
		tls := map[string]any{
			"enabled":     true,
			"certificate": []string{cert.Certificate},
			"key":         cert.Key,
		}
		if serverName != "" {
			tls["server_name"] = serverName
		}
		if len(alpn) > 0 {
			tls["alpn"] = alpn
		}
		return tls
	}
	passwordUsers := func(password string) []any {
		users := []any{}
		for _, u := range ib.Users {
			users = append(users, map[string]any{"name": u.Name, "password": password})
		}
		return users
	}

	switch ib.Protocol {
	case ProtocolVLESSReality:
		var p realityParams
		_ = json.Unmarshal(ib.Params, &p)
		base["type"] = "vless"
		users := []any{}
		for _, u := range ib.Users {
			m := map[string]any{"name": u.Name, "uuid": u.UUID}
			if p.Flow != "" {
				m["flow"] = p.Flow
			}
			users = append(users, m)
		}
		base["users"] = users
		reality := map[string]any{"enabled": true, "private_key": p.PrivateKey}
		if p.SNI != "" {
			reality["handshake"] = map[string]any{"server": p.SNI, "server_port": 443}
		}
		if p.ShortID != "" {
			reality["short_id"] = []string{p.ShortID}
		}
		base["tls"] = map[string]any{
			"enabled":     true,
			"server_name": p.SNI,
			"reality":     reality,
		}
	case ProtocolHysteria2:
		var p hy2Params
		_ = json.Unmarshal(ib.Params, &p)
		base["type"] = "hysteria2"
		base["users"] = passwordUsers(p.Password)
		if p.UpMbps > 0 {
			base["up_mbps"] = p.UpMbps
		}
		if p.DownMbps > 0 {
			base["down_mbps"] = p.DownMbps
		}
		if p.Obfs == "salamander" && p.ObfsPassword != "" {
			base["obfs"] = map[string]any{"type": "salamander", "password": p.ObfsPassword}
		}
		base["tls"] = tlsCert(publicHost, []string{"h3"})
	case ProtocolTUIC:
		var p tuicParams
		_ = json.Unmarshal(ib.Params, &p)
		base["type"] = "tuic"
		users := []any{}
		for _, u := range ib.Users {
			users = append(users, map[string]any{"name": u.Name, "uuid": u.UUID, "password": p.Password})
		}
		base["users"] = users
		congestion := p.CongestionControl
		if congestion == "" {
			congestion = "bbr"
		}
		base["congestion_control"] = congestion
		base["tls"] = tlsCert(p.SNI, []string{"h3"})
	case ProtocolShadowsocks:
		var p ssParams
		_ = json.Unmarshal(ib.Params, &p)
		base["type"] = "shadowsocks"
		base["method"] = p.Method
		base["password"] = p.Password
	case ProtocolAnyTLS:
		var p passwordSNIParams
		_ = json.Unmarshal(ib.Params, &p)
		base["type"] = "anytls"
		base["users"] = passwordUsers(p.Password)
		base["tls"] = tlsCert(p.SNI, nil)
	case ProtocolTrojan:
		var p passwordSNIParams
		_ = json.Unmarshal(ib.Params, &p)
		base["type"] = "trojan"
		base["users"] = passwordUsers(p.Password)
		base["tls"] = tlsCert(p.SNI, nil)
	default:
		// Validation rejects unknown protocols before generation,
		// so this is unreachable in the push path; skip defensively
		// like the panel does rather than failing the whole doc.
		return nil, nil
	}
	return base, nil
}

// parseDoc parses a core config doc into a generic map.
func parseDoc(b []byte) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// marshalDoc renders a parsed doc in the same indented form as
// generate, so hashes are comparable across paths.
func marshalDoc(m map[string]any) ([]byte, string) {
	b, _ := json.MarshalIndent(m, "", "  ")
	b = append(b, '\n')
	sum := sha256.Sum256(b)
	return b, hex.EncodeToString(sum[:])
}

// semanticEqual compares two parsed docs by their canonical bytes.
func semanticEqual(a, b map[string]any) bool {
	_, ha := marshalDoc(a)
	_, hb := marshalDoc(b)
	return ha == hb
}

// docInboundTags returns the tags of a parsed doc's inbounds.
func docInboundTags(m map[string]any) map[string]bool {
	tags := map[string]bool{}
	raw, _ := m["inbounds"].([]any)
	for _, item := range raw {
		if ib, ok := item.(map[string]any); ok {
			if tag, ok := ib["tag"].(string); ok && tag != "" {
				tags[tag] = true
			}
		}
	}
	return tags
}

// docInbounds returns the parsed doc's inbounds as generic maps.
func docInbounds(m map[string]any) []map[string]any {
	raw, _ := m["inbounds"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if ib, ok := item.(map[string]any); ok {
			out = append(out, ib)
		}
	}
	return out
}

package coresync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"singbox-ui/server/internal/store"
)

// activeSubscriptions returns the subscriptions that currently grant
// access (derived status == active). The status rules mirror the subs
// module; the copy keeps the modules independent.
func activeSubscriptions(subs []store.SubscriptionRow, now int64) []store.SubscriptionRow {
	out := []store.SubscriptionRow{}
	for _, r := range subs {
		switch {
		case !r.Enabled:
		case r.ExpireAt > 0 && r.ExpireAt < now:
		case r.LimitBytes > 0 && r.UsedUp+r.UsedDown >= r.LimitBytes:
		default:
			out = append(out, r)
		}
	}
	return out
}

// Local mirrors of the inbound params JSON (the params blob is the
// contract with the inbounds module).
type realityParams struct {
	SNI        string `json:"sni"`
	PublicKey  string `json:"publicKey"`
	PrivateKey string `json:"privateKey"`
	ShortID    string `json:"shortId"`
	Flow       string `json:"flow"`
}

type hy2Params struct {
	UpMbps       int    `json:"upMbps"`
	DownMbps     int    `json:"downMbps"`
	Password     string `json:"password"`
	Obfs         string `json:"obfs"`
	ObfsPassword string `json:"obfsPassword"`
}

type tuicParams struct {
	Password          string `json:"password"`
	CongestionControl string `json:"congestionControl"`
	SNI               string `json:"sni"`
}

type ssParams struct {
	Method   string `json:"method"`
	Password string `json:"password"`
}

type passwordSNIParams struct {
	Password string `json:"password"`
	SNI      string `json:"sni"`
}

// Generate builds the full sing-box config from the panel's desired
// state. The skeleton mirrors the running config's shape (log,
// clash_api for the dashboard, direct outbound); inbounds come from
// enabled panel inbounds, users from active subscriptions.
func (s *Service) Generate(ctx context.Context) (doc []byte, hash string, err error) {
	inbounds, err := s.st.ListInbounds(ctx)
	if err != nil {
		return nil, "", err
	}
	subs, err := s.st.ListSubscriptions(ctx)
	if err != nil {
		return nil, "", err
	}
	templates, err := s.st.ListTemplates(ctx)
	if err != nil {
		return nil, "", err
	}
	cfg, err := s.settings.Get(ctx)
	if err != nil {
		return nil, "", err
	}

	// template id -> set of inbound ids
	tplInbounds := map[int64]map[int64]bool{}
	for _, t := range templates {
		var ids []int64
		_ = json.Unmarshal([]byte(t.InboundIDs), &ids)
		set := map[int64]bool{}
		for _, id := range ids {
			set[id] = true
		}
		tplInbounds[t.ID] = set
	}
	active := activeSubscriptions(subs, time.Now().Unix())
	subsFor := func(inboundID int64) []store.SubscriptionRow {
		out := []store.SubscriptionRow{}
		for _, sub := range active {
			if tplInbounds[sub.TemplateID][inboundID] {
				out = append(out, sub)
			}
		}
		return out
	}

	// The active panel certificate is embedded for TLS protocols, so
	// fetch it lazily only if such an inbound exists.
	var certPEM, keyPEM []byte
	needCert := false
	for _, ib := range inbounds {
		// Nodes-only inbounds need no local cert: their cert is
		// fetched by the node push, and a missing local cert must
		// not fail the local sync.
		if ib.Enabled && ib.RunLocal && (ib.Protocol == "hysteria2" || ib.Protocol == "tuic" || ib.Protocol == "anytls" || ib.Protocol == "trojan") {
			needCert = true
		}
	}
	if needCert {
		certPEM, keyPEM, err = s.certs.ActivePEM()
		if err != nil {
			return nil, "", fmt.Errorf("coresync: active certificate: %w", err)
		}
	}

	genInbounds := []any{}
	for _, ib := range inbounds {
		// Nodes-only inbounds are not generated locally: the local
		// core never listens on them.
		if !ib.Enabled || !ib.RunLocal {
			continue
		}
		gen, err := s.genInbound(ib, subsFor(ib.ID), cfg.PublicHost, certPEM, keyPEM)
		if err != nil {
			return nil, "", err
		}
		if gen != nil {
			genInbounds = append(genInbounds, gen)
		}
	}

	logBlock := map[string]any{"level": "info", "timestamp": true}
	if s.logPath != "" {
		// Panel-managed log file: from this sync on, the core writes
		// here and the panel's follower reads here. Official log.output
		// only — nothing the stock core does not provide.
		logBlock["output"] = s.logPath
	}
	root := map[string]any{
		"log": logBlock,
		"experimental": map[string]any{
			"clash_api": map[string]any{"external_controller": s.clashAddr},
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

// coreUserName is the sing-box core username for one subscription on
// one password-protocol inbound. sing-box rejects the same core
// username with two different passwords across inbounds, so a
// subscription that appears on both hysteria2 and trojan (different
// inbound passwords) used to make every local sync check-fail. The
// suffix makes each (subscription, inbound) pair a distinct core
// user while the client-facing subscription name and link stay
// unchanged; clients of password protocols send only the password,
// never this name. vless keeps the plain subscription name because
// its identity is the UUID, which is already identical everywhere.
// Trojan is the exception: it identifies users by password, so it
// gets a single user per inbound (see the trojan case below), not
// one per subscription; coreUserName for trojan is legacy only
// (classify still recognises old renamed trojan users on transition).
// The agent mirrors this rule in its own generate.go; keep them
// in sync. (Hyphenated names could theoretically collide across
// (sub, inbound) pairs, e.g. sub "a-b" + inbound "c" vs sub "a" +
// inbound "b-c"; subscription and inbound names are user-chosen,
// and no such pair exists in practice — the exact (sub, inbound)
// map is what classify and the online-IP counts use, never parsing.)
func coreUserName(subName, inboundName string) string {
	return subName + "-" + inboundName
}

func (s *Service) genInbound(ib store.InboundRow, subs []store.SubscriptionRow, publicHost string, certPEM, keyPEM []byte) (map[string]any, error) {
	base := map[string]any{
		"tag":         ib.Name,
		"listen":      "::",
		"listen_port": ib.Port,
	}
	tlsCert := func(serverName string, alpn []string) map[string]any {
		tls := map[string]any{
			"enabled":     true,
			"certificate": []string{string(certPEM)},
			"key":         string(keyPEM),
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
		for _, sub := range subs {
			users = append(users, map[string]any{"name": coreUserName(sub.Name, ib.Name), "password": password})
		}
		return users
	}

	switch ib.Protocol {
	case "vless-reality":
		var p realityParams
		_ = json.Unmarshal([]byte(ib.Params), &p)
		base["type"] = "vless"
		users := []any{}
		for _, sub := range subs {
			u := map[string]any{"name": sub.Name, "uuid": sub.UUID}
			if p.Flow != "" {
				u["flow"] = p.Flow
			}
			users = append(users, u)
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
	case "hysteria2":
		var p hy2Params
		_ = json.Unmarshal([]byte(ib.Params), &p)
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
	case "tuic":
		var p tuicParams
		_ = json.Unmarshal([]byte(ib.Params), &p)
		base["type"] = "tuic"
		users := []any{}
		for _, sub := range subs {
			users = append(users, map[string]any{"name": coreUserName(sub.Name, ib.Name), "uuid": sub.UUID, "password": p.Password})
		}
		base["users"] = users
		congestion := p.CongestionControl
		if congestion == "" {
			congestion = "bbr"
		}
		base["congestion_control"] = congestion
		base["tls"] = tlsCert(p.SNI, []string{"h3"})
	case "shadowsocks":
		var p ssParams
		_ = json.Unmarshal([]byte(ib.Params), &p)
		base["type"] = "shadowsocks"
		base["method"] = p.Method
		base["password"] = p.Password
	case "anytls":
		var p passwordSNIParams
		_ = json.Unmarshal([]byte(ib.Params), &p)
		base["type"] = "anytls"
		base["users"] = passwordUsers(p.Password)
		base["tls"] = tlsCert(p.SNI, nil)
	case "trojan":
		var p passwordSNIParams
		_ = json.Unmarshal([]byte(ib.Params), &p)
		base["type"] = "trojan"
		// Trojan identifies users by PASSWORD, not name: two
		// entries sharing the inbound password fail the core
		// check ("password used by 0"), so a trojan inbound
		// gets exactly one user, named after the inbound
		// itself, whenever at least one active subscription
		// uses it. Trojan clients send only the password, so
		// sharing one core user across subscriptions changes
		// nothing client-side; per-subscription distinction
		// for trojan was never real (shared password).
		if len(subs) > 0 {
			base["users"] = []any{map[string]any{"name": ib.Name, "password": p.Password}}
		} else {
			base["users"] = []any{}
		}
		base["tls"] = tlsCert(p.SNI, nil)
	default:
		return nil, nil
	}
	return base, nil
}

// hashBytes is the content hash used for change detection and the
// version archive.
func hashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

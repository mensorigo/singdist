package nodes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"singbox-ui/server/internal/certs"
	"singbox-ui/server/internal/store"
)

// activeSubscriptions mirrors the sync engine's derived-status rules; the
// copy keeps the modules independent.
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

// tlsProtocols are the protocols whose core config embeds the panel
// certificate (same set the sync engine uses).
func isTLSProtocol(protocol string) bool {
	switch protocol {
	case "hysteria2", "tuic", "anytls", "trojan":
		return true
	}
	return false
}

// BuildPush builds the agent push payload for a node from the panel's
// desired state: enabled inbounds assigned to the node, users from
// active subscriptions whose template includes each inbound (the same
// user derivation the local sync uses, so one subscription UUID works
// on every node), the panel's active certificate when a TLS inbound is
// present, and the node's public host. It also returns the canonical
// hash the poller compares against the last pushed hash.
func BuildPush(ctx context.Context, st *store.Store, certMgr *certs.Manager, node store.NodeRow) (PushPayload, string, error) {
	inbounds, err := st.ListInbounds(ctx)
	if err != nil {
		return PushPayload{}, "", err
	}
	subs, err := st.ListSubscriptions(ctx)
	if err != nil {
		return PushPayload{}, "", err
	}
	templates, err := st.ListTemplates(ctx)
	if err != nil {
		return PushPayload{}, "", err
	}
	assigned, err := st.NodeInboundAssignments(ctx, node.ID)
	if err != nil {
		return PushPayload{}, "", err
	}
	// inbound id -> per-node port override (0 = the inbound's own port).
	assignedSet := map[int64]bool{}
	assignedPort := map[int64]int{}
	for _, a := range assigned {
		assignedSet[a.InboundID] = true
		assignedPort[a.InboundID] = a.Port
	}

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
		sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
		return out
	}

	pushed := []PushInbound{}
	needCert := false
	for _, ib := range inbounds {
		if !ib.Enabled || !assignedSet[ib.ID] {
			continue
		}
		users := []PushUser{}
		for _, sub := range subsFor(ib.ID) {
			switch ib.Protocol {
			case "vless-reality", "tuic":
				users = append(users, PushUser{Name: sub.Name, UUID: sub.UUID})
			case "hysteria2", "anytls", "trojan":
				users = append(users, PushUser{Name: sub.Name})
			case "shadowsocks":
				// No per-sub users; traffic is unattributable.
			default:
				// Unknown protocol: still pushed (the agent
				// rejects it loudly); users mirror vless.
				users = append(users, PushUser{Name: sub.Name, UUID: sub.UUID})
			}
		}
		if isTLSProtocol(ib.Protocol) {
			needCert = true
		}
		port := ib.Port
		if p, ok := assignedPort[ib.ID]; ok && p > 0 {
			port = p
		}
		pushed = append(pushed, PushInbound{
			Name:     ib.Name,
			Protocol: ib.Protocol,
			Port:     port,
			Enabled:  true,
			Params:   json.RawMessage(ib.Params),
			Users:    users,
		})
	}
	// Canonical order so the hash is stable regardless of DB order.
	sort.Slice(pushed, func(i, j int) bool {
		if pushed[i].Name != pushed[j].Name {
			return pushed[i].Name < pushed[j].Name
		}
		if pushed[i].Protocol != pushed[j].Protocol {
			return pushed[i].Protocol < pushed[j].Protocol
		}
		return pushed[i].Port < pushed[j].Port
	})

	payload := PushPayload{
		Inbounds:   pushed,
		PublicHost: PublicHostOf(node.PublicHost, node.Address),
	}
	if needCert {
		certPEM, keyPEM, err := certMgr.ActivePEM()
		if err != nil {
			return PushPayload{}, "", fmt.Errorf("nodes: active certificate: %w", err)
		}
		payload.Cert = &PushCert{Certificate: string(certPEM), Key: string(keyPEM)}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return PushPayload{}, "", err
	}
	sum := sha256.Sum256(raw)
	return payload, hex.EncodeToString(sum[:]), nil
}

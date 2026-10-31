package nodes

import (
	"context"
	"net"
	"strings"
	"time"

	"singbox-ui/server/internal/store"
)

// CreateRequest is the POST /nodes body. PairingCode is a temporary
// pairing field: it is never stored and never audited — when set,
// it is exchanged for a long-term token via the agent first, and
// that token becomes the node's token. Token is then not required.
type CreateRequest struct {
	Name             string   `json:"name"`
	Address          string   `json:"address"`
	Token            string   `json:"token"`
	PairingCode      string   `json:"pairing_code"`
	PublicHost       string   `json:"publicHost"`
	TLSMode          string   `json:"tlsMode"`
	UsageRatio       *float64 `json:"usageRatio"`
	AddToAllInbounds bool     `json:"addToAllInbounds"`
}

// pairForAddress exchanges a pairing code for a long-term token at
// the agent. An old-agent 404 becomes the upgrade message; a
// generic pairing failure becomes a validation error carrying the
// agent's message; anything else (unreachable, bad address) is an
// upstream error.
func (s *Service) Create(ctx context.Context, req CreateRequest) (NodeView, error) {
	// An empty 备注 is filled from the node's hostname (creation
	// probe) and falls back to the address host part; it never fails
	// creation on its own. A non-empty 备注 keeps the old 1-64
	// validation.
	name := strings.TrimSpace(req.Name)
	nameWasEmpty := name == ""
	if !nameWasEmpty {
		var err error
		name, err = validateName(name)
		if err != nil {
			return NodeView{}, err
		}
	}
	address, err := validateAddress(req.Address)
	if err != nil {
		return NodeView{}, err
	}
	// Pairing wins over a manual token: the code is exchanged first
	// (no old-token probe is involved — there is no old token on a
	// create), and the returned token becomes the node's token.
	// Without a code, the manual token rules are unchanged.
	var token string
	paired := false
	if code := strings.TrimSpace(req.PairingCode); code != "" {
		t, err := s.pairForAddress(ctx, address, code)
		if err != nil {
			return NodeView{}, err
		}
		token, paired = t, true
	} else {
		t, err := validateToken(req.Token)
		if err != nil {
			return NodeView{}, err
		}
		token = t
	}
	tlsMode, err := validateTLSMode(req.TLSMode)
	if err != nil {
		return NodeView{}, err
	}
	ratio, err := validateRatio(req.UsageRatio)
	if err != nil {
		return NodeView{}, err
	}
	// Derive an empty 备注 before insert (the row needs a name for
	// the UNIQUE check). The probe is capped at 3s like the
	// post-insert probe below, and its result is reused for the view
	// so an empty-name creation probes at most once. A failed probe,
	// or an old agent without the hostname field, falls back to the
	// address host part (address validation guarantees a non-empty
	// host, so derivation always yields a name) and still creates
	// the node — only the warning is set.
	var probeStatus *AgentStatus
	probeOK := false
	if nameWasEmpty {
		probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		st, perr := s.client.Status(probeCtx, address, token)
		cancel()
		if perr == nil && st != nil {
			probeStatus, probeOK = st, true
			if h := truncateRunes(strings.TrimSpace(st.Hostname), 64); h != "" {
				name = h
			}
		}
		if name == "" {
			if _, hostport, aerr := ParseAddress(address); aerr == nil {
				if host, _, herr := net.SplitHostPort(hostport); herr == nil {
					name = truncateRunes(host, 64)
				}
			}
		}
	}
	existing, err := s.st.ListNodes(ctx)
	if err != nil {
		return NodeView{}, err
	}
	for _, e := range existing {
		if e.Name == name {
			return NodeView{}, errConflict("node name already exists")
		}
	}
	now := time.Now().Unix()
	id, err := s.st.CreateNode(ctx, store.NodeRow{
		Name: name, Address: address, Token: token,
		PublicHost: strings.TrimSpace(req.PublicHost),
		TLSMode:    tlsMode, UsageRatio: ratio,
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			return NodeView{}, errConflict("node name already exists")
		}
		return NodeView{}, err
	}
	if req.AddToAllInbounds {
		if err := s.st.AddNodeToAllInbounds(ctx, id); err != nil {
			return NodeView{}, err
		}
	}
	r, err := s.st.GetNode(ctx, id)
	if err != nil {
		return NodeView{}, err
	}
	v := toView(*r, now)
	// A pairing just proved the agent reachable with the fresh
	// token, so a flaky follow-up probe must not warn (and must
	// not suppress the frontend's post-create import scan): the
	// node is online by construction of the pairing itself.
	if paired {
		v.Online = true
		if probeOK {
			v.LastInboundCount = len(probeStatus.Inbounds)
			v.LastVersion = probeStatus.Version
		}
	} else if nameWasEmpty {
		if probeOK {
			v.Online = true
			v.LastInboundCount = len(probeStatus.Inbounds)
			v.LastVersion = probeStatus.Version
		} else {
			// Same warning as the post-insert probe below, without
			// probing a second time.
			v.Warning = "节点暂时无法联系，创建已保存，后台会继续探测"
		}
	} else {
		// Creation-time reachability probe (capped at 3s so a dead
		// node does not stall creation): a failure only warns — the
		// node is still registered and the background poller keeps
		// probing it.
		probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		if st, err := s.client.Status(probeCtx, r.Address, r.Token); err != nil {
			v.Warning = "节点暂时无法联系，创建已保存，后台会继续探测"
		} else {
			v.Online = true
			v.LastInboundCount = len(st.Inbounds)
			v.LastVersion = st.Version
		}
	}
	return v, nil
}

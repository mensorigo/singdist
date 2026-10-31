package nodes

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

)

// UpdateRequest is the PATCH /nodes/:id body. Only non-nil fields are
// applied; an empty token means "keep the existing one". A set
// PairingCode re-pairs the node: it is exchanged for a fresh token
// that replaces the stored one, and the Token field is ignored.
// UpdateRequest is the PATCH /nodes/:id body. Only non-nil fields are
// applied; an empty token means "keep the existing one". A set
// PairingCode re-pairs the node: it is exchanged for a fresh token
// that replaces the stored one, and the Token field is ignored.
type UpdateRequest struct {
	Name        *string  `json:"name"`
	Address     *string  `json:"address"`
	Token       *string  `json:"token"`
	PairingCode *string  `json:"pairing_code"`
	PublicHost  *string  `json:"publicHost"`
	TLSMode     *string  `json:"tlsMode"`
	UsageRatio  *float64 `json:"usageRatio"`
	Paused      *bool    `json:"paused"`
}

func (s *Service) Update(ctx context.Context, id int64, req UpdateRequest) (NodeView, error) {
	r, err := s.st.GetNode(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return NodeView{}, errNotFound("node")
		}
		return NodeView{}, err
	}
	// Re-pairing first: the code is exchanged against the effective
	// address (the new address when one is given, else the stored
	// one) before any old-token use. The returned token replaces
	// the stored one below, and the manual Token field is ignored.
	var pairedToken string
	if req.PairingCode != nil && strings.TrimSpace(*req.PairingCode) != "" {
		effAddr := r.Address
		if req.Address != nil {
			a, err := validateAddress(*req.Address)
			if err != nil {
				return NodeView{}, err
			}
			effAddr = a
		}
		t, err := s.pairForAddress(ctx, effAddr, strings.TrimSpace(*req.PairingCode))
		if err != nil {
			return NodeView{}, err
		}
		pairedToken = t
	}
	addrChanged, tokenChanged := false, false
	if req.Name != nil {
		name, err := validateName(*req.Name)
		if err != nil {
			return NodeView{}, err
		}
		if name != r.Name {
			existing, err := s.st.ListNodes(ctx)
			if err != nil {
				return NodeView{}, err
			}
			for _, e := range existing {
				if e.ID != id && e.Name == name {
					return NodeView{}, errConflict("node name already exists")
				}
			}
		}
		r.Name = name
	}
	if req.Address != nil {
		address, err := validateAddress(*req.Address)
		if err != nil {
			return NodeView{}, err
		}
		if address != r.Address {
			addrChanged = true
		}
		r.Address = address
	}
	if pairedToken != "" {
		if pairedToken != r.Token {
			tokenChanged = true
		}
		r.Token = pairedToken
	} else if req.Token != nil {
		if token := strings.TrimSpace(*req.Token); token != "" {
			token, err := validateToken(token)
			if err != nil {
				return NodeView{}, err
			}
			if token != r.Token {
				tokenChanged = true
			}
			r.Token = token
		}
	}
	if req.PublicHost != nil {
		r.PublicHost = strings.TrimSpace(*req.PublicHost)
	}
	if req.TLSMode != nil {
		tlsMode, err := validateTLSMode(*req.TLSMode)
		if err != nil {
			return NodeView{}, err
		}
		r.TLSMode = tlsMode
	}
	if req.UsageRatio != nil {
		ratio, err := validateRatio(req.UsageRatio)
		if err != nil {
			return NodeView{}, err
		}
		r.UsageRatio = ratio
	}
	// Pausing never resets push state: the hash is kept, so the
	// poller pushes the empty payload on pause (its hash differs)
	// and the normal payload again on enable (when it differs from
	// the paused hash). Flipping the flag only clears a stale push
	// backoff, so the stop/restore push is not delayed by it.
	pauseChanged := req.Paused != nil && *req.Paused != r.Paused
	if req.Paused != nil {
		r.Paused = *req.Paused
	}
	now := time.Now().Unix()
	r.UpdatedAt = now
	if err := s.st.UpdateNode(ctx, *r); err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			return NodeView{}, errConflict("node name already exists")
		}
		return NodeView{}, err
	}
	if pauseChanged {
		if err := s.st.ClearNodeNextRetry(ctx, id); err != nil {
			return NodeView{}, err
		}
	}
	// A node that moved to a different agent (or got a new token) must
	// be pushed fresh: the stored hash belongs to the old agent and
	// must not suppress a push to the new one.
	if addrChanged || tokenChanged {
		if err := s.st.ResetNodePushState(ctx, id, now); err != nil {
			return NodeView{}, err
		}
	}
	fresh, err := s.st.GetNode(ctx, id)
	if err != nil {
		return NodeView{}, err
	}
	return toView(*fresh, now), nil
}

// Package render generates subscription content (Clash YAML / sing-box
// JSON) from a subscription, its template's inbounds and the global
// settings, and serves it on the public /sub endpoint. It is a pure
// read path: nothing here writes to the store or the core.
package render

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"time"

	"singbox-ui/server/internal/settings"
	"singbox-ui/server/internal/store"
)

// Error mirrors the other modules' typed API failure.
type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }
func errValidation(msg string) *Error {
	return &Error{Status: 400, Code: "validation", Message: msg}
}
func errNotFound(what string) *Error {
	return &Error{Status: 404, Code: what + "_not_found", Message: what + " not found"}
}
func errUnavailable(msg string) *Error {
	return &Error{Status: 403, Code: "subscription_unavailable", Message: msg}
}

type Service struct {
	st       *store.Store
	settings *settings.Service
}

func NewService(st *store.Store, settingsSvc *settings.Service) *Service {
	return &Service{st: st, settings: settingsSvc}
}

// --- node model ---------------------------------------------------------------

// Node is one client proxy entry derived from one inbound. Credential
// semantics mirror the domain model: vless/tuic nodes carry the
// SUBSCRIPTION's uuid; password protocols carry the inbound's shared
// password (per-subscription users for them arrive with the sync
// engine).
type Result struct {
	Body        string
	ContentType string
	Format      string // clash | singbox

	SubName    string
	ShowInfo   bool
	UsedUp     int64
	UsedDown   int64
	LimitBytes int64
	ExpireAt   int64
	UpdateHrs  int
}

// Render builds the subscription document. format "" resolves to the
// settings default; reqHost supplies the node server address when no
// public host is configured.
// Render builds the subscription document. format "" resolves to the
// settings default; reqHost supplies the node server address when no
// public host is configured.
func (s *Service) Render(ctx context.Context, sub store.SubscriptionRow, format, reqHost string) (Result, error) {
	cfg, err := s.settings.Get(ctx)
	if err != nil {
		return Result{}, err
	}
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "sing-box" {
		format = "singbox"
	}
	if format == "" {
		format = cfg.LinkFormat
	}
	if format != "clash" && format != "singbox" {
		return Result{}, errValidation("format must be clash or singbox")
	}

	host := cfg.PublicHost
	if host == "" {
		host = stripPort(reqHost)
	}

	nodes, err := s.buildAllNodes(ctx, sub, host)
	if err != nil {
		return Result{}, err
	}

	// Proxy groups for Clash: effective template groups (defaults when
	// the template stores none), each mapped to the generated node
	// names whose inbound is in the group. A group with no generated
	// nodes is skipped, so a group over a disabled/paused inbound
	// simply disappears instead of rendering an empty group.
	var clashGroups []ClashGroup
	if tpl, terr := s.st.GetTemplate(ctx, sub.TemplateID); terr == nil {
		var tplIDs []int64
		_ = json.Unmarshal([]byte(tpl.InboundIDs), &tplIDs)
		for _, g := range store.EffectiveProxyGroups(tpl.Groups, tplIDs) {
			if !g.Enabled {
				continue
			}
			want := map[int64]bool{}
			for _, id := range g.InboundIDs {
				want[id] = true
			}
			names := []string{}
			seenNames := map[string]bool{}
			for _, n := range nodes {
				if want[n.InboundID] && !seenNames[n.Name] {
					seenNames[n.Name] = true
					names = append(names, n.Name)
				}
			}
			if len(names) == 0 {
				continue
			}
			clashGroups = append(clashGroups, ClashGroup{
				Name: g.Name, Type: g.Type, Proxies: names,
				URL: g.URL, Interval: g.Interval, Tolerance: g.Tolerance,
			})
		}
	}
	if len(clashGroups) == 0 {
		// Fallback: never render a subscription without a group.
		all := []string{}
		for _, n := range nodes {
			all = append(all, n.Name)
		}
		if len(all) > 0 {
			clashGroups = []ClashGroup{{Name: "Proxy", Type: "select", Proxies: all}}
		}
	}

	res := Result{
		Format:     format,
		SubName:    sub.Name,
		ShowInfo:   cfg.ShowUserinfo,
		UsedUp:     sub.UsedUp,
		UsedDown:   sub.UsedDown,
		LimitBytes: sub.LimitBytes,
		ExpireAt:   sub.ExpireAt,
		UpdateHrs:  cfg.UpdateIntervalHrs,
	}
	switch format {
	case "clash":
		res.Body = renderClash(nodes, clashGroups, cfg, host)
		res.ContentType = "text/yaml; charset=utf-8"
	case "singbox":
		body, err := renderSingbox(nodes, cfg, host)
		if err != nil {
			return Result{}, err
		}
		res.Body = body
		res.ContentType = "application/json; charset=utf-8"
	}
	return res, nil
}

// RenderByToken is the public /sub path: it enforces the derived
// subscription status before rendering.
// RenderByToken is the public /sub path: it enforces the derived
// subscription status before rendering.
func (s *Service) RenderByToken(ctx context.Context, token, format, reqHost string) (Result, error) {
	sub, err := s.st.GetSubscriptionByToken(ctx, token)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Result{}, errNotFound("subscription")
		}
		return Result{}, err
	}
	switch statusOf(*sub, time.Now().Unix()) {
	case "active":
		return s.Render(ctx, *sub, format, reqHost)
	case "disabled":
		return Result{}, errUnavailable("subscription is disabled")
	case "expired":
		return Result{}, errUnavailable("subscription has expired")
	default:
		return Result{}, errUnavailable("subscription is over quota")
	}
}

// RenderByID is the admin preview path: it renders regardless of the
// subscription's status.
// RenderByID is the admin preview path: it renders regardless of the
// subscription's status.
func (s *Service) RenderByID(ctx context.Context, id int64, format, reqHost string) (Result, error) {
	sub, err := s.st.GetSubscription(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Result{}, errNotFound("subscription")
		}
		return Result{}, err
	}
	return s.Render(ctx, *sub, format, reqHost)
}
func stripPort(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}
func isIP(host string) bool {
	return net.ParseIP(host) != nil
}

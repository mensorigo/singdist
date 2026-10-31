package traffic

import (
	"context"
	"encoding/json"
	"net/http"

	"singbox-ui/server/internal/nodes"
	"singbox-ui/server/internal/store"

	"github.com/gin-gonic/gin"
)

// Summary is the subscription page's traffic view: per-subscription
// totals with a per-inbound breakdown, plus per-inbound totals.
type Summary struct {
	Local          Snapshot              `json:"local"`
	Nodes          []NodeTraffic         `json:"nodes"`
	BySubscription []SubscriptionTraffic `json:"bySubscription"`
	ByInbound      []InboundTraffic      `json:"byInbound"`
	UnassignedUp   int64                 `json:"unassignedUp"`
	UnassignedDown int64                 `json:"unassignedDown"`
	// UnassignedByInbound splits the unassigned totals per
	// inbound (and node), so a caller can see where the gap is.
	UnassignedByInbound []InboundTraffic `json:"unassignedByInbound,omitempty"`
}

type NodeTraffic struct {
	NodeID   int64    `json:"nodeId"`
	NodeName string   `json:"nodeName"`
	Traffic  Snapshot `json:"traffic"`
}

type SubscriptionTraffic struct {
	Name string `json:"name"`
	// Up/Down are the bytes actually attributed to this
	// subscription's core users.
	Up   int64 `json:"up"`
	Down int64 `json:"down"`
	// UnassignedUp/Down are unassigned bytes from inbounds that
	// exactly one enabled subscription uses: no other
	// subscription could have produced them, so they are the
	// only unassigned bytes ever allocated to a subscription.
	// EstimatedUp/Down = Up/Down + UnassignedUp/Down. Shared-
	// inbound unassigned bytes are never allocated.
	UnassignedUp   int64            `json:"unassignedUp"`
	UnassignedDown int64            `json:"unassignedDown"`
	EstimatedUp    int64            `json:"estimatedUp"`
	EstimatedDown  int64            `json:"estimatedDown"`
	ByInbound      []InboundTraffic `json:"byInbound"`
}

type InboundTraffic struct {
	Inbound string `json:"inbound"`
	Node    string `json:"node"` // "" = local core, else node name
	Up      int64  `json:"up"`
	Down    int64  `json:"down"`
}

// coreUserToSubscription maps a logged core username back to a
// subscription name for one inbound. vless keeps the plain name;
// password protocols use name+"-"+inbound (trojan included, since
// each subscription now has its own derived password and user).
func coreUserToSubscription(user, inbound, protocol string, subs []store.SubscriptionRow) string {
	if user == "" {
		return ""
	}
	for _, s := range subs {
		if protocol == "vless-reality" && user == s.Name {
			return s.Name
		}
		if user == s.Name+"-"+inbound {
			return s.Name
		}
	}
	return ""
}

type SummaryService struct {
	st    *store.Store
	nodes *nodes.Service
	local *Collector
}

func NewSummaryService(st *store.Store, nodesSvc *nodes.Service, local *Collector) *SummaryService {
	return &SummaryService{st: st, nodes: nodesSvc, local: local}
}

func (s *SummaryService) Summary(ctx context.Context) (Summary, error) {
	var out Summary
	subs, err := s.st.ListSubscriptions(ctx)
	if err != nil {
		return out, err
	}
	inbounds, err := s.st.ListInbounds(ctx)
	if err != nil {
		return out, err
	}
	protoByName := map[string]string{}
	for _, ib := range inbounds {
		protoByName[ib.Name] = ib.Protocol
	}
	if s.local != nil {
		out.Local = s.local.Snapshot()
	}
	nodeViews, _ := s.nodes.List(ctx)
	type subKey struct{ name, inbound, node string }
	subTotals := map[string]*SubscriptionTraffic{}
	subByInbound := map[subKey]*InboundTraffic{}
	inbTotals := map[string]*InboundTraffic{}
	addUser := func(nodeName, inbound, user string, up, down int64) {
		proto := protoByName[inbound]
		subName := coreUserToSubscription(user, inbound, proto, subs)
		if subName == "" {
			return
		}
		st, ok := subTotals[subName]
		if !ok {
			st = &SubscriptionTraffic{Name: subName}
			subTotals[subName] = st
		}
		st.Up += up
		st.Down += down
		k := subKey{subName, inbound, nodeName}
		it, ok := subByInbound[k]
		if !ok {
			it = &InboundTraffic{Inbound: inbound, Node: nodeName}
			subByInbound[k] = it
		}
		it.Up += up
		it.Down += down
	}
	addInbound := func(nodeName, inbound string, up, down int64) {
		k := inbound + "\x00" + nodeName
		it, ok := inbTotals[k]
		if !ok {
			it = &InboundTraffic{Inbound: inbound, Node: nodeName}
			inbTotals[k] = it
		}
		it.Up += up
		it.Down += down
	}
	// Local core.
	for _, e := range out.Local.ByUser {
		addUser("", e.Inbound, e.User, e.Up, e.Down)
	}
	for _, e := range out.Local.ByInbound {
		addInbound("", e.Inbound, e.Up, e.Down)
	}
	out.UnassignedUp += out.Local.UnassignedUp
	out.UnassignedDown += out.Local.UnassignedDown
	// Nodes.
	for _, nv := range nodeViews {
		if nv.AgentTraffic == nil {
			continue
		}
		nt := NodeTraffic{NodeID: nv.ID, NodeName: nv.Name}
		nt.Traffic.Up = nv.AgentTraffic.Up
		nt.Traffic.Down = nv.AgentTraffic.Down
		nt.Traffic.UnassignedUp = nv.AgentTraffic.UnassignedUp
		nt.Traffic.UnassignedDown = nv.AgentTraffic.UnassignedDown
		for _, e := range nv.AgentTraffic.UnassignedByInbound {
			nt.Traffic.UnassignedByInbound = append(nt.Traffic.UnassignedByInbound, Entry(e))
		}
		for _, e := range nv.AgentTraffic.ByInbound {
			nt.Traffic.ByInbound = append(nt.Traffic.ByInbound, Entry(e))
		}
		for _, e := range nv.AgentTraffic.ByUser {
			nt.Traffic.ByUser = append(nt.Traffic.ByUser, Entry(e))
		}
		out.Nodes = append(out.Nodes, nt)
		for _, e := range nv.AgentTraffic.ByUser {
			addUser(nv.Name, e.Inbound, e.User, e.Up, e.Down)
		}
		for _, e := range nv.AgentTraffic.ByInbound {
			addInbound(nv.Name, e.Inbound, e.Up, e.Down)
		}
		out.UnassignedUp += nv.AgentTraffic.UnassignedUp
		out.UnassignedDown += nv.AgentTraffic.UnassignedDown
	}
	// Sole-subscriber allocation: an inbound used by exactly one
	// enabled subscription can only have produced that
	// subscription's unassigned bytes (plus legacy passthrough
	// users, which are KB-scale and were deleted as subscriptions).
	// Shared-inbound unassigned bytes stay unallocated.
	{
		idToName := map[int64]string{}
		for _, ib := range inbounds {
			idToName[ib.ID] = ib.Name
		}
		usersOf := map[string]map[string]bool{}
		if templates, err := s.st.ListTemplates(ctx); err == nil {
			tplByID := map[int64]store.TemplateRow{}
			for _, tpl := range templates {
				tplByID[tpl.ID] = tpl
			}
			for _, sub := range subs {
				if !sub.Enabled {
					continue
				}
				tpl, ok := tplByID[sub.TemplateID]
				if !ok {
					continue
				}
				var ids []int64
				if json.Unmarshal([]byte(tpl.InboundIDs), &ids) != nil {
					continue
				}
				for _, id := range ids {
					name, ok := idToName[id]
					if !ok {
						continue
					}
					if usersOf[name] == nil {
						usersOf[name] = map[string]bool{}
					}
					usersOf[name][sub.Name] = true
				}
			}
		}
		sole := map[string]string{}
		for name, set := range usersOf {
			if len(set) == 1 {
				for subName := range set {
					sole[name] = subName
				}
			}
		}
		addUnassigned := func(nodeName, inbound string, up, down int64) {
			out.UnassignedByInbound = append(out.UnassignedByInbound, InboundTraffic{Inbound: inbound, Node: nodeName, Up: up, Down: down})
			subName, ok := sole[inbound]
			if !ok {
				return
			}
			st, ok := subTotals[subName]
			if !ok {
				st = &SubscriptionTraffic{Name: subName}
				subTotals[subName] = st
			}
			st.UnassignedUp += up
			st.UnassignedDown += down
		}
		for _, e := range out.Local.UnassignedByInbound {
			addUnassigned("", e.Inbound, e.Up, e.Down)
		}
		for _, nt := range out.Nodes {
			for _, e := range nt.Traffic.UnassignedByInbound {
				addUnassigned(nt.NodeName, e.Inbound, e.Up, e.Down)
			}
		}
	}
	for _, st := range subTotals {
		st.EstimatedUp = st.Up + st.UnassignedUp
		st.EstimatedDown = st.Down + st.UnassignedDown
		for k, it := range subByInbound {
			if k.name == st.Name {
				st.ByInbound = append(st.ByInbound, *it)
			}
		}
		out.BySubscription = append(out.BySubscription, *st)
	}
	for _, it := range inbTotals {
		out.ByInbound = append(out.ByInbound, *it)
	}
	return out, nil
}

// SyncUsage writes the collector's per-subscription totals back to
// the subscriptions table, so the subscription page's quota meter
// and the client's Subscription-Userinfo header show the same
// traffic the collector attributes (local + node, per core user).
// It writes the estimated total: attributed bytes plus
// sole-subscriber unassigned bytes (see Summary). Shared-inbound
// unassigned bytes are never forced onto a subscription.
// Subscriptions with no attributed or sole-subscriber traffic are
// left untouched, and only changed totals are written, so an idle
// ticker does not churn the table.
func (s *SummaryService) SyncUsage(ctx context.Context) (int, error) {
	sum, err := s.Summary(ctx)
	if err != nil {
		return 0, err
	}
	subs, err := s.st.ListSubscriptions(ctx)
	if err != nil {
		return 0, err
	}
	byName := map[string]SubscriptionTraffic{}
	for _, st := range sum.BySubscription {
		byName[st.Name] = st
	}
	updated := 0
	for _, sub := range subs {
		tot, ok := byName[sub.Name]
		if !ok {
			continue
		}
		if tot.EstimatedUp == sub.UsedUp && tot.EstimatedDown == sub.UsedDown {
			continue
		}
		if err := s.st.UpdateSubscriptionUsage(ctx, sub.ID, tot.EstimatedUp, tot.EstimatedDown); err != nil {
			return updated, err
		}
		updated++
	}
	return updated, nil
}

type Handler struct {
	svc *SummaryService
}

func NewHandler(svc *SummaryService) *Handler { return &Handler{svc: svc} }

func (h *Handler) RegisterProtected(r gin.IRoutes) {
	r.GET("/traffic/summary", func(c *gin.Context) {
		sum, err := h.svc.Summary(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, sum)
	})
}

package inbounds

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"singbox-ui/server/internal/store"
)

// List returns every stored inbound with its comparison status, plus any
// running inbounds the panel does not manage yet.
func (s *Service) List(ctx context.Context) (ListResult, error) {
	rows, err := s.st.ListInbounds(ctx)
	if err != nil {
		return ListResult{}, err
	}
	running, available := ReadRunning(s.configPath)
	// Node data for node-only inbounds only; a failure here must not
	// break the local list, so node inbounds fall back to unknown.
	now := nowUnix()
	nodesByID := map[int64]store.NodeRow{}
	asgsByInbound := map[int64][]store.InboundNodeRow{}
	if ns, err := s.st.ListNodes(ctx); err == nil {
		for _, n := range ns {
			nodesByID[n.ID] = n
		}
	}
	if asgs, err := s.st.AllInboundNodes(ctx); err == nil {
		for _, a := range asgs {
			asgsByInbound[a.InboundID] = append(asgsByInbound[a.InboundID], a)
		}
	}
	res := ListResult{Items: []View{}, RunningAvailable: available, Extras: []RunningInbound{}}
	matched := map[int]bool{}
	for _, row := range rows {
		v := View{
			ID: row.ID, Name: row.Name, Protocol: row.Protocol, Port: row.Port,
			Params: json.RawMessage(row.Params), Enabled: row.Enabled,
			RunLocal:  row.RunLocal,
			CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
			Status: "unknown",
		}
		if !row.RunLocal {
			// Nodes-only: the local core never runs it, so there
			// is no local state to compare against — and it must
			// not match a different local inbound that happens to
			// share its port. Its status comes from the assigned
			// node's push + agent snapshot instead, scoped to
			// this one inbound.
			v.Status = nodeInboundStatusAll(row, asgsByInbound[row.ID], nodesByID, now)
		} else if available {
			m := matchRunning(row, running)
			if m != nil {
				matched[m.Port] = true
				v.Status, v.Diffs = compareOf(row, m)
			} else {
				v.Status = "pending"
			}
		}
		if !row.Enabled {
			// A parked inbound's drift is not actionable.
			v.Status = "disabled"
			v.Diffs = nil
		}
		res.Items = append(res.Items, v)
	}
	if available {
		for _, r := range running {
			if !matched[r.Port] {
				res.Extras = append(res.Extras, r)
			}
		}
	}
	return res, nil
}

// ImportIfEmpty seeds the store from the running config when the panel
// has no inbounds yet, so the page starts from reality instead of empty.
// Returns how many inbounds were imported.
// ImportIfEmpty seeds the store from the running config when the panel
// has no inbounds yet, so the page starts from reality instead of empty.
// Returns how many inbounds were imported.
func (s *Service) ImportIfEmpty(ctx context.Context, log *slog.Logger) int {
	n, err := s.st.CountInbounds(ctx)
	if err != nil || n > 0 {
		return 0
	}
	running, ok := ReadRunning(s.configPath)
	if !ok {
		return 0
	}
	imported := 0
	for _, r := range running {
		name := r.Tag
		if name == "" {
			name = fmt.Sprintf("%s-%d", r.Type, r.Port)
		}
		protocol := ProtocolOf(r.Type)
		spec, ok := protocolSpecs[protocol]
		if !ok {
			continue
		}
		params := spec.importParams(&r)
		if _, err := s.st.CreateInbound(ctx, name, protocol, r.Port, params, true, nowUnix()); err != nil {
			log.Warn("inbounds: import from running config failed", "tag", r.Tag, "err", err)
			continue
		}
		imported++
	}
	if imported > 0 {
		log.Info("inbounds: imported from running config", "count", imported)
	}
	return imported
}

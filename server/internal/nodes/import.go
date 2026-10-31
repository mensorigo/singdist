package nodes

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"singbox-ui/server/internal/inbounds"
	"singbox-ui/server/internal/store"
)

// ImportCandidate is one node core inbound offered for import.
// Params are deliberately NOT included: the frontend only displays
// name/protocol/port/reason, and confirm re-fetches from the agent,
// so import secrets never travel to the browser for this flow.
type ImportCandidate struct {
	Name           string `json:"name"`
	Protocol       string `json:"protocol"` // panel protocol key
	Port           int    `json:"port"`
	DisabledReason string `json:"disabledReason,omitempty"` // "" = importable
}

// ImportScanResult is the GET import-scan payload.
type ImportScanResult struct {
	Candidates []ImportCandidate `json:"candidates"`
}

// ImportCreated is one inbound created by an import confirm.
type ImportCreated struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	Port     int    `json:"port"`
}

// ImportSkipped is one requested name that was not imported, with why.
type ImportSkipped struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// ImportResult is the POST import payload.
type ImportResult struct {
	Created []ImportCreated `json:"created"`
	Skipped []ImportSkipped `json:"skipped"`
}

// importCandidate is the internal scan row: the API shape plus the
// mapped canonical params, kept server-side until confirm.
type importCandidate struct {
	Name           string
	Protocol       string
	Port           int
	Params         string // canonical, validated params JSON
	DisabledReason string
}

// ScanImport fetches a node's current core inbounds and maps them to
// import candidates. Supported protocols only (others are dropped
// silently); already panel-managed names for this node are dropped;
// name/port/params problems become disabled rows with reasons.
func (s *Service) ScanImport(ctx context.Context, nodeID int64) (ImportScanResult, error) {
	node, err := s.st.GetNode(ctx, nodeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ImportScanResult{}, errNotFound("node")
		}
		return ImportScanResult{}, err
	}
	raw, err := s.fetchNodeInbounds(ctx, node)
	if err != nil {
		return ImportScanResult{}, err
	}
	cands, err := s.scanCandidates(ctx, nodeID, raw)
	if err != nil {
		return ImportScanResult{}, err
	}
	out := ImportScanResult{Candidates: []ImportCandidate{}}
	for _, c := range cands {
		out.Candidates = append(out.Candidates, ImportCandidate{
			Name: c.Name, Protocol: c.Protocol, Port: c.Port,
			DisabledReason: c.DisabledReason,
		})
	}
	return out, nil
}

// ImportInbounds creates panel inbounds for the named candidates. It
// re-fetches from the agent (no params travel to the browser), so a
// stale selection is re-validated against fresh state: names that
// are missing, disabled, or now conflicting land in Skipped, not an
// error, so the rest still import. Created inbounds are nodes-only
// (run_local=0, exactly one assignment to this node, explicit port);
// the normal auto-push picks them up within one poller tick.
func (s *Service) ImportInbounds(ctx context.Context, nodeID int64, names []string) (ImportResult, error) {
	node, err := s.st.GetNode(ctx, nodeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ImportResult{}, errNotFound("node")
		}
		return ImportResult{}, err
	}
	raw, err := s.fetchNodeInbounds(ctx, node)
	if err != nil {
		return ImportResult{}, err
	}
	cands, err := s.scanCandidates(ctx, nodeID, raw)
	if err != nil {
		return ImportResult{}, err
	}
	byName := map[string]importCandidate{}
	for _, c := range cands {
		byName[c.Name] = c
	}
	res := ImportResult{Created: []ImportCreated{}, Skipped: []ImportSkipped{}}
	seen := map[string]bool{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		cand, ok := byName[name]
		if !ok {
			res.Skipped = append(res.Skipped, ImportSkipped{Name: name, Reason: "在节点上不存在或不可导入"})
			continue
		}
		if cand.DisabledReason != "" {
			res.Skipped = append(res.Skipped, ImportSkipped{Name: name, Reason: cand.DisabledReason})
			continue
		}
		id, err := s.st.CreateInbound(ctx, cand.Name, cand.Protocol, cand.Port, cand.Params, false, time.Now().Unix())
		if err != nil {
			res.Skipped = append(res.Skipped, ImportSkipped{Name: name, Reason: importCreateReason(err)})
			continue
		}
		if err := s.st.SetInboundNodes(ctx, id, []store.InboundAssignment{{NodeID: nodeID, Port: cand.Port}}); err != nil {
			// No assignment means the inbound would run nowhere:
			// roll the record back instead of leaving a dead row.
			_ = s.st.DeleteInbound(ctx, id)
			res.Skipped = append(res.Skipped, ImportSkipped{Name: name, Reason: truncate(err.Error(), 200)})
			continue
		}
		res.Created = append(res.Created, ImportCreated{ID: id, Name: cand.Name, Protocol: cand.Protocol, Port: cand.Port})
	}
	return res, nil
}

// fetchNodeInbounds is the shared agent fetch for scan + import. An
// old agent (404) and an unreachable node map to user-facing errors;
// anything else passes through truncated.
func (s *Service) fetchNodeInbounds(ctx context.Context, node *store.NodeRow) ([]json.RawMessage, error) {
	raw, err := s.client.Inbounds(ctx, node.Address, node.Token)
	if err != nil {
		if errors.Is(err, ErrInboundsUnsupported) {
			return nil, errValidation("节点 agent 版本过旧，不支持导入，请升级 agent 后再试")
		}
		return nil, errUpstream(truncate(err.Error(), 200))
	}
	return raw, nil
}

// scanCandidates is the pure mapping step: parse core objects, map to
// panel params, and classify each against current panel state.
func (s *Service) scanCandidates(ctx context.Context, nodeID int64, raw []json.RawMessage) ([]importCandidate, error) {
	joined := joinRawArray(raw)
	running, err := inbounds.ParseCoreInbounds(joined)
	if err != nil {
		return nil, errValidation("节点核心配置无法解析：" + truncate(err.Error(), 200))
	}
	rows, err := s.st.ListInbounds(ctx)
	if err != nil {
		return nil, err
	}
	nodeAssignments, err := s.st.NodeInboundAssignments(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	assignedSet := map[int64]bool{}
	for _, a := range nodeAssignments {
		assignedSet[a.InboundID] = true
	}
	byName := map[string]store.InboundRow{}
	for _, r := range rows {
		byName[r.Name] = r
	}
	// Existing panel inbounds on this node, with effective node ports
	// (assignment port when >0, else the inbound's own port).
	var nodePorts []importNodePort
	assignPort := map[int64]int{}
	for _, a := range nodeAssignments {
		assignPort[a.InboundID] = a.Port
	}
	for _, r := range rows {
		if !assignedSet[r.ID] {
			continue
		}
		p := assignPort[r.ID]
		if p <= 0 {
			p = r.Port
		}
		nodePorts = append(nodePorts, importNodePort{row: r, port: p})
	}

	cands := []importCandidate{}
	var importable []importCandidate // for same-scan port conflicts
	for _, r := range running {
		protocol, params, ok := inbounds.MapRunning(&r)
		if !ok {
			continue // unreachable: ParseCoreInbounds already filtered
		}
		name := r.Tag
		if name == "" {
			// Untagged core inbounds still need a panel name; the
			// same fallback the local first-import uses.
			name = fmt.Sprintf("%s-%d", r.Type, r.Port)
		}
		// Already panel-managed for this node: drop silently (it
		// is in the inbound list already, not an import candidate).
		if existing, ok := byName[name]; ok && assignedSet[existing.ID] {
			continue
		}
		cand := importCandidate{Name: name, Protocol: protocol, Port: r.Port}
		_, nameTaken := byName[name]
		switch {
		case len([]rune(name)) > 64:
			cand.DisabledReason = "名称过长（超过 64 字）"
		case nameTaken:
			cand.DisabledReason = fmt.Sprintf("名称已被入站 %q 使用", name)
		case r.Port < 1 || r.Port > 65535:
			cand.DisabledReason = "端口无效"
		default:
			if what, reserved := inbounds.ReservedPorts[r.Port]; reserved {
				cand.DisabledReason = fmt.Sprintf("端口 %d 为保留端口（%s）", r.Port, what)
			} else if other, clash := nodePortClash(nodePorts, protocol, r.Port); clash {
				cand.DisabledReason = fmt.Sprintf("端口 %d（%s）已被该节点上的入站 %q 占用",
					r.Port, transportLabel(protocolTransport[protocol]), other.Name)
			} else if other, clash := scanPortClash(importable, protocol, r.Port); clash {
				cand.DisabledReason = fmt.Sprintf("与本次扫描中的入站 %q 端口冲突", other)
			} else if canonical, verr := inbounds.ValidateParams(protocol, json.RawMessage(params)); verr != nil {
				cand.DisabledReason = truncate(verr.Error(), 200)
			} else {
				cand.Params = canonical
			}
		}
		if cand.DisabledReason == "" {
			importable = append(importable, cand)
		}
		cands = append(cands, cand)
	}
	return cands, nil
}

// joinRawArray joins raw JSON objects into one JSON array document
// for ParseCoreInbounds.
func joinRawArray(raw []json.RawMessage) json.RawMessage {
	parts := make([]string, 0, len(raw))
	for _, r := range raw {
		if len(r) > 0 {
			parts = append(parts, string(r))
		}
	}
	return json.RawMessage("[" + strings.Join(parts, ",") + "]")
}

// importNodePort is an existing panel inbound on the scanned node
// with its effective node port (assignment port when >0, else the
// inbound's own port).
type importNodePort struct {
	row  store.InboundRow
	port int
}

// nodePortClash reports whether port+transport collides with an
// existing panel inbound on this node.
func nodePortClash(nodePorts []importNodePort, protocol string, port int) (store.InboundRow, bool) {
	for _, np := range nodePorts {
		if np.port == port && transportsOverlap(protocolTransport[np.row.Protocol], protocolTransport[protocol]) {
			return np.row, true
		}
	}
	return store.InboundRow{}, false
}

// scanPortClash reports whether port+transport collides with an
// earlier importable candidate from the same scan.
func scanPortClash(importable []importCandidate, protocol string, port int) (string, bool) {
	for _, c := range importable {
		if c.Port == port && transportsOverlap(protocolTransport[c.Protocol], protocolTransport[protocol]) {
			return c.Name, true
		}
	}
	return "", false
}

// transportsOverlap reports whether two transport sets share a lane.
// "" is treated as both (conservative).
func transportsOverlap(a, b string) bool {
	if a == "" {
		a = "both"
	}
	if b == "" {
		b = "both"
	}
	if a == "both" || b == "both" {
		return true
	}
	return a == b
}

// transportLabel renders a protocolTransport value for messages.
func transportLabel(t string) string {
	switch t {
	case "tcp":
		return "TCP"
	case "udp":
		return "UDP"
	default:
		return "TCP+UDP"
	}
}

// importCreateReason maps a create failure to a user-facing reason.
func importCreateReason(err error) string {
	msg := err.Error()
	if strings.Contains(msg, "UNIQUE constraint") {
		return "名称已被使用"
	}
	return truncate(msg, 200)
}

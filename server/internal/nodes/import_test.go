package nodes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"singbox-ui/server/internal/inbounds"
	"singbox-ui/server/internal/store"
)

func seedImportStore(t *testing.T) (*store.Store, int64) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(ctx, filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	now := int64(1700000000)
	// v1: assigned to the node (effective node port 443).
	vlessID, err := st.CreateInbound(ctx, "v1", "vless-reality", 443,
		`{"sni":"example.com","publicKey":"k","privateKey":"p","shortId":"","flow":"xtls-rprx-vision"}`, true, now)
	if err != nil {
		t.Fatalf("CreateInbound v1: %v", err)
	}
	// ss2: local only (NOT assigned to the node) — a name conflict
	// target for import candidates.
	if _, err := st.CreateInbound(ctx, "ss2", "trojan", 18353,
		`{"password":"pw","sni":""}`, true, now); err != nil {
		t.Fatalf("CreateInbound ss2: %v", err)
	}
	nodeID, err := st.CreateNode(ctx, store.NodeRow{
		Name: "tokyo", Address: "203.0.113.7:32435", Token: "ntok",
		PublicHost: "node.example.com", CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	if err := st.SetInboundNodes(ctx, vlessID, []store.InboundAssignment{{NodeID: nodeID}}); err != nil {
		t.Fatalf("SetInboundNodes vless: %v", err)
	}
	return st, nodeID
}

// importTestCore is the node's core config as the agent would return
// it: a mix of importable, conflicting, unsupported and broken rows.
func importTestCore(t *testing.T) []json.RawMessage {
	t.Helper()
	kp, err := inbounds.GenerateRealityKeypair()
	if err != nil {
		t.Fatal(err)
	}
	raw := []string{
		// Already panel-managed for this node (same name, assigned):
		// dropped silently.
		`{"type":"vless","tag":"v1","listen_port":443}`,
		// Importable hy2 with salamander obfs (must survive mapping).
		`{"type":"hysteria2","tag":"legacy-hy2","listen_port":18360,
		  "up_mbps":100,"down_mbps":200,
		  "users":[{"name":"u1","password":"pw-123"}],
		  "obfs":{"type":"salamander","password":"obfs-123"},
		  "tls":{"server_name":"node.example.com"}}`,
		// Name matches a panel inbound NOT for this node: disabled.
		`{"type":"trojan","tag":"ss2","listen_port":18363,
		  "users":[{"name":"u","password":"pw"}]}`,
		// Port 443/TCP collides with v1 on this node: disabled.
		`{"type":"trojan","tag":"clash-port","listen_port":443,
		  "users":[{"name":"u","password":"pw"}]}`,
		// Unsupported type: dropped silently, never listed.
		`{"type":"vmess","tag":"old-vmess","listen_port":18361}`,
		// Trojan with an empty-password user: disabled by params validation.
		`{"type":"trojan","tag":"bad-tj","listen_port":18370,
		  "users":[{"name":"u","password":""}]}`,
		// Two TCP inbounds on one port: first wins, second disabled.
		`{"type":"anytls","tag":"at1","listen_port":18371,
		  "users":[{"name":"u","password":"pw"}]}`,
		`{"type":"trojan","tag":"tj1","listen_port":18371,
		  "users":[{"name":"u","password":"pw"}]}`,
		// Untagged: gets a generated name, importable.
		`{"type":"trojan","listen_port":18372,
		  "users":[{"name":"u","password":"pw"}]}`,
		// Reserved port (panel set covers the default agent port):
		// disabled.
		`{"type":"trojan","tag":"resv","listen_port":32435,
		  "users":[{"name":"u","password":"pw"}]}`,
		// Importable vless with a real keypair + flow.
		fmt.Sprintf(`{"type":"vless","tag":"legacy-v","listen_port":18373,
		  "users":[{"name":"u","uuid":"11111111-2222-3333-4444-555555555555","flow":"xtls-rprx-vision"}],
		  "tls":{"server_name":"example.com","reality":{"private_key":%q,"short_id":["abcd"]}}}`,
			kp.PrivateKey),
	}
	out := make([]json.RawMessage, 0, len(raw))
	for _, s := range raw {
		out = append(out, json.RawMessage(s))
	}
	return out
}

func TestScanCandidatesClassification(t *testing.T) {
	ctx := context.Background()
	st, nodeID := seedImportStore(t)
	svc := &Service{st: st, client: NewClient()}
	cands, err := svc.scanCandidates(ctx, nodeID, importTestCore(t))
	if err != nil {
		t.Fatalf("scanCandidates: %v", err)
	}
	byName := map[string]importCandidate{}
	for _, c := range cands {
		byName[c.Name] = c
	}
	// Dropped silently: already-managed v1, unsupported vmess.
	if _, ok := byName["v1"]; ok {
		t.Error("already-managed v1 must be dropped, not listed")
	}
	if _, ok := byName["old-vmess"]; ok {
		t.Error("unsupported vmess must be dropped, never listed")
	}
	// Importable hy2: obfs + password + bandwidth survive mapping.
	hy2, ok := byName["legacy-hy2"]
	if !ok {
		t.Fatal("legacy-hy2 missing from candidates")
	}
	if hy2.DisabledReason != "" {
		t.Errorf("legacy-hy2 must be importable, got disabled: %q", hy2.DisabledReason)
	}
	var hp struct {
		UpMbps       int    `json:"upMbps"`
		DownMbps     int    `json:"downMbps"`
		Password     string `json:"password"`
		Obfs         string `json:"obfs"`
		ObfsPassword string `json:"obfsPassword"`
	}
	if err := json.Unmarshal([]byte(hy2.Params), &hp); err != nil {
		t.Fatalf("hy2 params: %v", err)
	}
	if hp.UpMbps != 100 || hp.DownMbps != 200 || hp.Password != "pw-123" ||
		hp.Obfs != "salamander" || hp.ObfsPassword != "obfs-123" {
		t.Errorf("hy2 params not faithful: %+v", hp)
	}
	// Importable vless: keypair + short id + flow survive mapping.
	v, ok := byName["legacy-v"]
	if !ok {
		t.Fatal("legacy-v missing from candidates")
	}
	if v.DisabledReason != "" {
		t.Errorf("legacy-v must be importable, got disabled: %q", v.DisabledReason)
	}
	var vp struct {
		SNI        string `json:"sni"`
		PublicKey  string `json:"publicKey"`
		PrivateKey string `json:"privateKey"`
		ShortID    string `json:"shortId"`
		Flow       string `json:"flow"`
	}
	if err := json.Unmarshal([]byte(v.Params), &vp); err != nil {
		t.Fatalf("vless params: %v", err)
	}
	if vp.SNI != "example.com" || vp.PrivateKey == "" || vp.PublicKey == "" ||
		vp.ShortID != "abcd" || vp.Flow != "xtls-rprx-vision" {
		t.Errorf("vless params not faithful: %+v", vp)
	}
	// Disabled rows carry reasons.
	for name, want := range map[string]string{
		"ss2":        "名称已被入站",
		"clash-port": "已被该节点上的入站",
		"bad-tj":     "password is required",
		"tj1":        "端口冲突",
		"resv":       "保留端口",
	} {
		c, ok := byName[name]
		if !ok {
			t.Errorf("%s missing from candidates", name)
			continue
		}
		if !strings.Contains(c.DisabledReason, want) {
			t.Errorf("%s: disabled reason %q, want it to contain %q", name, c.DisabledReason, want)
		}
	}
	if c, ok := byName["at1"]; !ok || c.DisabledReason != "" {
		t.Errorf("at1 must be importable (first of the colliding pair), got %+v", c)
	}
	// Untagged inbound gets a generated name and is importable.
	if c, ok := byName["trojan-18372"]; !ok || c.DisabledReason != "" {
		t.Errorf("untagged trojan must be importable as trojan-18372, got %+v", c)
	}
}

func TestImportInboundsRoundTrip(t *testing.T) {
	ctx := context.Background()
	st, nodeID := seedImportStore(t)
	raw := importTestCore(t)
	parts := make([]string, 0, len(raw))
	for _, r := range raw {
		parts = append(parts, string(r))
	}
	body := `{"inbounds": [` + strings.Join(parts, ",") + `]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer ntok" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/api/agent/inbounds" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	// Point the node at the test agent (http scheme: test agent).
	node, err := st.GetNode(ctx, nodeID)
	if err != nil {
		t.Fatal(err)
	}
	node.Address = srv.URL
	if err := st.UpdateNode(ctx, *node); err != nil {
		t.Fatal(err)
	}
	svc := &Service{st: st, client: NewClient()}
	res, err := svc.ImportInbounds(ctx, nodeID, []string{"legacy-hy2", "ss2", "missing-name"})
	if err != nil {
		t.Fatalf("ImportInbounds: %v", err)
	}
	if len(res.Created) != 1 || res.Created[0].Name != "legacy-hy2" {
		t.Errorf("created = %+v, want only legacy-hy2", res.Created)
	}
	if len(res.Skipped) != 2 {
		t.Errorf("skipped = %+v, want ss2 + missing-name", res.Skipped)
	}
	// The created inbound is nodes-only with an explicit assignment.
	rows, err := st.ListInbounds(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var created *store.InboundRow
	for i := range rows {
		if rows[i].Name == "legacy-hy2" {
			created = &rows[i]
		}
	}
	if created == nil {
		t.Fatal("legacy-hy2 not in store after import")
	}
	if created.RunLocal {
		t.Error("imported inbound must be nodes-only (run_local=0)")
	}
	if created.Port != 18360 {
		t.Errorf("imported port = %d, want 18360", created.Port)
	}
	nodes, err := st.InboundNodes(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].NodeID != nodeID || nodes[0].Port != 18360 {
		t.Errorf("assignment = %+v, want exactly [{node %d, port 18360}]", nodes, nodeID)
	}
}

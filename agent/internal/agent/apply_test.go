package agent

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- fakes ---------------------------------------------------------------

type fakeChecker struct{ failMarker string }

func (f fakeChecker) Check(_ context.Context, path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if f.failMarker != "" && strings.Contains(string(b), f.failMarker) {
		return "check output: bad value", errors.New("exit status 1")
	}
	return "", nil
}

type fakeRestarter struct {
	failFirst int
	calls     int
}

func (f *fakeRestarter) Restart(_ context.Context) error {
	f.calls++
	if f.calls <= f.failFirst {
		return errors.New("restart boom")
	}
	return nil
}

type fakeProber struct {
	state string
	pids  []string
	call  int
}

func (f *fakeProber) State(_ context.Context) string { return f.state }

func (f *fakeProber) MainPID(_ context.Context) string {
	if len(f.pids) == 0 {
		return "1234"
	}
	p := f.pids[f.call%len(f.pids)]
	f.call++
	return p
}

type fakeClash struct{ alive bool }

func (f fakeClash) Alive(_ context.Context) bool { return f.alive }

// rawJSON marshals a test fixture (panics on error: fixtures are static).
func rawJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// --- engine tests ----------------------------------------------------------

func testEngine(t *testing.T, dir string, restarter *fakeRestarter, prober *fakeProber) *Engine {
	t.Helper()
	return &Engine{
		Target:    filepath.Join(dir, "config.json"),
		Bin:       "sing-box",
		Unit:      "sing-box",
		Check:     fakeChecker{},
		Restart:   restarter,
		UnitProbe: prober,
		Clash:     fakeClash{alive: true},
	}
}

func TestEngineNoRestartMode(t *testing.T) {
	dir := t.TempDir()
	e := &Engine{Target: filepath.Join(dir, "config.json"), Bin: "sing-box", Unit: "", Check: fakeChecker{}}
	if err := e.Apply(context.Background(), []byte(`{"inbounds":[]}`), nil); err != nil {
		t.Fatalf("write-only apply must succeed: %v", err)
	}
	if _, err := os.ReadFile(e.Target); err != nil {
		t.Fatal(err)
	}
}

func TestEngineCheckFailedKeepsOldFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "config.json")
	_ = os.WriteFile(target, []byte(`{"old":true}`), 0o600)
	e := &Engine{Target: target, Bin: "sing-box", Unit: "", Check: fakeChecker{failMarker: "BAD-METHOD"}}
	if err := e.Apply(context.Background(), []byte(`{"method":"BAD-METHOD"}`), nil); err == nil || err.result != "check_failed" {
		t.Fatalf("want check_failed, got %v", err)
	}
	b, _ := os.ReadFile(target)
	if string(b) != `{"old":true}` {
		t.Fatalf("failed check must not touch the target, got %s", b)
	}
}

func TestEngineRetryThenSuccess(t *testing.T) {
	dir := t.TempDir()
	r := &fakeRestarter{failFirst: 1}
	e := testEngine(t, dir, r, &fakeProber{state: "active"})
	if err := e.Apply(context.Background(), []byte(`{"new":true}`), []byte(`{"old":true}`)); err != nil {
		t.Fatalf("retry must recover: %v", err)
	}
	if r.calls != 2 {
		t.Fatalf("want 2 restart calls (1 fail + 1 ok), got %d", r.calls)
	}
}

func TestEngineRollbackOnPersistentFailure(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "config.json")
	_ = os.WriteFile(target, []byte(`{"old":true}`), 0o600)
	// 3 failed new-doc attempts, then the rollback restart succeeds.
	r := &fakeRestarter{failFirst: 3}
	e := testEngine(t, dir, r, &fakeProber{state: "active"})
	err := e.Apply(context.Background(), []byte(`{"new":true}`), []byte(`{"old":true}`))
	if err == nil || err.result != "restart_failed" || !err.rolledBack {
		t.Fatalf("want restart_failed + rolledBack, got %v", err)
	}
	b, _ := os.ReadFile(target)
	if string(b) != `{"old":true}` {
		t.Fatalf("rollback must restore the old file, got %s", b)
	}
	if r.calls != 4 {
		t.Fatalf("want 4 restart calls (3 fail + rollback ok), got %d", r.calls)
	}
}

func TestEngineFlappingPIDFailsFast(t *testing.T) {
	dir := t.TempDir()
	// Restarts succeed, but PIDs alternate: the health poll must
	// fail fast on flap, not burn the full 30s per attempt.
	// Rollback flaps too (same prober), so rolledBack is false,
	// but the result is still restart_failed, quickly.
	r := &fakeRestarter{failFirst: 0}
	e := testEngine(t, dir, r, &fakeProber{state: "active", pids: []string{"111", "222"}})
	err := e.Apply(context.Background(), []byte(`{"new":true}`), []byte(`{"old":true}`))
	if err == nil || err.result != "restart_failed" {
		t.Fatalf("want restart_failed on flap, got %v", err)
	}
	if err.rolledBack {
		t.Fatal("rollback cannot succeed while PIDs flap")
	}
}

// --- agent-level tests -----------------------------------------------------

func testAgent(t *testing.T, engine *Engine) (*Agent, string) {
	t.Helper()
	dir := t.TempDir()
	target := filepath.Join(dir, "config.json")
	engine.Target = target // the agent and its engine must share the target
	cfg := Config{
		DataDir:   dir,
		Target:    target,
		Bin:       "sing-box",
		ClashAddr: "",
		LogPath:   "",
		MgmtPort:  32435,
		Engine:    engine,
	}
	ag, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return ag, dir
}

func testRealityParamsForApply() realityParams {
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	return realityParams{
		SNI:        "www.microsoft.com",
		PublicKey:  base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes()),
		PrivateKey: base64.RawURLEncoding.EncodeToString(priv.Bytes()),
		ShortID:    "abcd1234",
	}
}

func ssPush(port int, method, key string) Push {
	_ = method
	_ = key
	return Push{Inbounds: []Inbound{{
		Name: "v1", Protocol: ProtocolVLESSReality, Port: port, Enabled: true,
		Params: rawJSON(testRealityParamsForApply()),
		Users:  []User{{Name: "s1", UUID: "11111111-1111-4111-8111-111111111111"}},
	}}}
}

func TestAgentPushApplyNoChange(t *testing.T) {
	engine := &Engine{Bin: "sing-box", Unit: "", Check: fakeChecker{}}
	ag, _ := testAgent(t, engine)
	push := ssPush(9001, "aes-256-gcm", "pw")
	res := ag.ApplyPush(context.Background(), push)
	if res.Result != "applied" {
		t.Fatalf("first push must apply, got %+v", res)
	}
	res2 := ag.ApplyPush(context.Background(), push)
	if res2.Result != "no_change" {
		t.Fatalf("identical push must be no_change, got %+v", res2)
	}
}

func TestAgentBlockedAndClear(t *testing.T) {
	// Pre-create the target so the first push has a rollback target.
	// The restarter never recovers: every apply fails at runtime,
	// so rollback restarts fail too (rolledBack false) but the file
	// is still restored and the input is still blocked.
	r := &fakeRestarter{failFirst: 99}
	engine := &Engine{Bin: "sing-box", Unit: "sing-box", Check: fakeChecker{},
		Restart: r, UnitProbe: &fakeProber{state: "active"}, Clash: fakeClash{alive: true}}
	ag, dir := testAgent(t, engine)
	_ = os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"old":true}`), 0o600)
	push := ssPush(9001, "aes-256-gcm", "pw")
	res := ag.ApplyPush(context.Background(), push)
	if res.Result != "restart_failed" {
		t.Fatalf("want restart_failed, got %+v", res)
	}
	callsAfterFail := r.calls
	res2 := ag.ApplyPush(context.Background(), push)
	if res2.Result != "blocked" {
		t.Fatalf("same input must be blocked, got %+v", res2)
	}
	if r.calls != callsAfterFail {
		t.Fatalf("blocked push must not restart, calls %d -> %d", callsAfterFail, r.calls)
	}
	if prev := ag.ClearBlock(); prev == "" {
		t.Fatal("clear-block must report the previous blocked hash")
	}
	res3 := ag.ApplyPush(context.Background(), push)
	if res3.Result != "restart_failed" {
		t.Fatalf("after clear, push must be attempted again, got %+v", res3)
	}
}

func TestAgentPassthroughAndDeletion(t *testing.T) {
	engine := &Engine{Bin: "sing-box", Unit: "", Check: fakeChecker{}}
	ag, dir := testAgent(t, engine)
	pushA := Push{Inbounds: []Inbound{{
		Name: "vA", Protocol: ProtocolVLESSReality, Port: 443, Enabled: true,
		Params: mustJSON(t, testRealityParams(t)),
		Users:  []User{{Name: "s1", UUID: "11111111-1111-4111-8111-111111111111"}},
	}}}
	if res := ag.ApplyPush(context.Background(), pushA); res.Result != "applied" {
		t.Fatalf("push A must apply: %+v", res)
	}
	// Hand-edit the target: add a route + an unmanaged inbound.
	target := filepath.Join(dir, "config.json")
	b, _ := os.ReadFile(target)
	var root map[string]any
	if err := json.Unmarshal(b, &root); err != nil {
		t.Fatal(err)
	}
	root["route"] = map[string]any{"final": "direct"}
	inbounds := docInbounds(root)
	inbounds = append(inbounds, map[string]any{"type": "socks", "tag": "hand", "listen": "::", "listen_port": 1080})
	root["inbounds"] = inbounds
	raw, _ := json.Marshal(root)
	_ = os.WriteFile(target, raw, 0o600)
	// Second push: vless B only. A was in the last doc and is not
	// desired -> deleted. route + hand are not in the last doc and
	// not desired -> preserved as passthrough.
	pushB := Push{Inbounds: []Inbound{{
		Name: "vB", Protocol: ProtocolVLESSReality, Port: 8443, Enabled: true,
		Params: mustJSON(t, testRealityParams(t)),
		Users:  []User{{Name: "s1", UUID: "11111111-1111-4111-8111-111111111111"}},
	}}}
	if res := ag.ApplyPush(context.Background(), pushB); res.Result != "applied" {
		t.Fatalf("push B must apply: %+v", res)
	}
	b2, _ := os.ReadFile(target)
	s := string(b2)
	for _, want := range []string{`"vB"`, `"route"`, `"hand"`, `"final": "direct"`} {
		if !strings.Contains(s, want) {
			t.Fatalf("merged file must contain %s, got:\n%s", want, s)
		}
	}
	if strings.Contains(s, `"vA"`) {
		t.Fatalf("deleted inbound vA must not survive, got:\n%s", s)
	}
}

func TestAgentPassthroughCheckFallback(t *testing.T) {
	engine := &Engine{Bin: "sing-box", Unit: "", Check: fakeChecker{failMarker: "BAD-METHOD"}}
	ag, dir := testAgent(t, engine)
	if res := ag.ApplyPush(context.Background(), ssPush(9001, "aes-256-gcm", "pw")); res.Result != "applied" {
		t.Fatalf("first push must apply: %+v", res)
	}
	// Hand-add a bogus passthrough inbound to the target file.
	target := filepath.Join(dir, "config.json")
	b, _ := os.ReadFile(target)
	var root map[string]any
	if err := json.Unmarshal(b, &root); err != nil {
		t.Fatal(err)
	}
	root["inbounds"] = append(docInbounds(root),
		map[string]any{"type": "trojan", "tag": "hand-bad", "method": "BAD-METHOD"})
	raw, _ := json.Marshal(root)
	_ = os.WriteFile(target, raw, 0o600)
	// A changed push: merged check fails on BAD-METHOD, desired-only
	// must apply with passthrough dropped.
	res := ag.ApplyPush(context.Background(), ssPush(9002, "aes-256-gcm", "pw"))
	if res.Result != "applied" {
		t.Fatalf("fallback must apply desired-only, got %+v", res)
	}
	b2, _ := os.ReadFile(target)
	if strings.Contains(string(b2), "BAD-METHOD") {
		t.Fatalf("dropped passthrough must not survive:\n%s", b2)
	}
}

func TestAgentTLSCertRequired(t *testing.T) {
	engine := &Engine{Bin: "sing-box", Unit: "", Check: fakeChecker{}}
	ag, _ := testAgent(t, engine)
	push := Push{Inbounds: []Inbound{{
		Name: "h1", Protocol: ProtocolHysteria2, Port: 443, Enabled: true,
		Params: mustJSON(t, hy2Params{Password: "pw"}),
		Users:  []User{{Name: "s1"}},
	}}}
	res := ag.ApplyPush(context.Background(), push)
	if res.Result != "error" {
		t.Fatalf("TLS inbound without a cert must error, got %+v", res)
	}
}

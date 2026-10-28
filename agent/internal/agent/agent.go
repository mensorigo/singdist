package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Config wires the agent. Reserved ports are derived: the fixed set
// (SSH, Clash API, panel) plus the agent's own management port, so
// a pushed inbound can never steal the port the agent listens on.
type Config struct {
	DataDir       string
	Target        string // core config path
	Bin           string // sing-box binary for `check`
	Unit          string // systemd unit; "" = write + validate only
	ClashAddr     string // "" = skip the Clash liveness gate
	LogPath       string // core log.output path ("" = none)
	MgmtPort      int    // management API port (for reserved ports)
	LocalInbounds string // optional local inbounds file path
	Engine        *Engine
}

func (c Config) reservedPorts() map[int]string {
	m := map[int]string{
		2826:  "SSH",
		9090:  "Clash API",
		32434: "面板",
	}
	if c.MgmtPort > 0 {
		m[c.MgmtPort] = "Agent 管理接口"
	}
	return m
}

// Result is one push outcome, mirroring the panel's sync results.
type Result struct {
	Result     string         `json:"result"` // applied|no_change|check_failed|restart_failed|error|blocked|busy
	Hash       string         `json:"hash"`
	Detail     string         `json:"detail"`
	Notes      []string       `json:"notes,omitempty"`
	Inbounds   []InboundState `json:"inbounds,omitempty"`
	RolledBack bool           `json:"rolledBack,omitempty"`
}

// InboundState is one inbound as reported in results and status.
type InboundState struct {
	Name      string `json:"name"`
	Protocol  string `json:"protocol"`
	Port      int    `json:"port"`
	Source    string `json:"source"` // managed | passthrough
	Listening *bool  `json:"listening,omitempty"`
}

// Agent owns the push pipeline. One apply runs at a time (try-lock:
// a concurrent push reports busy instead of queueing, like the
// panel's whole-sync mutex).
type Agent struct {
	cfg    Config
	engine *Engine
	mu     sync.Mutex
	state  *State
}

func New(cfg Config) (*Agent, error) {
	engine := cfg.Engine
	if engine == nil {
		engine = NewProductionEngine(cfg.Target, cfg.Bin, cfg.Unit, cfg.ClashAddr)
	}
	st, err := loadState(filepath.Join(cfg.DataDir, "state.json"))
	if err != nil {
		return nil, fmt.Errorf("agent: load state: %w", err)
	}
	return &Agent{cfg: cfg, engine: engine, state: st}, nil
}

// ApplyPush runs one push end to end: validate, merge with local
// inbounds and target-file passthrough, generate, then apply. The
// caller supplies a background context (a client disconnect must
// not abort an apply mid-restart); the pipeline itself is bounded
// by a 90s backstop like the panel's.
func (a *Agent) ApplyPush(ctx context.Context, push Push) Result {
	if !a.mu.TryLock() {
		return Result{Result: "busy", Detail: "another apply is still running; retry this push later"}
	}
	defer a.mu.Unlock()

	ctx90, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	ctx = ctx90

	reserved := a.cfg.reservedPorts()
	pushed, err := validateInbounds(push.Inbounds, reserved)
	if err != nil {
		return Result{Result: "error", Detail: err.Error()}
	}
	local, err := loadLocalFile(a.cfg.LocalInbounds, reserved)
	if err != nil {
		return Result{Result: "error", Detail: "local inbounds: " + err.Error()}
	}

	// Merge pushed + local: pushed wins by name; a local inbound
	// whose port collides (same transport) with a pushed one is
	// shadowed for this merge only (its file is untouched, so it
	// reappears if the pushed inbound goes away). Notes record
	// every shadowing so the central panel can show it.
	var notes []string
	desired, shadowed := mergeDesired(pushed, local.Inbounds)
	notes = append(notes, shadowed...)

	// Certificate: the push cert wins; the local file cert is the
	// fallback. Any enabled TLS inbound without a resolved cert is
	// a hard error (the agent never invents certificates).
	cert := push.Cert
	if cert == nil {
		cert = local.Cert
	}
	if needsCert(desired) {
		if cert == nil || cert.Certificate == "" || cert.Key == "" {
			return Result{Result: "error", Detail: "a TLS inbound is present but no certificate was pushed or configured locally"}
		}
	}

	desiredDoc, desiredHash, err := generate(desired, push.PublicHost, cert, a.cfg.LogPath, a.cfg.ClashAddr)
	if err != nil {
		return Result{Result: "error", Detail: "generate: " + err.Error()}
	}

	targetBytes, ferr := os.ReadFile(a.cfg.Target)
	targetMissing := os.IsNotExist(ferr)
	if ferr != nil && !targetMissing {
		return Result{Result: "error", Detail: "read target: " + ferr.Error()}
	}

	var mergedDoc []byte
	var mergedHash string
	firstApply := a.state.LastDoc == ""
	if firstApply {
		// No baseline: the agent takes over the core config
		// entirely (the distribution rule: after install, the
		// core belongs to the agent). The original file is
		// backed up first so this can be undone by hand.
		if !targetMissing {
			backupOriginal(a.cfg.DataDir, targetBytes)
		}
		mergedDoc, mergedHash = desiredDoc, desiredHash
		notes = append(notes, "first apply: core config taken over (original backed up)")
	} else {
		mergedDoc, mergedHash, notes = a.mergeWithTarget(desiredDoc, desiredHash, targetBytes, targetMissing, notes)
	}

	// Anti-loop input block: a merged doc that failed at runtime
	// is skipped until it changes (or the central panel clears
	// the block explicitly).
	if a.state.BlockedHash != "" && mergedHash == a.state.BlockedHash {
		return Result{Result: "blocked", Hash: mergedHash, Detail: "this config failed at runtime before and is blocked until it changes or the block is cleared", Notes: notes}
	}

	// No-change: the file already holds the merged doc AND the
	// core already runs it (last applied matches too). A file
	// that holds the merged doc while the core runs an older one
	// is a catch-up apply, not a no-op.
	if !targetMissing {
		if fileParsed, perr := parseDoc(targetBytes); perr == nil {
			mergedParsed, _ := parseDoc(mergedDoc)
			if semanticEqual(mergedParsed, fileParsed) && mergedHash == a.state.LastHash {
				a.state.LastResult = "no_change"
				a.state.LastSyncAt = nowUnix()
				_ = saveState(filepath.Join(a.cfg.DataDir, "state.json"), a.state)
				return Result{Result: "no_change", Hash: mergedHash, Detail: "merged config matches the target file and the running config", Notes: notes, Inbounds: a.statusInbounds()}
			}
			if semanticEqual(mergedParsed, fileParsed) {
				notes = append(notes, "catch-up: file already held the merged config; core restarted to catch up")
			}
		}
	}

	if a.cfg.LogPath != "" {
		if err := os.MkdirAll(filepath.Dir(a.cfg.LogPath), 0o755); err != nil {
			return Result{Result: "error", Hash: mergedHash, Detail: "create log directory: " + err.Error(), Notes: notes}
		}
	}

	var beforeDoc []byte
	if a.state.LastDoc != "" {
		beforeDoc = []byte(a.state.LastDoc)
	} else if !targetMissing {
		beforeDoc = targetBytes
	}

	if aerr := a.engine.Apply(ctx, mergedDoc, beforeDoc); aerr != nil {
		if aerr.result == "check_failed" && !firstApply {
			// Illegal passthrough must never block a legitimate
			// push: fall back to desired-only (all-drop, the
			// same rule the panel uses for bad passthrough).
			desiredParsed, _ := parseDoc(desiredDoc)
			mergedParsed, _ := parseDoc(mergedDoc)
			if !semanticEqual(desiredParsed, mergedParsed) {
				if aerr2 := a.engine.Apply(ctx, desiredDoc, beforeDoc); aerr2 == nil {
					a.saveApplied(desiredDoc, desiredHash, "applied")
					notes = append(notes, "merged config failed check ("+aerr.Error()+"); applied desired-only, passthrough dropped")
					return Result{Result: "applied", Hash: desiredHash, Detail: "config applied (passthrough dropped after check failure)", Notes: notes, Inbounds: a.statusInbounds()}
				} else {
					return a.failResult(aerr2, mergedHash, notes)
				}
			}
		}
		return a.failResult(aerr, mergedHash, notes)
	}

	a.saveApplied(mergedDoc, mergedHash, "applied")
	return Result{Result: "applied", Hash: mergedHash, Detail: "config applied", Notes: notes, Inbounds: a.statusInbounds()}
}

func (a *Agent) failResult(aerr *applyError, hash string, notes []string) Result {
	res := Result{Result: aerr.result, Hash: hash, Detail: aerr.Error(), Notes: notes, RolledBack: aerr.rolledBack}
	if aerr.result == "restart_failed" {
		// Keep the baseline at the pre-round version and block
		// this input until it changes; the core runs the old
		// config, so the push is NOT applied.
		a.state.BlockedHash = hash
		a.state.LastResult = "restart_failed"
		a.state.LastSyncAt = nowUnix()
		_ = saveState(filepath.Join(a.cfg.DataDir, "state.json"), a.state)
	} else {
		a.state.LastResult = aerr.result
		a.state.LastSyncAt = nowUnix()
		_ = saveState(filepath.Join(a.cfg.DataDir, "state.json"), a.state)
	}
	return res
}

func (a *Agent) saveApplied(doc []byte, hash, result string) {
	a.state.LastDoc = string(doc)
	a.state.LastHash = hash
	a.state.BlockedHash = ""
	a.state.LastResult = result
	a.state.LastSyncAt = nowUnix()
	_ = saveState(filepath.Join(a.cfg.DataDir, "state.json"), a.state)
}

// ClearBlock clears the anti-loop input block (the central panel's
// explicit override, mirroring the panel's manual-sync bypass).
// It returns the previous blocked hash ("" when nothing was blocked).
func (a *Agent) ClearBlock() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	prev := a.state.BlockedHash
	a.state.BlockedHash = ""
	_ = saveState(filepath.Join(a.cfg.DataDir, "state.json"), a.state)
	return prev
}

// mergeDesired merges pushed + local inbounds. Pushed wins by
// name; a local inbound colliding on port+transport with a pushed
// one is shadowed (not deleted: its file is untouched).
func mergeDesired(pushed, local []Inbound) (desired []Inbound, notes []string) {
	byName := map[string]bool{}
	for _, ib := range pushed {
		byName[ib.Name] = true
		desired = append(desired, ib)
	}
	for _, ib := range local {
		if byName[ib.Name] {
			notes = append(notes, fmt.Sprintf("local inbound %q shadowed by pushed inbound of the same name", ib.Name))
			continue
		}
		conflict := ""
		for _, p := range pushed {
			if p.Port == ib.Port && protocolTransports[p.Protocol]&protocolTransports[ib.Protocol] != 0 {
				conflict = p.Name
				break
			}
		}
		if conflict != "" {
			notes = append(notes, fmt.Sprintf("local inbound %q shadowed: port %d conflicts with pushed inbound %q", ib.Name, ib.Port, conflict))
			continue
		}
		byName[ib.Name] = true
		desired = append(desired, ib)
	}
	return desired, notes
}

// mergeWithTarget folds target-file passthrough into the desired
// doc: unmanaged top-level keys (anything the agent does not
// generate: not log/experimental/inbounds/outbounds) and target
// inbounds that are neither desired nor in the last applied doc
// (i.e. added outside the agent) are preserved. A target inbound
// that was in the last applied doc but is no longer desired was
// deleted by the central panel and is dropped, not preserved.
func (a *Agent) mergeWithTarget(desiredDoc []byte, desiredHash string, targetBytes []byte, targetMissing bool, notes []string) ([]byte, string, []string) {
	if targetMissing {
		return desiredDoc, desiredHash, append(notes, "target file missing, recreated from desired config")
	}
	fileParsed, perr := parseDoc(targetBytes)
	if perr != nil {
		return desiredDoc, desiredHash, append(notes, "target file was not valid JSON, replaced with desired config")
	}
	desiredParsed, _ := parseDoc(desiredDoc)
	lastParsed, _ := parseDoc([]byte(a.state.LastDoc))
	lastTags := docInboundTags(lastParsed)
	desiredNames := map[string]bool{}
	for _, ib := range docInbounds(desiredParsed) {
		if tag, ok := ib["tag"].(string); ok {
			desiredNames[tag] = true
		}
	}
	merged := desiredParsed
	for k, v := range fileParsed {
		switch k {
		case "log", "experimental", "inbounds", "outbounds":
			// Agent-managed keys: desired wins.
		default:
			merged[k] = v
		}
	}
	mergedInbounds := docInbounds(desiredParsed)
	for _, ib := range docInbounds(fileParsed) {
		tag, _ := ib["tag"].(string)
		if tag == "" || desiredNames[tag] || lastTags[tag] {
			continue
		}
		mergedInbounds = append(mergedInbounds, ib)
		notes = append(notes, fmt.Sprintf("target-file inbound %q preserved as passthrough", tag))
	}
	merged["inbounds"] = mergedInbounds
	doc, hash := marshalDoc(merged)
	return doc, hash, notes
}

// Status is the agent's live report for the central panel.
type Status struct {
	UnitActive  string         `json:"unitActive"` // active|failed|inactive|activating|unknown|"" (no unit)
	ClashAlive  bool           `json:"clashAlive"`
	Version     string         `json:"version,omitempty"`
	LastHash    string         `json:"lastHash"`
	LastSyncAt  int64          `json:"lastSyncAt"`
	LastResult  string         `json:"lastResult"`
	BlockedHash string         `json:"blockedHash,omitempty"`
	Inbounds    []InboundState `json:"inbounds"`
}

func (a *Agent) Status(ctx context.Context) Status {
	st := Status{
		LastHash:    a.state.LastHash,
		LastSyncAt:  a.state.LastSyncAt,
		LastResult:  a.state.LastResult,
		BlockedHash: a.state.BlockedHash,
		Inbounds:    a.statusInbounds(),
	}
	if a.engine.UnitProbe != nil {
		st.UnitActive = a.engine.UnitProbe.State(ctx)
	}
	if a.engine.Clash != nil {
		st.ClashAlive = a.engine.Clash.Alive(ctx)
	}
	return st
}

// statusInbounds derives inbound states from the last applied doc:
// managed inbounds from the doc itself, listening from /proc, and
// passthrough inbounds (in the file but not in the last doc) marked
// as such so the central panel can tell hand-added inbounds apart.
func (a *Agent) statusInbounds() []InboundState {
	out := []InboundState{}
	if a.state.LastDoc == "" {
		return out
	}
	lastParsed, err := parseDoc([]byte(a.state.LastDoc))
	if err != nil {
		return out
	}
	listening := ListeningPorts()
	for _, ib := range docInbounds(lastParsed) {
		name, _ := ib["tag"].(string)
		typ, _ := ib["type"].(string)
		port := 0
		if f, ok := ib["listen_port"].(float64); ok {
			port = int(f)
		}
		var l *bool
		if port > 0 {
			var v bool
			if typ == "hysteria2" || typ == "tuic" {
				v = listening.UDP[port]
			} else {
				v = listening.TCP[port]
			}
			l = &v
		}
		out = append(out, InboundState{Name: name, Protocol: singboxTypeToProtocol(typ), Port: port, Source: "managed", Listening: l})
	}
	return out
}

// singboxTypeToProtocol maps a core inbound type back to the panel
// protocol name. vless here is always vless-reality: the agent
// only generates Reality TLS for vless inbounds.
func singboxTypeToProtocol(typ string) string {
	if typ == "vless" {
		return ProtocolVLESSReality
	}
	return typ
}

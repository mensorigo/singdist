package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// applyError is one apply outcome. result mirrors the panel's sync
// results: check_failed, restart_failed, error. rolledBack reports
// whether the pre-round version was restored and the core is
// healthy on it again.
type applyError struct {
	result            string
	err               error
	rolledBack        bool
	rollbackAttempted bool
}

func (e *applyError) Error() string { return e.err.Error() }

// Checker validates a config file with the core binary.
// Checker validates a config file with the core binary.
type Checker interface {
	Check(ctx context.Context, path string) (string, error)
}

// Restarter restarts the core unit.
// Restarter restarts the core unit.
type Restarter interface {
	Restart(ctx context.Context) error
}

// UnitProber reports the core unit's state and main PID.
// UnitProber reports the core unit's state and main PID.
type UnitProber interface {
	State(ctx context.Context) string
	MainPID(ctx context.Context) string
}

// ClashProber reports whether the core's Clash API answers.
// ClashProber reports whether the core's Clash API answers.
type ClashProber interface {
	Alive(ctx context.Context) bool
}

// Engine applies a validated config doc to the core: check, atomic
// write, restart with health check, rollback on failure. It is the
// panel's apply pipeline with the store removed: the caller owns
// the desired doc and the pre-round version.
type Engine struct {
	Target    string // core config path
	Bin       string // sing-box binary for `check`
	Unit      string // systemd unit; "" = write + validate only, never restart
	ClashAddr string // "" = skip the Clash liveness gate

	Check   Checker
	Restart Restarter
	// UnitProbe is nil when no unit is configured (write + validate
	// only); restartAndCheck is never reached in that mode.
	UnitProbe UnitProber
	Clash     ClashProber
	Warn      func(msg string, args ...any) // optional advisory warnings
}

// execChecker runs `bin check -c path` (20s timeout).
// CheckDoc validates a doc without writing anything. It reports
// whether the check actually ran: when the check binary cannot even
// start, callers keep their previous optimistic behaviour instead
// of failing an apply that writes nothing.
func (e *Engine) CheckDoc(ctx context.Context, doc []byte) (ran, valid bool, out string) {
	dir := filepath.Dir(e.Target)
	tmp, err := os.CreateTemp(dir, ".singbox-agent-check-*.json")
	if err != nil {
		return false, false, ""
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(doc); err != nil {
		tmp.Close()
		return false, false, ""
	}
	if err := tmp.Close(); err != nil {
		return false, false, ""
	}
	out, err = e.Check.Check(ctx, tmpPath)
	if err != nil {
		var execErr *exec.Error
		if errors.As(err, &execErr) {
			return false, false, out
		}
		return true, false, out
	}
	return true, true, out
}

// Apply validates doc, atomically swaps it in, and (when a unit is
// configured) restarts the core and health-checks it. A restart or
// health failure is retried twice (3 attempts total, ~3s waits,
// restart-only: same file, no regenerate, no re-check). When all 3
// fail it rolls back to beforeDoc and health-checks that. beforeDoc
// may be nil (no pre-round version) — then there is nothing to
// roll back to.
// Apply validates doc, atomically swaps it in, and (when a unit is
// configured) restarts the core and health-checks it. A restart or
// health failure is retried twice (3 attempts total, ~3s waits,
// restart-only: same file, no regenerate, no re-check). When all 3
// fail it rolls back to beforeDoc and health-checks that. beforeDoc
// may be nil (no pre-round version) — then there is nothing to
// roll back to.
func (e *Engine) Apply(ctx context.Context, doc, beforeDoc []byte) *applyError {
	dir := filepath.Dir(e.Target)
	tmp, err := os.CreateTemp(dir, ".singbox-agent-sync-*.json")
	if err != nil {
		return &applyError{result: "error", err: fmt.Errorf("create temp config: %w", err)}
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(doc); err != nil {
		tmp.Close()
		return &applyError{result: "error", err: fmt.Errorf("write temp config: %w", err)}
	}
	if err := tmp.Close(); err != nil {
		return &applyError{result: "error", err: fmt.Errorf("close temp config: %w", err)}
	}
	if out, err := e.Check.Check(ctx, tmpPath); err != nil {
		return &applyError{result: "check_failed", err: fmt.Errorf("sing-box check failed: %s", firstLine(out, err))}
	}
	if err := os.Rename(tmpPath, e.Target); err != nil {
		return &applyError{result: "error", err: fmt.Errorf("swap config: %w", err)}
	}
	if e.Unit == "" {
		return nil
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return &applyError{result: "error", err: ctx.Err()}
			case <-time.After(3 * time.Second):
			}
		}
		if err := e.restartAndCheck(ctx, doc); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if ctx.Err() != nil {
			return &applyError{result: "error", err: ctx.Err()}
		}
	}
	if beforeDoc == nil {
		return &applyError{result: "restart_failed", err: fmt.Errorf("core restart/health failed after 3 attempts, no pre-round version to roll back to: %w", lastErr)}
	}
	if err := e.writeAtomic(beforeDoc); err != nil {
		return &applyError{result: "restart_failed", err: fmt.Errorf("core restart/health failed after 3 attempts; rollback write failed: %w (last restart error: %v)", err, lastErr), rollbackAttempted: true}
	}
	if err := e.restartAndCheck(ctx, beforeDoc); err != nil {
		return &applyError{result: "restart_failed", err: fmt.Errorf("core restart/health failed after 3 attempts; ROLLBACK ALSO FAILED (external cause suspected: port/disk/permission/core): %w (original: %v)", err, lastErr), rollbackAttempted: true}
	}
	return &applyError{result: "restart_failed", err: fmt.Errorf("core restart/health failed after 3 attempts; rolled back to pre-round version, core healthy on old config: %w", lastErr), rolledBack: true, rollbackAttempted: true}
}

// writeAtomic swaps doc into the target via temp file + rename, so
// the core never reads a half-written config.
// writeAtomic swaps doc into the target via temp file + rename, so
// the core never reads a half-written config.
func (e *Engine) writeAtomic(doc []byte) error {
	dir := filepath.Dir(e.Target)
	tmp, err := os.CreateTemp(dir, ".singbox-agent-sync-*.json")
	if err != nil {
		return fmt.Errorf("create temp config: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(doc); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp config: %w", err)
	}
	if err := os.Rename(tmpPath, e.Target); err != nil {
		return fmt.Errorf("swap config: %w", err)
	}
	return nil
}

// restartAndCheck restarts the unit and polls (every 500ms, up to
// 30s) until it reports active and the Clash API answers. It fails
// fast when the unit is definitively dead (failed/inactive) or the
// process flaps (MainPID changes between active samples), and
// requires TWO consecutive same-PID samples so a flapping process
// that briefly serves the Clash API never counts as healthy.
// restartAndCheck restarts the unit and polls (every 500ms, up to
// 30s) until it reports active and the Clash API answers. It fails
// fast when the unit is definitively dead (failed/inactive) or the
// process flaps (MainPID changes between active samples), and
// requires TWO consecutive same-PID samples so a flapping process
// that briefly serves the Clash API never counts as healthy.
func (e *Engine) restartAndCheck(ctx context.Context, doc []byte) error {
	if err := e.Restart.Restart(ctx); err != nil {
		return err
	}
	deadline := time.Now().Add(30 * time.Second)
	var stablePid string
	stableCount := 0
	for {
		state := e.UnitProbe.State(ctx)
		if state == "failed" || state == "inactive" {
			return fmt.Errorf("core unit is %s", state)
		}
		if state == "active" {
			if pid := e.UnitProbe.MainPID(ctx); pid != "" && pid != "0" {
				if stablePid == "" {
					stablePid = pid
					stableCount = 1
				} else if pid != stablePid {
					return fmt.Errorf("core process is flapping (MainPID changed %s -> %s)", stablePid, pid)
				} else {
					stableCount++
				}
				if stableCount >= 2 {
					if e.Clash == nil {
						return nil
					}
					if e.Clash.Alive(ctx) {
						e.checkPortsAdvisory(doc)
						return nil
					}
				}
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("core did not become healthy within 30s")
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// checkPortsAdvisory warns when a TCP listen port from the applied
// doc is not in LISTEN state. Best-effort and advisory-only: a
// miss never fails health (unit + Clash already proved the start).
// checkPortsAdvisory warns when a TCP listen port from the applied
// doc is not in LISTEN state. Best-effort and advisory-only: a
// miss never fails health (unit + Clash already proved the start).
func (e *Engine) checkPortsAdvisory(doc []byte) {
	if e.Warn == nil {
		return
	}
	parsed, err := parseDoc(doc)
	if err != nil {
		return
	}
	listening := ListeningPorts()
	for _, ib := range docInbounds(parsed) {
		typ, _ := ib["type"].(string)
		if typ == "hysteria2" || typ == "tuic" {
			continue // UDP-only: no TCP LISTEN to check
		}
		port := 0
		if f, ok := ib["listen_port"].(float64); ok {
			port = int(f)
		}
		if port > 0 && !listening.TCP[port] {
			e.Warn("applied doc port not listening (advisory only)", "port", port)
		}
	}
}

// Listening is the set of locally bound ports, read from /proc
func firstLine(out string, err error) string {
	out = strings.TrimSpace(out)
	if i := strings.IndexByte(out, '\n'); i >= 0 {
		out = out[:i]
	}
	if out == "" && err != nil {
		return err.Error()
	}
	return out
}

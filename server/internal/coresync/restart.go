package coresync

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

)

func (s *Service) restart(ctx context.Context) error {
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "systemctl", "restart", s.unit)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl restart %s: %s", s.unit, firstLine(string(out), err))
	}
	return nil
}

// restartAndCheck restarts the unit and polls (every 500ms, up to
// 30s) until it reports active and the Clash API answers. It fails
// fast when the unit is definitively dead (failed/inactive) instead
// of polling to the deadline: a core that failed to start is not
// coming back on its own, and a fast failure keeps the retry loop
// short. doc is the just-applied config; its TCP listen ports are
// checked advisory-only (a warning, never a failure — a parse miss
// must not trigger a false rollback; unit + Clash already prove the
// core started).
//
// PID-stability fail-fast: with Restart=on-failure, a core that
// fails to start flaps (systemd respawns it every ~100ms at the
// default interval), so is-active samples alternate active /
// activating and a 500ms poll can miss every brief "failed" window,
// burning the full 30s per attempt — 3 attempts + rollback then
// exceed the 90s backstop (observed on the test box: the sync died
// on the backstop with no rollback, no block, core left to systemd).
// A MainPID change between two active samples proves the process is
// flapping, so it fails fast. Success additionally requires TWO
// consecutive same-PID samples: a flapping process that briefly
// serves the Clash API must never count as healthy on a single
// lucky sample. A healthy start changes PID once (old -> new, after
// restart() returns the old process is dead) and then stays stable;
// a zero/empty PID is transient, never compared. Non-active samples
// do not reset the stable PID, so activating samples interleaved
// between two differing active samples still trip the detection.
// restartAndCheck restarts the unit and polls (every 500ms, up to
// 30s) until it reports active and the Clash API answers. It fails
// fast when the unit is definitively dead (failed/inactive) instead
// of polling to the deadline: a core that failed to start is not
// coming back on its own, and a fast failure keeps the retry loop
// short. doc is the just-applied config; its TCP listen ports are
// checked advisory-only (a warning, never a failure — a parse miss
// must not trigger a false rollback; unit + Clash already prove the
// core started).
//
// PID-stability fail-fast: with Restart=on-failure, a core that
// fails to start flaps (systemd respawns it every ~100ms at the
// default interval), so is-active samples alternate active /
// activating and a 500ms poll can miss every brief "failed" window,
// burning the full 30s per attempt — 3 attempts + rollback then
// exceed the 90s backstop (observed on the test box: the sync died
// on the backstop with no rollback, no block, core left to systemd).
// A MainPID change between two active samples proves the process is
// flapping, so it fails fast. Success additionally requires TWO
// consecutive same-PID samples: a flapping process that briefly
// serves the Clash API must never count as healthy on a single
// lucky sample. A healthy start changes PID once (old -> new, after
// restart() returns the old process is dead) and then stays stable;
// a zero/empty PID is transient, never compared. Non-active samples
// do not reset the stable PID, so activating samples interleaved
// between two differing active samples still trip the detection.
func (s *Service) restartAndCheck(ctx context.Context, doc []byte) error {
	if err := s.restart(ctx); err != nil {
		return err
	}
	deadline := time.Now().Add(30 * time.Second)
	var stablePid string
	stableCount := 0
	for {
		state := s.unitState(ctx)
		if state == "failed" || state == "inactive" {
			return fmt.Errorf("core unit is %s", state)
		}
		if state == "active" {
			if pid := s.unitMainPID(ctx); pid != "" && pid != "0" {
				if stablePid == "" {
					stablePid = pid
					stableCount = 1
				} else if pid != stablePid {
					return fmt.Errorf("core process is flapping (MainPID changed %s -> %s)", stablePid, pid)
				} else {
					stableCount++
				}
				if stableCount >= 2 {
					if s.clashAPI == "" {
						return nil
					}
					if s.clashAlive(ctx) {
						s.checkPortsAdvisory(doc)
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

// unitMainPID reports the unit's main PID ("0" when none). A command
// failure or empty output reports "" (poll, do not fail fast on it).
// unitMainPID reports the unit's main PID ("0" when none). A command
// failure or empty output reports "" (poll, do not fail fast on it).
func (s *Service) unitMainPID(ctx context.Context) string {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, _ := exec.CommandContext(cctx, "systemctl", "show", "-p", "MainPID", s.unit).CombinedOutput()
	line := strings.TrimSpace(string(out))
	if line == "" {
		return ""
	}
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	if v, ok := strings.CutPrefix(line, "MainPID="); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// unitState reports the unit's is-active state ("active", "failed",
// "inactive", "activating", ...). A command failure with no output
// reports "unknown" (poll, do not fail fast on it).
// unitState reports the unit's is-active state ("active", "failed",
// "inactive", "activating", ...). A command failure with no output
// reports "unknown" (poll, do not fail fast on it).
func (s *Service) unitState(ctx context.Context) string {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, _ := exec.CommandContext(cctx, "systemctl", "is-active", s.unit).CombinedOutput()
	state := strings.TrimSpace(string(out))
	if state == "" {
		return "unknown"
	}
	return state
}

// checkPortsAdvisory warns when a TCP listen port from the applied
// doc is not in LISTEN state (via /proc/net/tcp + tcp6, state
// observation only — no connection is opened). Best-effort: an
// unreadable table or an unparsable doc skips silently, and a miss
// never fails health (unit + Clash already proved the start).
// checkPortsAdvisory warns when a TCP listen port from the applied
// doc is not in LISTEN state (via /proc/net/tcp + tcp6, state
// observation only — no connection is opened). Best-effort: an
// unreadable table or an unparsable doc skips silently, and a miss
// never fails health (unit + Clash already proved the start).
func (s *Service) checkPortsAdvisory(doc []byte) {
	parsed, err := parseDoc(doc)
	if err != nil {
		return
	}
	want := map[int]bool{}
	for _, ib := range inboundList(parsed) {
		typ := asString(ib["type"])
		if typ == "hysteria2" || typ == "tuic" {
			continue // UDP-only: no TCP LISTEN to check
		}
		if port := portOf(ib["listen_port"]); port > 0 {
			want[port] = true
		}
	}
	if len(want) == 0 {
		return
	}
	listening := map[int]bool{}
	for _, path := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 4 || fields[0] == "sl" {
				continue
			}
			if fields[3] != "0A" { // LISTEN
				continue
			}
			parts := strings.Split(fields[1], ":")
			if len(parts) != 2 {
				continue
			}
			var port int
			if _, err := fmt.Sscanf(parts[1], "%x", &port); err == nil {
				listening[port] = true
			}
		}
	}
	for port := range want {
		if !listening[port] {
			s.warn("applied doc port not listening (advisory only)", "port", port)
		}
	}
}
func (s *Service) clashAlive(ctx context.Context) bool {
	cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, strings.TrimSuffix(s.clashAPI, "/")+"/version", nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode == http.StatusOK
}

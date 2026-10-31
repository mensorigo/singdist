package agent

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// execChecker runs `bin check -c path` (20s timeout).
type execChecker struct{ bin string }

func (c execChecker) Check(ctx context.Context, path string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, c.bin, "check", "-c", path)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// systemctlRestarter runs `systemctl restart unit` (30s timeout).
// systemctlRestarter runs `systemctl restart unit` (30s timeout).
type systemctlRestarter struct{ unit string }

func (r systemctlRestarter) Restart(ctx context.Context) error {
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "systemctl", "restart", r.unit)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl restart %s: %s", r.unit, firstLine(string(out), err))
	}
	return nil
}

// systemctlProber reports is-active state and MainPID.
// systemctlProber reports is-active state and MainPID.
type systemctlProber struct{ unit string }

func (p systemctlProber) State(ctx context.Context) string {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, _ := exec.CommandContext(cctx, "systemctl", "is-active", p.unit).CombinedOutput()
	state := strings.TrimSpace(string(out))
	if state == "" {
		return "unknown"
	}
	return state
}
func (p systemctlProber) MainPID(ctx context.Context) string {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, _ := exec.CommandContext(cctx, "systemctl", "show", "-p", "MainPID", p.unit).CombinedOutput()
	line := strings.TrimSpace(string(out))
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	if v, ok := strings.CutPrefix(line, "MainPID="); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// httpClashProber GETs <clashAddr>/version and requires HTTP 200.
// httpClashProber GETs <clashAddr>/version and requires HTTP 200.
type httpClashProber struct{ addr string }

func (p httpClashProber) Alive(ctx context.Context) bool {
	cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, strings.TrimSuffix(p.addr, "/")+"/version", nil)
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

// NewProductionEngine builds an Engine wired to the real core binary,
// systemd unit and Clash API. Pass unit "" to disable restarts.
// NewProductionEngine builds an Engine wired to the real core binary,
// systemd unit and Clash API. Pass unit "" to disable restarts.
func NewProductionEngine(target, bin, unit, clashAddr string) *Engine {
	e := &Engine{Target: target, Bin: bin, Unit: unit, ClashAddr: clashAddr}
	e.Check = execChecker{bin: bin}
	if unit != "" {
		e.Restart = systemctlRestarter{unit: unit}
		e.UnitProbe = systemctlProber{unit: unit}
	}
	if clashAddr != "" {
		e.Clash = httpClashProber{addr: clashAddr}
	}
	return e
}

// CheckDoc validates a doc without writing anything. It reports
// whether the check actually ran: when the check binary cannot even
// start, callers keep their previous optimistic behaviour instead
// of failing an apply that writes nothing.

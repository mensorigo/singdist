// Package coresync is the sync engine: it regenerates the sing-box
// config from the panel's desired state, validates it with the core
// binary, swaps it in atomically, optionally restarts the core unit,
// health-checks the result and restores the previous file on failure.
// Every attempt is logged. (An archived-versions feature with rollback
// used to live here; it was removed 2026-10-24 — every sync archived
// a config copy, so a short auto-sync interval would pile up archive
// data for no real benefit.)
//
// Safety shape: the write target (TargetPath) and the restart unit are
// configuration, not constants. With no unit configured the engine
// writes and validates but never restarts anything — that is how the
// shadow instance exercises it against a sandbox file.
package coresync

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"singbox-ui/server/internal/certs"
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

func errNotFound(what string) *Error {
	return &Error{Status: 404, Code: what + "_not_found", Message: what + " not found"}
}

type Config struct {
	Bin        string // sing-box binary for `check`
	TargetPath string // config file the engine owns
	CoreUnit   string // systemd unit to restart; "" = never restart
	ClashAPI   string // base URL for the health check, e.g. http://127.0.0.1:9090
	LogPath    string // panel-managed core log file (log.output); "" = omit the field (legacy shape)
}

type Service struct {
	st        *store.Store
	settings  *settings.Service
	certs     *certs.Manager
	bin       string
	target    string
	unit      string
	clashAddr string // host:port for the generated clash_api block
	clashAPI  string
	logPath   string
	mu        sync.Mutex
}

func NewService(st *store.Store, settingsSvc *settings.Service, certMgr *certs.Manager, cfg Config) *Service {
	clashAddr := strings.TrimPrefix(strings.TrimPrefix(cfg.ClashAPI, "http://"), "https://")
	return &Service{
		st:        st,
		settings:  settingsSvc,
		certs:     certMgr,
		bin:       cfg.Bin,
		target:    cfg.TargetPath,
		unit:      cfg.CoreUnit,
		clashAddr: clashAddr,
		clashAPI:  cfg.ClashAPI,
		logPath:   cfg.LogPath,
	}
}

func nowUnix() int64 { return time.Now().Unix() }

// Status is the engine's report for the panel.
type Status struct {
	TargetPath     string            `json:"targetPath"`
	RestartEnabled bool              `json:"restartEnabled"`
	DesiredHash    string            `json:"desiredHash"`
	TargetHash     string            `json:"targetHash"`
	InSync         bool              `json:"inSync"`
	Last           *store.SyncLogRow `json:"last"`
}

func (s *Service) Status(ctx context.Context) (Status, error) {
	_, desiredHash, err := s.Generate(ctx)
	if err != nil {
		return Status{}, err
	}
	targetHash := ""
	if cur, err := os.ReadFile(s.target); err == nil {
		targetHash = hashBytes(cur)
	}
	logs, err := s.st.ListSyncLogs(ctx, 1)
	if err != nil {
		return Status{}, err
	}
	var last *store.SyncLogRow
	if len(logs) > 0 {
		last = &logs[0]
	}
	return Status{
		TargetPath:     s.target,
		RestartEnabled: s.unit != "",
		DesiredHash:    desiredHash,
		TargetHash:     targetHash,
		InSync:         targetHash != "" && targetHash == desiredHash,
		Last:           last,
	}, nil
}

// Sync runs the full pipeline once.
func (s *Service) Sync(ctx context.Context) (store.SyncLogRow, error) {
	return s.syncInternal(ctx, "sync")
}

// AutoSync is the periodic ticker's entry point: it compares the
// desired config against the target first and only runs the pipeline
// when they differ, so idle ticks leave no log rows and no restarts.
// It reports whether a sync actually ran.
func (s *Service) AutoSync(ctx context.Context) (store.SyncLogRow, bool, error) {
	st, err := s.Status(ctx)
	if err != nil {
		return store.SyncLogRow{}, false, err
	}
	if st.InSync {
		return store.SyncLogRow{}, false, nil
	}
	row, err := s.syncInternal(ctx, "auto-sync")
	return row, true, err
}

func (s *Service) syncInternal(ctx context.Context, action string) (store.SyncLogRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	doc, hash, err := s.Generate(ctx)
	if err != nil {
		return s.log(ctx, action, "error", "", err.Error())
	}

	if cur, err := os.ReadFile(s.target); err == nil && hashBytes(cur) == hash {
		return s.log(ctx, action, "no_change", hash, "generated config matches the target file")
	}

	// The core opens the log file on restart, so its directory must
	// exist before the config lands — otherwise the restart fails and
	// the pipeline restores the previous file for nothing.
	if s.logPath != "" {
		if err := os.MkdirAll(filepath.Dir(s.logPath), 0o755); err != nil {
			return s.log(ctx, action, "error", hash, "create log directory: "+err.Error())
		}
	}

	if err := s.apply(ctx, doc); err != nil {
		return s.log(ctx, action, err.result, hash, err.Error())
	}
	detail := "config applied"
	if s.unit == "" {
		detail = "config written and validated (core restart not configured)"
	}
	return s.log(ctx, action, "applied", hash, detail)
}

// ListLog backs the panel's sync log view.
func (s *Service) ListLog(ctx context.Context, limit int) ([]store.SyncLogRow, error) {
	return s.st.ListSyncLogs(ctx, limit)
}

// --- pipeline internals -------------------------------------------------------

type applyError struct {
	result string
	err    error
}

func (e *applyError) Error() string { return e.err.Error() }

// apply validates doc, atomically swaps it in, and (when a unit is
// configured) restarts the core and health-checks it; on restart or
// health failure it restores the previous file and restarts again.
func (s *Service) apply(ctx context.Context, doc []byte) *applyError {
	dir := filepath.Dir(s.target)
	tmp, err := os.CreateTemp(dir, ".singbox-ui-sync-*.json")
	if err != nil {
		return &applyError{"error", fmt.Errorf("create temp config: %w", err)}
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(doc); err != nil {
		tmp.Close()
		return &applyError{"error", fmt.Errorf("write temp config: %w", err)}
	}
	if err := tmp.Close(); err != nil {
		return &applyError{"error", fmt.Errorf("close temp config: %w", err)}
	}

	if out, err := s.runCheck(ctx, tmpPath); err != nil {
		return &applyError{"check_failed", fmt.Errorf("sing-box check failed: %s", firstLine(out, err))}
	}

	prev, prevErr := os.ReadFile(s.target)
	if err := os.Rename(tmpPath, s.target); err != nil {
		return &applyError{"error", fmt.Errorf("swap config: %w", err)}
	}

	if s.unit == "" {
		return nil
	}
	if err := s.restartAndCheck(ctx); err != nil {
		// Roll back to the previous file and restart once more.
		if prevErr == nil {
			_ = os.WriteFile(s.target, prev, 0o644)
			_ = s.restart(ctx)
		}
		return &applyError{"restart_failed", fmt.Errorf("core restart/health failed, previous config restored: %w", err)}
	}
	return nil
}

func (s *Service) runCheck(ctx context.Context, path string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, s.bin, "check", "-c", path)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

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

// restartAndCheck restarts the unit and waits until it reports active
// and the Clash API answers.
func (s *Service) restartAndCheck(ctx context.Context) error {
	if err := s.restart(ctx); err != nil {
		return err
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		active := false
		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		out, err := exec.CommandContext(cctx, "systemctl", "is-active", s.unit).CombinedOutput()
		cancel()
		if err == nil && strings.TrimSpace(string(out)) == "active" {
			active = true
		}
		if active {
			if s.clashAPI == "" {
				return nil
			}
			if s.clashAlive(ctx) {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("core did not become healthy within 10s")
		}
		time.Sleep(500 * time.Millisecond)
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

func (s *Service) log(ctx context.Context, action, result, hash, detail string) (store.SyncLogRow, error) {
	row := store.SyncLogRow{At: nowUnix(), Action: action, Result: result, Hash: hash, Detail: detail}
	id, err := s.st.AddSyncLog(ctx, row)
	if err != nil {
		return row, err
	}
	row.ID = id
	return row, nil
}

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

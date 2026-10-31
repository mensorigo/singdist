package coresync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"singbox-ui/server/internal/store"
)

type applyError struct {
	result string
	err    error
	// rolledBack reports whether a post-write rollback to beforeDoc
	// succeeded and the core is healthy on it. rollbackAttempted
	// reports whether a rollback was attempted at all (false when
	// there was no pre-round version to roll back to).
	rolledBack        bool
	rollbackAttempted bool
}

func (e *applyError) Error() string { return e.err.Error() }

// apply validates doc, atomically swaps it in, and (when a unit is
// configured) restarts the core and health-checks it; on restart or
// health failure it restores the previous file and restarts again.
// checkOnly validates a merged doc without writing or restarting
// anything. It reports whether the check actually ran: when the check
// binary cannot even start, callers keep their previous optimistic
// behaviour instead of failing a sync that writes nothing.
// apply validates doc, atomically swaps it in, and (when a unit is
// configured) restarts the core and health-checks it; on restart or
// health failure it restores the previous file and restarts again.
// checkOnly validates a merged doc without writing or restarting
// anything. It reports whether the check actually ran: when the check
// binary cannot even start, callers keep their previous optimistic
// behaviour instead of failing a sync that writes nothing.
func (s *Service) checkOnly(ctx context.Context, doc map[string]any) (ran, valid bool) {
	raw, _ := marshalDoc(doc)
	dir := filepath.Dir(s.target)
	tmp, err := os.CreateTemp(dir, ".singbox-ui-check-*.json")
	if err != nil {
		return false, false
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return false, false
	}
	if err := tmp.Close(); err != nil {
		return false, false
	}
	_, err = s.runCheck(ctx, tmpPath)
	if err != nil {
		var execErr *exec.Error
		if errors.As(err, &execErr) {
			return false, false
		}
		return true, false
	}
	return true, true
}

// apply validates doc, atomically swaps it in, and (when a unit is
// configured) restarts the core and health-checks it. A restart or
// health failure is retried twice (3 attempts total, ~3s waits,
// restart-only: same file, no regenerate, no re-check) because some
// failures are transient (old process still releasing a port).
// When all 3 fail it rolls back to beforeDoc (the pre-round version,
// snapshotted once before this round) and health-checks that: a
// rollback success means the core runs the old config again (the
// round still reports restart_failed, with the input blocked); a
// rollback failure means the cause is outside this round's changes
// (port/disk/permission/core) and the file is left at beforeDoc.
// beforeDoc may be nil (no pre-round version: first sync with no
// baseline and no file) — then there is nothing to roll back to.
// apply validates doc, atomically swaps it in, and (when a unit is
// configured) restarts the core and health-checks it. A restart or
// health failure is retried twice (3 attempts total, ~3s waits,
// restart-only: same file, no regenerate, no re-check) because some
// failures are transient (old process still releasing a port).
// When all 3 fail it rolls back to beforeDoc (the pre-round version,
// snapshotted once before this round) and health-checks that: a
// rollback success means the core runs the old config again (the
// round still reports restart_failed, with the input blocked); a
// rollback failure means the cause is outside this round's changes
// (port/disk/permission/core) and the file is left at beforeDoc.
// beforeDoc may be nil (no pre-round version: first sync with no
// baseline and no file) — then there is nothing to roll back to.
func (s *Service) apply(ctx context.Context, doc, beforeDoc []byte) *applyError {
	dir := filepath.Dir(s.target)
	tmp, err := os.CreateTemp(dir, ".singbox-ui-sync-*.json")
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

	if out, err := s.runCheck(ctx, tmpPath); err != nil {
		return &applyError{result: "check_failed", err: fmt.Errorf("sing-box check failed: %s", firstLine(out, err))}
	}

	if err := os.Rename(tmpPath, s.target); err != nil {
		return &applyError{result: "error", err: fmt.Errorf("swap config: %w", err)}
	}

	if s.unit == "" {
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
		if err := s.restartAndCheck(ctx, doc); err == nil {
			if attempt > 0 {
				s.logger.Info("coresync: restart succeeded on retry", "attempt", attempt+1)
			}
			return nil
		} else {
			lastErr = err
		}
		if ctx.Err() != nil {
			// Overall timeout mid-retry: abort without rollback
			// (state unknown); the caller logs it and the input
			// gating prevents a same-input retry loop.
			return &applyError{result: "error", err: ctx.Err()}
		}
	}
	if beforeDoc == nil {
		return &applyError{result: "restart_failed", err: fmt.Errorf("core restart/health failed after 3 attempts, no pre-round version to roll back to: %w", lastErr)}
	}
	if err := s.writeAtomic(beforeDoc); err != nil {
		return &applyError{result: "restart_failed", err: fmt.Errorf("core restart/health failed after 3 attempts; rollback write failed: %w (last restart error: %v)", err, lastErr), rollbackAttempted: true}
	}
	if err := s.restartAndCheck(ctx, beforeDoc); err != nil {
		return &applyError{result: "restart_failed", err: fmt.Errorf("core restart/health failed after 3 attempts; ROLLBACK ALSO FAILED (external cause suspected: port/disk/permission/core): %w (original: %v)", err, lastErr), rollbackAttempted: true}
	}
	return &applyError{result: "restart_failed", err: fmt.Errorf("core restart/health failed after 3 attempts; rolled back to pre-round version, core healthy on old config: %w", lastErr), rolledBack: true, rollbackAttempted: true}
}

// writeAtomic swaps doc into the target via temp file + rename, so
// the core never reads a half-written config (and a write failure
// leaves the previous file untouched).
// writeAtomic swaps doc into the target via temp file + rename, so
// the core never reads a half-written config (and a write failure
// leaves the previous file untouched).
func (s *Service) writeAtomic(doc []byte) error {
	dir := filepath.Dir(s.target)
	tmp, err := os.CreateTemp(dir, ".singbox-ui-sync-*.json")
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
	if err := os.Rename(tmpPath, s.target); err != nil {
		return fmt.Errorf("swap config: %w", err)
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

// applyAndRecord applies a panel-only doc (used by the missing-file
// and illegal-file paths, which bypass classification) and records
// the sync row, change rows, baseline and audit marking.
func (s *Service) applyAndRecord(ctx context.Context, action string, beforeID int64, doc []byte, hash string, beforeDoc []byte, detail string, rows []change) (store.SyncLogRow, error) {
	if s.logPath != "" {
		if err := os.MkdirAll(filepath.Dir(s.logPath), 0o755); err != nil {
			return s.log(ctx, action, "error", hash, "create log directory: "+err.Error())
		}
	}
	if aerr := s.apply(ctx, doc, beforeDoc); aerr != nil {
		if aerr.result == "restart_failed" {
			// Same rollback/block treatment as the main path: the
			// round failed after writing, so block this input.
			note := change{action: changeRestoreNote, item: "配置文件", detail: "重启失败，已回退到本轮之前的版本"}
			if !aerr.rolledBack {
				note.detail = "重启失败，且回退也失败（疑似外部原因）；文件保持旧版本，已停止重试"
			}
			row, rerr := s.log(ctx, action, "restart_failed", hash, aerr.Error())
			if rerr == nil {
				s.writeChangeRows(ctx, row.ID, append(append([]change{}, rows...), note))
				s.setBlockedHash(ctx, hash)
			}
			return row, rerr
		}
		return s.log(ctx, action, aerr.result, hash, aerr.Error())
	}
	row, rerr := s.log(ctx, action, "applied", hash, detail)
	if rerr == nil {
		s.writeChangeRows(ctx, row.ID, rows)
		_ = s.st.SetSyncState(ctx, store.SyncState{LastDoc: string(doc), LastHash: hash, UpdatedAt: nowUnix()})
		s.setBlockedHash(ctx, "")
		_ = s.st.MarkPendingAuditsApplied(ctx, beforeID, hash)
	}
	return row, rerr
}

// mergedPassTags recomputes the passthrough tag set from a merged
// doc: tags present in the doc that are not panel rows.
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

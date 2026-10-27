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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"singbox-ui/server/internal/certs"
	"singbox-ui/server/internal/inbounds"
	"singbox-ui/server/internal/settings"
	"singbox-ui/server/internal/store"
	"singbox-ui/server/internal/subs"
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
	Bin        string       // sing-box binary for `check`
	TargetPath string       // config file the engine owns
	CoreUnit   string       // systemd unit to restart; "" = never restart
	ClashAPI   string       // base URL for the health check, e.g. http://127.0.0.1:9090
	LogPath    string       // panel-managed core log file (log.output); "" = omit the field (legacy shape)
	Log        *slog.Logger // optional; best-effort warnings only, never fails a sync
}

type Service struct {
	st          *store.Store
	settings    *settings.Service
	certs       *certs.Manager
	inboundsSvc *inbounds.Service
	subsSvc     *subs.Service
	bin         string
	target      string
	unit        string
	clashAddr   string // host:port for the generated clash_api block
	clashAPI    string
	logPath     string
	logger      *slog.Logger
	mu          sync.Mutex
}

func NewService(st *store.Store, settingsSvc *settings.Service, certMgr *certs.Manager, cfg Config) *Service {
	clashAddr := strings.TrimPrefix(strings.TrimPrefix(cfg.ClashAPI, "http://"), "https://")
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		st:          st,
		settings:    settingsSvc,
		certs:       certMgr,
		inboundsSvc: inbounds.NewService(st, cfg.TargetPath),
		subsSvc:     subs.NewService(st),
		bin:         cfg.Bin,
		target:      cfg.TargetPath,
		unit:        cfg.CoreUnit,
		clashAddr:   clashAddr,
		clashAPI:    cfg.ClashAPI,
		logPath:     cfg.LogPath,
		logger:      log,
	}
}

func (s *Service) warn(msg string, args ...any) {
	if s.logger != nil {
		s.logger.Warn("coresync: "+msg, args...)
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
	genDoc, genHash, err := s.Generate(ctx)
	if err != nil {
		return Status{}, err
	}
	genParsed, _ := parseDoc(genDoc)
	logs, err := s.st.ListSyncLogs(ctx, 1)
	if err != nil {
		return Status{}, err
	}
	var last *store.SyncLogRow
	if len(logs) > 0 {
		last = &logs[0]
	}
	base := Status{
		TargetPath:     s.target,
		RestartEnabled: s.unit != "",
		DesiredHash:    genHash,
		Last:           last,
	}
	fileBytes, ferr := os.ReadFile(s.target)
	if ferr != nil {
		return base, nil // missing/unreadable target: out of sync, panel-only desired
	}
	base.TargetHash = hashBytes(fileBytes)
	fileParsed, perr := parseDoc(fileBytes)
	if perr != nil {
		return base, nil // unparseable target: out of sync, panel-only desired
	}
	// Best-effort merged comparison. Any load failure falls back to
	// panel-only vs file — Status is read-only and must never fail
	// the top-bar dot.
	inbounds, err1 := s.st.ListInbounds(ctx)
	subscriptions, err2 := s.st.ListSubscriptions(ctx)
	cfg, err3 := s.settings.Get(ctx)
	baseline, err4 := s.st.GetSyncState(ctx)
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
		base.InSync = semanticEqual(genParsed, fileParsed)
		return base, nil
	}
	oldParsed := genParsed
	if baseline.LastDoc != "" {
		if p, err := parseDoc([]byte(baseline.LastDoc)); err == nil {
			oldParsed = p
		}
	}
	panel := buildPanelState(inbounds, subscriptions, cfg.PublicHost, baseline.PassthroughTags)
	res := classify(oldParsed, fileParsed, panel, baseline.LastDoc == "")
	merged := mergeConfig(genParsed, res.passes)
	_, mergedHash := marshalDoc(merged)
	base.DesiredHash = mergedHash
	base.InSync = semanticEqual(merged, fileParsed)
	if base.InSync && baseline.LastDoc != "" {
		// The file matching the merge does not mean the core runs
		// it: a file-driven change (passthrough add, file-wins
		// import) makes merged == file while the core still runs
		// the last applied doc. Only report in sync when the last
		// applied doc matches too, so the checker does not skip an
		// auto-sync the core still needs.
		if lastApplied, err := parseDoc([]byte(baseline.LastDoc)); err == nil && !semanticEqual(merged, lastApplied) {
			base.InSync = false
		}
	}
	if base.InSync {
		// A hand-edited file can be semantically equal to the merge
		// yet invalid (illegal passthrough content). Reporting it in
		// sync would leave a config the core cannot start with behind
		// a green dot, and auto-sync would keep skipping it. When the
		// check binary runs, validity is part of InSync; when it
		// cannot run, fall back to the semantic result.
		if ran, valid := s.checkOnly(ctx, merged); ran && !valid {
			base.InSync = false
		}
	}
	return base, nil
}

// Sync runs the full pipeline once (manual path: it bypasses the
// auto-sync input block — an explicit manual retry is an override).
func (s *Service) Sync(ctx context.Context) (store.SyncLogRow, error) {
	return s.syncInternal(ctx, "sync")
}

// AutoSync is the debouncer's entry point: it compares the desired
// config against the target first and only runs the pipeline when
// they differ, so idle pushes leave no log rows and no restarts.
// It reports whether a sync actually ran. A busy pipeline (another
// sync holding the lock) reports ran=false with a "skipped" row so
// the debouncer retries soon instead of queueing. An input that
// failed at runtime and was rolled back is skipped until it changes.
func (s *Service) AutoSync(ctx context.Context) (store.SyncLogRow, bool, error) {
	st, err := s.Status(ctx)
	if err != nil {
		return store.SyncLogRow{}, false, err
	}
	if st.InSync {
		// Converged while a block was set (e.g., the operator
		// removed the conflicting file content by hand): the
		// blocking input is gone, so the block must not linger
		// stale. No row: InSync produces no rows by design. (The
		// drift poll triggers a run for exactly this case, since
		// InSync alone never notifies.)
		if baseline, berr := s.st.GetSyncState(ctx); berr == nil && baseline.BlockedHash != "" {
			_ = s.st.SetBlockedHash(ctx, "")
			s.logger.Info("coresync: auto-sync cleared stale input block (converged)", "blocked", baseline.BlockedHash)
		}
		return store.SyncLogRow{}, false, nil
	}
	if baseline, berr := s.st.GetSyncState(ctx); berr == nil && baseline.BlockedHash != "" && baseline.BlockedHash == st.DesiredHash {
		s.logger.Info("coresync: auto-sync skipped (input blocked after rollback)", "hash", st.DesiredHash)
		return store.SyncLogRow{}, false, nil
	}
	row, err := s.syncInternal(ctx, "auto-sync")
	if row.Result == "skipped" {
		return row, false, err
	}
	return row, true, err
}

func (s *Service) syncInternal(ctx context.Context, action string) (store.SyncLogRow, error) {
	// Try-lock: one sync at a time, whole pipeline. A busy pipeline
	// reports "skipped" (logged) instead of queueing; the next
	// tick/debounce picks the changes up (they persist in the DB).
	if !s.mu.TryLock() {
		s.logger.Info("coresync: sync skipped (previous sync still running)")
		return s.log(context.Background(), action, "skipped", "", "previous sync still running; next tick/debounce picks it up")
	}
	defer s.mu.Unlock()

	// Overall backstop: 90s. Steps have their own timeouts (check
	// 20s, restart 30s, health 30s); the overall ctx aborts a
	// pathological hang. Bookkeeping of a completed apply still runs
	// (with a live ctx) so a timeout after a successful restart does
	// not leave a stale baseline behind.
	ctx90, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	ctx = ctx90

	// Snapshot the audit watermark before the pipeline runs; the
	// post-sync marking below must only cover rows that existed
	// before this sync started (see MaxAuditID).
	beforeID, _ := s.st.MaxAuditID(ctx)

	// Panel inputs, needed by every path below.
	inbounds, err := s.st.ListInbounds(ctx)
	if err != nil {
		return s.log(ctx, action, "error", "", "list inbounds: "+err.Error())
	}
	subscriptions, err := s.st.ListSubscriptions(ctx)
	if err != nil {
		return s.log(ctx, action, "error", "", "list subscriptions: "+err.Error())
	}
	cfg, err := s.settings.Get(ctx)
	if err != nil {
		return s.log(ctx, action, "error", "", "read settings: "+err.Error())
	}
	baseline, _ := s.st.GetSyncState(ctx)

	genDoc, genHash, err := s.Generate(ctx)
	if err != nil {
		return s.log(ctx, action, "error", "", err.Error())
	}
	genParsed, _ := parseDoc(genDoc)

	// Two slots for this round: before (the pre-round version — the
	// baseline when one exists, else the current file, which is what
	// the core runs) and after (the merged doc below). The before
	// slot is the rollback target; it is snapshotted once, before
	// any restart, so rollback never re-derives anything.
	var beforeDoc []byte
	if baseline.LastDoc != "" {
		beforeDoc = []byte(baseline.LastDoc)
	}

	// Missing target: a deleted file is NOT "the user deleted
	// everything" — it bypasses classification entirely and is
	// recreated from the panel, with a note in the change log.
	fileBytes, ferr := os.ReadFile(s.target)
	if os.IsNotExist(ferr) {
		return s.applyAndRecord(ctx, action, beforeID, genDoc, genHash, beforeDoc,
			"配置文件缺失，已用面板配置重建",
			[]change{{action: changeRestoreNote, item: "配置文件", detail: "目标文件缺失（不是文件改动），已按面板配置重建；分类已跳过"}})
	}
	if ferr != nil {
		return s.log(ctx, action, "error", "", "read target: "+ferr.Error())
	}
	if beforeDoc == nil {
		// No baseline: the running config is the file on disk.
		beforeDoc = fileBytes
	}
	// Invalid JSON: the whole file is illegal — panel-only overwrite.
	fileParsed, perr := parseDoc(fileBytes)
	if perr != nil {
		return s.applyAndRecord(ctx, action, beforeID, genDoc, genHash, beforeDoc,
			"配置文件不是合法 JSON，已用面板配置覆盖："+perr.Error(),
			[]change{{action: changeDroppedIllegal, item: "配置文件", detail: "文件不是合法 JSON，已用面板配置覆盖：" + perr.Error()}})
	}

	// First import: no baseline means the panel has never synced.
	// The file's panel-supported content is imported into the panel
	// ONCE (backup first, import report in the change log); after
	// this round the baseline exists and later syncs are panel-wins
	// (file edits to managed content revert, never import).
	firstImport := baseline.LastDoc == ""
	var oldParsed map[string]any
	if firstImport {
		oldParsed = map[string]any{}
		s.backupFirstImport(fileBytes)
	} else if p, err := parseDoc([]byte(baseline.LastDoc)); err == nil {
		oldParsed = p
	} else {
		// Corrupt baseline (panel-written docs should not corrupt):
		// diff against empty so every file value reads as new, but
		// never import — panel-wins still holds, and success resets
		// the baseline to the new merged doc.
		s.warn("baseline unparseable, diffing against empty", "error", err)
		oldParsed = map[string]any{}
	}
	panel := buildPanelState(inbounds, subscriptions, cfg.PublicHost, baseline.PassthroughTags)
	res := classify(oldParsed, fileParsed, panel, firstImport)

	// Apply imports (first import only; post-baseline classification
	// produces no imports by construction).
	var importOutcomes []change
	if len(res.imports) > 0 {
		importOutcomes = s.applyImports(ctx, res.imports)
	}

	// Regenerate after imports, then merge the passthrough. With no
	// imports the panel state is unchanged, so regeneration would
	// reproduce the same doc — skip it.
	genDoc2 := genDoc
	genParsed2 := genParsed
	if len(res.imports) > 0 {
		var err error
		genDoc2, _, err = s.Generate(ctx)
		if err != nil {
			// Panel state already changed; the file did not. The
			// import rows are real, so they are still recorded —
			// but the passthrough/revert rows describe a merge that
			// never landed, so they are dropped here.
			row, rerr := s.log(ctx, action, "error", "", "regenerate after imports: "+err.Error())
			if rerr == nil {
				s.writeChangeRows(ctx, row.ID, importOutcomes)
			}
			return row, rerr
		}
		genParsed2, _ = parseDoc(genDoc2)
	}
	merged := mergeConfig(genParsed2, res.passes)
	mergedDoc, mergedHash := marshalDoc(merged)

	catchUp := false
	if semanticEqual(merged, fileParsed) {
		// The file matches the desired config, but a hand-edited file
		// was never validated: an illegal passthrough that happens to
		// equal the merge must not be reported in sync. Validate
		// first; on failure fall through to the apply path below,
		// which re-runs the check and triggers the panel-only
		// fallback. When the check binary cannot run at all, keep the
		// previous optimistic behaviour.
		if ran, valid := s.checkOnly(ctx, merged); ran && !valid {
			goto applyMerged
		}
		// The file matching the merge does NOT mean the core runs
		// it: a file-driven change (passthrough add, first-import
		// content) makes merged == file while the core still runs
		// the last applied doc. Only skip activation when the last
		// applied doc matches too; otherwise fall through to apply
		// (the file already holds the content, but the core must
		// restart to catch up). With no baseline yet (first sync)
		// there is nothing to compare against, so keep the old
		// behaviour for that one case.
		if baseline.LastDoc != "" {
			if lastApplied, err := parseDoc([]byte(baseline.LastDoc)); err == nil && !semanticEqual(merged, lastApplied) {
				catchUp = true
				goto applyMerged
			}
		}
		// No write, no restart. The file already holds everything
		// (including passthrough) and the core already runs it, so
		// the baseline becomes the on-disk truth and the full change
		// rows apply. The pending audits are marked applied too:
		// the file already contains the panel's current state, and
		// the running config matches it.
		row, rerr := s.log(ctx, action, "no_change", mergedHash, "merged config matches the target file")
		if rerr == nil {
			all := append(append([]change{}, res.changes...), importOutcomes...)
			s.writeChangeRows(ctx, row.ID, all)
			_ = s.st.SetSyncState(ctx, store.SyncState{LastDoc: string(fileBytes), LastHash: hashBytes(fileBytes), PassthroughTags: s.mergedPassTags(ctx, merged), UpdatedAt: nowUnix()})
			_ = s.st.SetBlockedHash(ctx, "")
			_ = s.st.MarkPendingAuditsApplied(ctx, beforeID, mergedHash)
		}
		return row, rerr
	}
applyMerged:

	// The core opens the log file on restart, so its directory must
	// exist before the config lands — otherwise the restart fails and
	// the pipeline restores the previous file for nothing.
	if s.logPath != "" {
		if err := os.MkdirAll(filepath.Dir(s.logPath), 0o755); err != nil {
			row, rerr := s.log(ctx, action, "error", mergedHash, "create log directory: "+err.Error())
			if rerr == nil {
				s.writeChangeRows(ctx, row.ID, importOutcomes)
			}
			return row, rerr
		}
	}

	if ctx.Err() != nil {
		return s.log(ctx, action, "error", mergedHash, "sync timed out after 90s before apply")
	}
	if aerr := s.apply(ctx, mergedDoc, beforeDoc); aerr != nil {
		if aerr.result == "check_failed" {
			// Illegal passthrough content must never block a
			// legitimate sync: fall back to panel-only.
			panelOnlyDoc, panelOnlyHash := marshalDoc(genParsed2)
			if aerr2 := s.apply(ctx, panelOnlyDoc, beforeDoc); aerr2 != nil {
				row, rerr := s.log(ctx, action, aerr2.result, panelOnlyHash, aerr2.Error())
				if rerr == nil {
					s.writeChangeRows(ctx, row.ID, importOutcomes)
				}
				return row, rerr
			}
			row, rerr := s.log(ctx, action, "applied", panelOnlyHash, "merged config failed check ("+aerr.Error()+"); applied panel-only, passthrough dropped")
			if rerr == nil {
				rows := append(append([]change{}, importOutcomes...), change{action: changeDroppedIllegal, item: "透传内容", detail: "合并配置校验失败，已丢弃透传内容并用面板配置覆盖：" + aerr.Error()})
				s.writeChangeRows(ctx, row.ID, rows)
				_ = s.st.SetSyncState(ctx, store.SyncState{LastDoc: string(panelOnlyDoc), LastHash: panelOnlyHash, UpdatedAt: nowUnix()})
				_ = s.st.SetBlockedHash(ctx, "")
				_ = s.st.MarkPendingAuditsApplied(ctx, beforeID, panelOnlyHash)
			}
			return row, rerr
		}
		if aerr.result == "restart_failed" {
			// Post-write runtime failure: apply() already retried
			// and rolled back (or attempted to). The round failed:
			// block this input from auto-retry until it changes,
			// keep the baseline at the pre-round version, and leave
			// the panel audits pending (the core runs the old
			// config, so the edits are NOT applied).
			rows := append(append([]change{}, res.changes...), importOutcomes...)
			if aerr.rollbackAttempted {
				if aerr.rolledBack {
					rows = append(rows, change{action: changeRestoreNote, item: "配置文件", detail: "重启失败 3 次，已回退到本轮之前的版本并重启成功；核心运行旧配置"})
				} else {
					rows = append(rows, change{action: changeRestoreNote, item: "配置文件", detail: "重启失败 3 次，且回退也失败（疑似外部原因：端口/磁盘/权限/核心）；文件保持旧版本，已停止重试"})
				}
			}
			row, rerr := s.log(ctx, action, "restart_failed", mergedHash, aerr.Error())
			if rerr == nil {
				s.writeChangeRows(ctx, row.ID, rows)
				_ = s.st.SetBlockedHash(ctx, mergedHash)
			}
			return row, rerr
		}
		row, rerr := s.log(ctx, action, aerr.result, mergedHash, aerr.Error())
		if rerr == nil {
			s.writeChangeRows(ctx, row.ID, importOutcomes)
		}
		return row, rerr
	}

	detail := "config applied"
	if s.unit == "" {
		detail = "config written and validated (core restart not configured)"
	} else if catchUp {
		detail = "config applied (file already held the merged config; core restarted to catch up)"
	}
	if firstImport {
		detail += "; first import: original file backed up, panel-supported content imported once"
	}
	row, rerr := s.log(ctx, action, "applied", mergedHash, detail)
	if rerr == nil {
		all := append(append([]change{}, res.changes...), importOutcomes...)
		s.writeChangeRows(ctx, row.ID, all)
		// Bookkeeping uses a live ctx: a 90s timeout landing after a
		// successful restart must not leave a stale baseline behind.
		bctx := ctx
		if ctx.Err() != nil {
			bctx = context.Background()
		}
		_ = s.st.SetSyncState(bctx, store.SyncState{LastDoc: string(mergedDoc), LastHash: mergedHash, PassthroughTags: s.mergedPassTags(bctx, merged), UpdatedAt: nowUnix()})
		_ = s.st.SetBlockedHash(bctx, "")
		// Mark every still-pending audit row from before this sync
		// as applied by it, so the Logs page can show 待同步 / 已应
		// 用 per edit. Best-effort: a marking failure must not fail
		// the sync itself. Known edge: if a file import overwrote
		// a field a pending audit row edited, that row is still
		// marked applied — the change log is the authoritative
		// field record, not the audit row.
		_ = s.st.MarkPendingAuditsApplied(bctx, beforeID, mergedHash)
	}
	return row, rerr
}

// backupFirstImport copies the pre-import file into the panel data
// dir (0600: it may carry credentials) so the one-time import can be
// undone by hand. Best-effort: a backup failure is logged, never
// fails the sync (the import report in the change log still stands).
func (s *Service) backupFirstImport(fileBytes []byte) {
	if s.logPath == "" {
		return
	}
	dataDir := filepath.Dir(filepath.Dir(s.logPath))
	path := filepath.Join(dataDir, "first-import-backup.json")
	if err := os.WriteFile(path, fileBytes, 0o600); err != nil {
		s.warn("first-import backup failed", "path", path, "error", err)
		return
	}
	s.logger.Info("coresync: first-import backup written", "path", path)
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
				_ = s.st.SetBlockedHash(ctx, hash)
			}
			return row, rerr
		}
		return s.log(ctx, action, aerr.result, hash, aerr.Error())
	}
	row, rerr := s.log(ctx, action, "applied", hash, detail)
	if rerr == nil {
		s.writeChangeRows(ctx, row.ID, rows)
		_ = s.st.SetSyncState(ctx, store.SyncState{LastDoc: string(doc), LastHash: hash, UpdatedAt: nowUnix()})
		_ = s.st.SetBlockedHash(ctx, "")
		_ = s.st.MarkPendingAuditsApplied(ctx, beforeID, hash)
	}
	return row, rerr
}

// mergedPassTags recomputes the passthrough tag set from a merged
// doc: tags present in the doc that are not panel rows.
func (s *Service) mergedPassTags(ctx context.Context, merged map[string]any) []string {
	rows, err := s.st.ListInbounds(ctx)
	if err != nil {
		return nil
	}
	panelTags := map[string]bool{}
	for _, r := range rows {
		panelTags[r.Name] = true
	}
	var tags []string
	for _, v := range inboundList(merged) {
		if m, ok := asMap(v); ok {
			if tag := asString(m["tag"]); tag != "" && !panelTags[tag] {
				tags = append(tags, tag)
			}
		}
	}
	return tags
}

// applyImports executes the classifier's import ops one by one.
// Every op is best-effort and post-state verified: a service call
// may report an error from a view-only step (viewByID reads the
// target file) while its store write already landed, so success is
// decided by re-reading the row, not by the call's error alone.
// A failed op never aborts the sync — it is converted to a
// dropped_illegal or reverted row and the next sync retries it.
func (s *Service) applyImports(ctx context.Context, ops []importOp) []change {
	var out []change
	for _, op := range ops {
		switch op.kind {
		case "settingHost":
			_, _, err := s.settings.Update(ctx, settings.Update{PublicHost: &op.host})
			out = append(out, s.convertOp(op, err)...)
		case "inboundEnable":
			_, _, err := s.inboundsSvc.SetEnabled(ctx, op.inboundID, op.enabled)
			if err == nil || s.inboundEnabledMatches(ctx, op.inboundID, op.enabled) {
				out = append(out, op.changes...)
			} else {
				out = append(out, s.convertOp(op, err)...)
			}
		case "inboundPort":
			row := s.inboundRow(ctx, op.inboundID)
			if row == nil {
				out = append(out, s.convertOp(op, errors.New("inbound not found"))...)
				continue
			}
			_, _, err := s.inboundsSvc.Update(ctx, op.inboundID, inbounds.UpdateRequest{Name: row.Name, Port: op.port, Params: json.RawMessage(row.Params)})
			if err == nil || s.inboundPortMatches(ctx, op.inboundID, op.port) {
				out = append(out, op.changes...)
			} else {
				out = append(out, s.convertOp(op, err)...)
			}
		case "inboundParams":
			row := s.inboundRow(ctx, op.inboundID)
			if row == nil {
				out = append(out, s.convertOp(op, errors.New("inbound not found"))...)
				continue
			}
			_, _, err := s.inboundsSvc.Update(ctx, op.inboundID, inbounds.UpdateRequest{Name: row.Name, Port: row.Port, Params: json.RawMessage(op.params)})
			if err == nil || s.inboundParamsMatch(ctx, op.inboundID, op.params) {
				out = append(out, op.changes...)
			} else {
				out = append(out, s.convertOp(op, err)...)
			}
		case "inboundCreate":
			_, err := s.inboundsSvc.Create(ctx, inbounds.CreateRequest{Name: op.name, Protocol: op.protocol, Port: op.port, Params: json.RawMessage(op.params)})
			if err == nil || s.inboundCreateMatches(ctx, op) {
				out = append(out, op.changes...)
			} else {
				out = append(out, s.convertOp(op, err)...)
			}
		case "inboundDelete":
			err := s.inboundsSvc.Delete(ctx, op.inboundID)
			if err == nil || s.inboundRow(ctx, op.inboundID) == nil {
				out = append(out, op.changes...)
			} else {
				out = append(out, s.convertOp(op, err)...)
			}
		case "subUUID":
			if _, err := uuid.Parse(op.uuid); err != nil {
				out = append(out, s.convertOp(op, errors.New("invalid uuid: "+err.Error()))...)
				continue
			}
			if s.subUUIDTaken(ctx, op.subID, op.uuid) {
				out = append(out, s.toReverted(op.changes, "uuid 已被其他订阅使用，面板值保留")...)
				continue
			}
			err := s.st.UpdateSubscriptionUUID(ctx, op.subID, op.uuid, nowUnix())
			if err == nil || s.subUUIDMatches(ctx, op.subID, op.uuid) {
				out = append(out, op.changes...)
			} else {
				out = append(out, s.convertOp(op, err)...)
			}
		case "subRename":
			if !validSubName(op.name) {
				out = append(out, s.convertOp(op, errors.New("invalid subscription name"))...)
				continue
			}
			if s.subNameTaken(ctx, op.subID, op.name) {
				out = append(out, s.toReverted(op.changes, "订阅名已被其他订阅使用，面板值保留")...)
				continue
			}
			cur, err := s.st.GetSubscription(ctx, op.subID)
			if err != nil {
				out = append(out, s.convertOp(op, err)...)
				continue
			}
			err = s.st.UpdateSubscription(ctx, op.subID, op.name, cur.TemplateID, cur.LimitBytes, cur.ExpireAt, cur.MaxIPs, nowUnix())
			if err == nil || s.subNameMatches(ctx, op.subID, op.name) {
				out = append(out, op.changes...)
			} else {
				out = append(out, s.convertOp(op, err)...)
			}
		default:
			s.warn("unknown import op kind", "kind", op.kind)
		}
	}
	return out
}

// convertOp maps a failed op's proposed rows to dropped_illegal
// (invalid data) or reverted (conflict / row gone — the panel wins).
func (s *Service) convertOp(op importOp, err error) []change {
	if err == nil {
		return op.changes
	}
	var iErr *inbounds.Error
	var sErr *subs.Error
	code := ""
	if errors.As(err, &iErr) {
		code = iErr.Code
	} else if errors.As(err, &sErr) {
		code = sErr.Code
	}
	if strings.Contains(code, "conflict") || strings.Contains(code, "not_found") {
		return s.toReverted(op.changes, err.Error())
	}
	return s.toDropped(op.changes, err.Error())
}

func (s *Service) toDropped(changes []change, reason string) []change {
	out := make([]change, 0, len(changes))
	for _, ch := range changes {
		ch.action = changeDroppedIllegal
		ch.detail = reason
		out = append(out, ch)
	}
	return out
}

func (s *Service) toReverted(changes []change, reason string) []change {
	out := make([]change, 0, len(changes))
	for _, ch := range changes {
		ch.action = changeReverted
		ch.detail = reason
		out = append(out, ch)
	}
	return out
}

func (s *Service) inboundRow(ctx context.Context, id int64) *store.InboundRow {
	rows, err := s.st.ListInbounds(ctx)
	if err != nil {
		return nil
	}
	for i := range rows {
		if rows[i].ID == id {
			return &rows[i]
		}
	}
	return nil
}

func (s *Service) inboundEnabledMatches(ctx context.Context, id int64, enabled bool) bool {
	row := s.inboundRow(ctx, id)
	return row != nil && row.Enabled == enabled
}

func (s *Service) inboundPortMatches(ctx context.Context, id int64, port int) bool {
	row := s.inboundRow(ctx, id)
	return row != nil && row.Port == port
}

// inboundParamsMatch checks the intended params are a subset of the
// stored params, with one tolerance: an intended empty shadowsocks
// method normalizes to the panel default on save.
func (s *Service) inboundParamsMatch(ctx context.Context, id int64, intended string) bool {
	row := s.inboundRow(ctx, id)
	if row == nil {
		return false
	}
	want, _ := asMap(mustJSON(intended))
	got, _ := asMap(mustJSON(row.Params))
	if want == nil || got == nil {
		return false
	}
	for k, v := range want {
		gv, ok := got[k]
		if !ok {
			return false
		}
		if !jsonEqual(v, gv) {
			if k == "method" && asString(v) == "" && asString(gv) == "2022-blake3-aes-128-gcm" {
				continue
			}
			return false
		}
	}
	return true
}

func mustJSON(s string) any {
	var v any
	_ = json.Unmarshal([]byte(s), &v)
	return v
}

func (s *Service) inboundCreateMatches(ctx context.Context, op importOp) bool {
	rows, err := s.st.ListInbounds(ctx)
	if err != nil {
		return false
	}
	for i := range rows {
		if rows[i].Name == op.name && rows[i].Protocol == op.protocol && rows[i].Port == op.port {
			return s.inboundParamsMatch(ctx, rows[i].ID, op.params)
		}
	}
	return false
}

func (s *Service) subUUIDTaken(ctx context.Context, selfID int64, uuidStr string) bool {
	rows, err := s.st.ListSubscriptions(ctx)
	if err != nil {
		return false
	}
	for _, r := range rows {
		if r.ID != selfID && r.UUID == uuidStr {
			return true
		}
	}
	return false
}

func (s *Service) subNameTaken(ctx context.Context, selfID int64, name string) bool {
	rows, err := s.st.ListSubscriptions(ctx)
	if err != nil {
		return false
	}
	for _, r := range rows {
		if r.ID != selfID && r.Name == name {
			return true
		}
	}
	return false
}

func (s *Service) subUUIDMatches(ctx context.Context, id int64, uuidStr string) bool {
	row, err := s.st.GetSubscription(ctx, id)
	return err == nil && row.UUID == uuidStr
}

func (s *Service) subNameMatches(ctx context.Context, id int64, name string) bool {
	row, err := s.st.GetSubscription(ctx, id)
	return err == nil && row.Name == name
}

// changeActionLabel maps a change action to its Chinese label for the
// config-changes.log file.
func changeActionLabel(a changeAction) string {
	switch a {
	case changeUpdated:
		return "更新"
	case changeAdded:
		return "新增"
	case changeDeleted:
		return "删除"
	case changePassthrough:
		return "透传"
	case changeDroppedIllegal:
		return "丢弃非法"
	case changeReverted:
		return "恢复面板"
	case changeRestoreNote:
		return "提示"
	default:
		return string(a)
	}
}

// writeChangeRows persists change rows to the DB and appends them to
// <dataDir>/logs/config-changes.log (beside the core log file).
// Best-effort: a write failure never fails the sync itself.
func (s *Service) writeChangeRows(ctx context.Context, syncID int64, rows []change) {
	if len(rows) == 0 {
		return
	}
	loc := time.Local
	if cfg, err := s.settings.Get(ctx); err == nil {
		if l, err := time.LoadLocation(cfg.Timezone); err == nil {
			loc = l
		}
	}
	var file *os.File
	if s.logPath != "" {
		path := filepath.Join(filepath.Dir(s.logPath), "config-changes.log")
		if f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			file = f
			defer f.Close()
		} else {
			s.warn("open config-changes.log", "error", err)
		}
	}
	stamp := time.Now().In(loc).Format("2006-01-02 15:04:05")
	for _, ch := range rows {
		if _, err := s.st.AddConfigChange(ctx, store.ConfigChangeRow{
			At:        nowUnix(),
			SyncLogID: syncID,
			Action:    string(ch.action),
			Item:      ch.item,
			OldValue:  ch.oldVal,
			NewValue:  ch.newVal,
			Detail:    ch.detail,
		}); err != nil {
			s.warn("write config change row", "item", ch.item, "error", err)
		}
		if file != nil {
			line := stamp + " " + changeActionLabel(ch.action) + " " + ch.item
			if ch.oldVal != "" || ch.newVal != "" {
				line += " " + ch.oldVal + "→" + ch.newVal
			}
			if ch.detail != "" {
				line += " (" + ch.detail + ")"
			}
			if _, err := file.WriteString(line + "\n"); err != nil {
				s.warn("append config-changes.log", "error", err)
			}
		}
	}
}

// ListLog backs the panel's sync log view.
func (s *Service) ListLog(ctx context.Context, limit int) ([]store.SyncLogRow, error) {
	return s.st.ListSyncLogs(ctx, limit)
}

// ListChanges backs the Logs page "config" category.
func (s *Service) ListChanges(ctx context.Context, f store.ConfigChangeFilter) ([]store.ConfigChangeRow, error) {
	return s.st.ListConfigChanges(ctx, f)
}

// --- pipeline internals -------------------------------------------------------

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

func (s *Service) log(ctx context.Context, action, result, hash, detail string) (store.SyncLogRow, error) {
	// A sync row must be recorded even when the pipeline ctx timed
	// out (otherwise a timeout leaves no trace at all).
	if ctx.Err() != nil {
		ctx = context.Background()
	}
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

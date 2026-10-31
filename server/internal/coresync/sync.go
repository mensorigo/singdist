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
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"singbox-ui/server/internal/certs"
	"singbox-ui/server/internal/inbounds"
	"singbox-ui/server/internal/settings"
	"singbox-ui/server/internal/store"
	"singbox-ui/server/internal/subs"
)

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

// Status is the engine's report for the panel.
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
// Sync runs the full pipeline once (manual path: it bypasses the
// auto-sync input block — an explicit manual retry is an override).
func (s *Service) Sync(ctx context.Context) (store.SyncLogRow, error) {
	return s.syncInternal(ctx, "sync")
}

// setBlockedHash records (or clears, with "") the blocked merged-config
// hash. A failed SET is Error (the failed input would not be blocked
// and would retry in a loop); a failed CLEAR is Warn (the stale block
// is re-cleared on the next InSync convergence).
// setBlockedHash records (or clears, with "") the blocked merged-config
// hash. A failed SET is Error (the failed input would not be blocked
// and would retry in a loop); a failed CLEAR is Warn (the stale block
// is re-cleared on the next InSync convergence).
func (s *Service) setBlockedHash(ctx context.Context, hash string) {
	if err := s.st.SetBlockedHash(ctx, hash); err != nil {
		if hash == "" {
			s.logger.Warn("coresync: clear blocked hash failed", "error", err)
		} else {
			s.logger.Error("coresync: set blocked hash failed", "error", err)
		}
	}
}

// AutoSync is the debouncer's entry point: it compares the desired
// config against the target first and only runs the pipeline when
// they differ, so idle pushes leave no log rows and no restarts.
// It reports whether a sync actually ran. A busy pipeline (another
// sync holding the lock) reports ran=false with a "skipped" row so
// the debouncer retries soon instead of queueing. An input that
// failed at runtime and was rolled back is skipped until it changes.
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
			s.setBlockedHash(ctx, "")
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
			s.setBlockedHash(ctx, "")
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
				s.setBlockedHash(ctx, "")
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
				s.setBlockedHash(ctx, mergedHash)
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
		s.setBlockedHash(bctx, "")
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

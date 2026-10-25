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
	res := classify(oldParsed, fileParsed, panel)
	merged := mergeConfig(genParsed, res.passes)
	_, mergedHash := marshalDoc(merged)
	base.DesiredHash = mergedHash
	base.InSync = semanticEqual(merged, fileParsed)
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

	// Missing target: a deleted file is NOT "the user deleted
	// everything" — it bypasses classification entirely and is
	// recreated from the panel, with a note in the change log.
	fileBytes, ferr := os.ReadFile(s.target)
	if os.IsNotExist(ferr) {
		return s.applyAndRecord(ctx, action, beforeID, genDoc, genHash,
			"配置文件缺失，已用面板配置重建",
			[]change{{action: changeRestoreNote, item: "配置文件", detail: "目标文件缺失（不是文件改动），已按面板配置重建；分类已跳过"}})
	}
	if ferr != nil {
		return s.log(ctx, action, "error", "", "read target: "+ferr.Error())
	}
	// Invalid JSON: the whole file is illegal — panel-only overwrite.
	fileParsed, perr := parseDoc(fileBytes)
	if perr != nil {
		return s.applyAndRecord(ctx, action, beforeID, genDoc, genHash,
			"配置文件不是合法 JSON，已用面板配置覆盖："+perr.Error(),
			[]change{{action: changeDroppedIllegal, item: "配置文件", detail: "文件不是合法 JSON，已用面板配置覆盖：" + perr.Error()}})
	}

	// Baseline resolution.
	var oldParsed map[string]any
	if baseline.LastDoc != "" {
		if p, err := parseDoc([]byte(baseline.LastDoc)); err == nil {
			oldParsed = p
		}
	}
	if oldParsed == nil {
		// First run (or a corrupt baseline): bootstrap.
		lastApplied := ""
		if logs, err := s.st.ListSyncLogs(ctx, 50); err == nil {
			for _, l := range logs {
				if l.Result == "applied" {
					lastApplied = l.Hash
					break
				}
			}
		}
		if lastApplied != "" && hashBytes(fileBytes) == lastApplied {
			// The file is untouched panel output from a previous
			// sync (byte-identical to what the panel last
			// applied): nothing external exists, so no
			// classification is needed — the diff below is
			// empty by construction.
			oldParsed = fileParsed
		} else {
			pending, _ := s.st.CountPendingAudits(ctx)
			if pending > 0 && action != "sync" {
				// Ambiguous: the panel changed AND the file
				// changed and there is no baseline to separate
				// them. Auto sync must not guess — it skips,
				// and one manual sync resolves toward the file.
				return s.log(ctx, action, "skipped", genHash, "首次同步基线缺失且面板与文件同时有改动，无法区分来源，已跳过；手动同步一次可按文件为准解决")
			}
			// Manual sync resolves toward the file; no pending
			// audits means the panel is unchanged, so the diff
			// is purely external either way.
			oldParsed = genParsed
		}
	}
	panel := buildPanelState(inbounds, subscriptions, cfg.PublicHost, baseline.PassthroughTags)
	res := classify(oldParsed, fileParsed, panel)

	// Apply imports (best-effort per op, verified against the store).
	importOutcomes := s.applyImports(ctx, res.imports)

	// Regenerate after imports, then merge the passthrough.
	genDoc2, _, err := s.Generate(ctx)
	if err != nil {
		// Panel state already changed; the file did not. The
		// import rows are real, so they are still recorded — but
		// the passthrough/revert rows describe a merge that never
		// landed, so they are dropped here.
		row, rerr := s.log(ctx, action, "error", "", "regenerate after imports: "+err.Error())
		if rerr == nil {
			s.writeChangeRows(ctx, row.ID, importOutcomes)
		}
		return row, rerr
	}
	genParsed2, _ := parseDoc(genDoc2)
	merged := mergeConfig(genParsed2, res.passes)
	mergedDoc, mergedHash := marshalDoc(merged)

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
		// No write, no restart. The file already holds everything
		// (including passthrough), so the baseline becomes the
		// on-disk truth and the full change rows apply. The pending
		// audits are marked applied too: the file already contains
		// the panel's current state, so the edits are in effect.
		row, rerr := s.log(ctx, action, "no_change", mergedHash, "merged config matches the target file")
		if rerr == nil {
			all := append(append([]change{}, res.changes...), importOutcomes...)
			s.writeChangeRows(ctx, row.ID, all)
			_ = s.st.SetSyncState(ctx, store.SyncState{LastDoc: string(fileBytes), LastHash: hashBytes(fileBytes), PassthroughTags: s.mergedPassTags(ctx, merged), UpdatedAt: nowUnix()})
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

	if aerr := s.apply(ctx, mergedDoc); aerr != nil {
		if aerr.result == "check_failed" {
			// Illegal passthrough content must never block a
			// legitimate sync: fall back to panel-only.
			panelOnlyDoc, panelOnlyHash := marshalDoc(genParsed2)
			if aerr2 := s.apply(ctx, panelOnlyDoc); aerr2 != nil {
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
				_ = s.st.MarkPendingAuditsApplied(ctx, beforeID, panelOnlyHash)
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
	}
	row, rerr := s.log(ctx, action, "applied", mergedHash, detail)
	if rerr == nil {
		all := append(append([]change{}, res.changes...), importOutcomes...)
		s.writeChangeRows(ctx, row.ID, all)
		_ = s.st.SetSyncState(ctx, store.SyncState{LastDoc: string(mergedDoc), LastHash: mergedHash, PassthroughTags: s.mergedPassTags(ctx, merged), UpdatedAt: nowUnix()})
		// Mark every still-pending audit row from before this sync
		// as applied by it, so the Logs page can show 待同步 / 已应
		// 用 per edit. Best-effort: a marking failure must not fail
		// the sync itself. Known edge: if a file import overwrote
		// a field a pending audit row edited, that row is still
		// marked applied — the change log is the authoritative
		// field record, not the audit row.
		_ = s.st.MarkPendingAuditsApplied(ctx, beforeID, mergedHash)
	}
	return row, rerr
}

// applyAndRecord applies a panel-only doc (used by the missing-file
// and illegal-file paths, which bypass classification) and records
// the sync row, change rows, baseline and audit marking.
func (s *Service) applyAndRecord(ctx context.Context, action string, beforeID int64, doc []byte, hash, detail string, rows []change) (store.SyncLogRow, error) {
	if s.logPath != "" {
		if err := os.MkdirAll(filepath.Dir(s.logPath), 0o755); err != nil {
			return s.log(ctx, action, "error", hash, "create log directory: "+err.Error())
		}
	}
	if aerr := s.apply(ctx, doc); aerr != nil {
		return s.log(ctx, action, aerr.result, hash, aerr.Error())
	}
	row, rerr := s.log(ctx, action, "applied", hash, detail)
	if rerr == nil {
		s.writeChangeRows(ctx, row.ID, rows)
		_ = s.st.SetSyncState(ctx, store.SyncState{LastDoc: string(doc), LastHash: hash, UpdatedAt: nowUnix()})
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

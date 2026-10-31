package coresync

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"singbox-ui/server/internal/inbounds"
	"singbox-ui/server/internal/settings"
	"singbox-ui/server/internal/store"
	"singbox-ui/server/internal/subs"
)

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
// stored params; see inboundParamsMatch for its tolerances.
// inboundParamsMatch checks the intended params are a subset of the
// stored params; see inboundParamsMatch for its tolerances.
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

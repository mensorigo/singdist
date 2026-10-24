// Package logs serves the sidebar Logs page: three categories with
// filters. "panel" is the audit table (every mutating API call, written
// by the audit middleware); "core" and "connection" are the sing-box
// log file, ingested into the core_logs table by a background follower
// (filefollow.go) and served as indexed SELECTs. There is no journal
// fallback anymore: a fresh install starts with an empty table that
// fills from the moment the core starts writing the panel-managed
// file, which is the intended no-backfill behavior. Entry timestamps
// are unix milliseconds; the frontend renders them in the panel
// timezone.
package logs

import (
	"context"
	"log/slog"
	"strconv"

	"singbox-ui/server/internal/store"
)

// Entry is one log row in any category. Fields outside the row's
// category are empty. ConnID is internal only (sing-box's per-connection
// id, used to merge a connection's lines); it never leaves the API.
type Entry struct {
	Ts       int64  `json:"ts"`
	Category string `json:"category"`
	ConnID   string `json:"-"`
	// Core / connection fields.
	Level       string `json:"level,omitempty"`
	Message     string `json:"message,omitempty"`
	Source      string `json:"source,omitempty"`
	Destination string `json:"destination,omitempty"`
	Tag         string `json:"tag,omitempty"`
	User        string `json:"user,omitempty"`
	// Panel-operation fields.
	Action string `json:"action,omitempty"`
	Target string `json:"target,omitempty"`
	Result string `json:"result,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// ListParams scopes one page. Category is "core", "panel" or
// "connection". Before is an opaque cursor from a previous response's
// NextBefore: an audit id for panel, a core_logs id for the ingested
// categories. Empty means the latest page.
type ListParams struct {
	Category string
	Keyword  string
	Level    string
	Result   string
	Limit    int
	Before   string
}

// ListResult is one page plus the cursor for the next page ("" = none).
type ListResult struct {
	Entries    []Entry `json:"entries"`
	NextBefore string  `json:"nextBefore"`
}

// Service lists log entries from the audit table and the ingested
// core_logs table.
type Service struct {
	st *store.Store
	ff *fileFollower
}

func NewService(st *store.Store, logPath string, log *slog.Logger) *Service {
	return &Service{st: st, ff: newFileFollower(st, logPath, log)}
}

// StartFollow ingests the panel-managed log file into core_logs until
// ctx ends. It never fails the panel: follower errors are logged and
// retried on the next poll.
func (s *Service) StartFollow(ctx context.Context) {
	s.ff.Run(ctx)
}

func (s *Service) List(ctx context.Context, p ListParams) (ListResult, error) {
	limit := p.Limit
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	switch p.Category {
	case "panel":
		return s.listPanel(ctx, p, limit)
	case "connection":
		return s.listIngested(ctx, "connection", p, limit)
	case "core", "":
		return s.listIngested(ctx, "core", p, limit)
	default:
		return ListResult{}, errValidation("unknown log category")
	}
}

func (s *Service) listPanel(ctx context.Context, p ListParams, limit int) (ListResult, error) {
	var before int64
	if p.Before != "" {
		before, _ = strconv.ParseInt(p.Before, 10, 64)
	}
	rows, err := s.st.ListAuditLogs(ctx, store.AuditFilter{
		Result:  p.Result,
		Keyword: p.Keyword,
		Before:  before,
		Limit:   limit,
	})
	if err != nil {
		return ListResult{}, err
	}
	entries := make([]Entry, 0, len(rows))
	for _, r := range rows {
		entries = append(entries, Entry{
			Ts:       r.At * 1000,
			Category: "panel",
			Action:   r.Action,
			Target:   r.Target,
			Result:   r.Result,
			Detail:   r.Detail,
		})
	}
	res := ListResult{Entries: entries}
	if len(rows) == limit {
		res.NextBefore = strconv.FormatInt(rows[len(rows)-1].ID, 10)
	}
	return res, nil
}

// listIngested serves a core/connection page from core_logs.
func (s *Service) listIngested(ctx context.Context, category string, p ListParams, limit int) (ListResult, error) {
	var before int64
	if p.Before != "" {
		before, _ = strconv.ParseInt(p.Before, 10, 64)
	}
	rows, err := s.st.ListCoreLogs(ctx, store.CoreLogFilter{
		Kind:    category,
		Keyword: p.Keyword,
		Level:   p.Level,
		Before:  before,
		Limit:   limit,
	})
	if err != nil {
		return ListResult{}, err
	}
	entries := make([]Entry, 0, len(rows))
	for _, r := range rows {
		entries = append(entries, Entry{
			Ts:          r.Ts,
			Category:    r.Kind,
			Level:       r.Level,
			Message:     r.Message,
			Source:      r.Source,
			Destination: r.Destination,
			Tag:         r.Tag,
			User:        r.User,
		})
	}
	res := ListResult{Entries: entries}
	if len(rows) == limit {
		res.NextBefore = strconv.FormatInt(rows[len(rows)-1].ID, 10)
	}
	return res, nil
}

// Error mirrors the other modules' typed API failure.
type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }

func errValidation(msg string) *Error {
	return &Error{Status: 400, Code: "validation", Message: msg}
}

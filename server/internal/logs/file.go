package logs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"singbox-ui/server/internal/store"
)

// auditFileWriter appends panel-operation audit rows to a single
// plain-text log file. No rotation: panel operations are few (a
// handful of user actions a day), so one file stays small. It is
// best-effort: failures are reported to the caller for slogging and
// never fail the request itself.
type auditFileWriter struct {
	mu  sync.Mutex
	dir string
	loc func(context.Context) *time.Location
}

func newAuditFileWriter(dir string, loc func(context.Context) *time.Location) *auditFileWriter {
	return &auditFileWriter{dir: dir, loc: loc}
}

// LocationFromStore resolves the panel timezone from the settings
// table on every call (same source the dashboard sampler uses),
// falling back to Asia/Shanghai, then UTC.
func LocationFromStore(st *store.Store) func(context.Context) *time.Location {
	return func(ctx context.Context) *time.Location {
		if v, ok, err := st.GetSetting(ctx, "timezone"); err == nil && ok {
			if loc, lerr := time.LoadLocation(strings.TrimSpace(v)); lerr == nil {
				return loc
			}
		}
		if loc, err := time.LoadLocation("Asia/Shanghai"); err == nil {
			return loc
		}
		return time.UTC
	}
}

// Write appends one formatted line to audit.log, creating the file
// and its directory on first use.
func (w *auditFileWriter) Write(ctx context.Context, r store.AuditRow) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := os.MkdirAll(w.dir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(w.dir, "audit.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(w.format(ctx, r))
	return err
}

func (w *auditFileWriter) format(ctx context.Context, r store.AuditRow) string {
	loc := time.UTC
	if w.loc != nil {
		loc = w.loc(ctx)
	}
	result := "失败"
	if r.Result == "success" {
		result = "成功"
	}
	var b strings.Builder
	b.WriteString(time.Unix(r.At, 0).In(loc).Format("2006-01-02 15:04:05"))
	b.WriteString(" [")
	b.WriteString(result)
	b.WriteString("] ")
	b.WriteString(r.Action)
	if r.Target != "" {
		b.WriteString(" ")
		b.WriteString(r.Target)
	}
	if r.Detail != "" {
		b.WriteString(" (")
		b.WriteString(r.Detail)
		b.WriteString(")")
	}
	b.WriteString("\n")
	return b.String()
}

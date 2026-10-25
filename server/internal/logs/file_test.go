package logs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"singbox-ui/server/internal/store"
)

func TestAuditFileSingleFile(t *testing.T) {
	dir := t.TempDir()
	loc := func(context.Context) *time.Location {
		l, _ := time.LoadLocation("Asia/Shanghai")
		return l
	}
	w := newAuditFileWriter(dir, loc)

	rows := []store.AuditRow{
		{At: 1729752000, Action: "创建入站", Target: "hy2", Result: "success"},
		{At: 1729752060, Action: "登录", Result: "failed", Detail: "invalid_credentials", IP: "203.0.113.7"},
		{At: 1729752120, Action: "修改设置", Result: "success"},
	}
	for _, r := range rows {
		if err := w.Write(context.Background(), r); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	// One file holds every row, in order; no rotated siblings.
	data, err := os.ReadFile(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatalf("read audit.log: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"[成功] 创建入站 hy2",
		"[失败] 登录 (invalid_credentials) [203.0.113.7]",
		"[成功] 修改设置",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("audit.log = %q, missing %q", got, want)
		}
	}
	if strings.Index(got, "创建入站") > strings.Index(got, "修改设置") {
		t.Fatalf("audit.log out of order: %q", got)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("want exactly 1 file in dir, got %d", len(entries))
	}
	// Format check on a bare row: no trailing space or empty parens.
	line := w.format(context.Background(), store.AuditRow{
		At: 1729752000, Action: "登录", Result: "success",
	})
	if line != "2024-10-24 14:40:00 [成功] 登录\n" {
		t.Fatalf("format = %q", line)
	}
	// And with an IP: appended in brackets, nothing else changes.
	line = w.format(context.Background(), store.AuditRow{
		At: 1729752000, Action: "登录", Result: "success", IP: "203.0.113.7",
	})
	if line != "2024-10-24 14:40:00 [成功] 登录 [203.0.113.7]\n" {
		t.Fatalf("format = %q", line)
	}
	// And with a summary: appended after the IP.
	line = w.format(context.Background(), store.AuditRow{
		At: 1729752000, Action: "修改入站", Target: "hy2", Result: "success", IP: "203.0.113.7", Summary: "端口 443→8443",
	})
	if line != "2024-10-24 14:40:00 [成功] 修改入站 hy2 [203.0.113.7] 端口 443→8443\n" {
		t.Fatalf("format = %q", line)
	}
}

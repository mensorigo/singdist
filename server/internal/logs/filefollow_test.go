package logs

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"singbox-ui/server/internal/store"
)

func testFileStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// lineTsNow renders a core log line timestamp in the core's own format.
// Fixtures use recent timestamps: the follower's 90-day prune would
// delete epoch-era rows, and the test would measure the prune instead
// of the grouping.
func lineTsNow() string {
	return "+0000 " + time.Now().UTC().Format("2006-01-02 15:04:05")
}

func connLines(ts, id, node, from, user, to string) []string {
	return []string{
		ts + " INFO [" + id + " 12ms] inbound/vless-reality[" + node + "]: inbound connection from " + from,
		ts + " INFO [" + id + " 15ms] [" + user + "] inbound connection to " + to,
		ts + " INFO [" + id + " 18ms] outbound/direct[direct]: outbound connection to " + to,
	}
}

func appendLines(t *testing.T, path string, lines ...string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open log file: %v", err)
	}
	defer f.Close()
	for _, l := range lines {
		if _, err := f.WriteString(l + "\n"); err != nil {
			t.Fatalf("write log line: %v", err)
		}
	}
}

func countRows(t *testing.T, st *store.Store, kind string) int {
	t.Helper()
	// The store caps a page at 200 rows, so page through everything.
	total := 0
	var before int64
	for {
		rows, err := st.ListCoreLogs(context.Background(), store.CoreLogFilter{Kind: kind, Limit: 200, Before: before})
		if err != nil {
			t.Fatalf("list %s: %v", kind, err)
		}
		total += len(rows)
		if len(rows) < 200 {
			return total
		}
		before = rows[len(rows)-1].ID
	}
}

func TestFollowerGroupingAndPosition(t *testing.T) {
	ctx := context.Background()
	st := testFileStore(t)
	path := filepath.Join(t.TempDir(), "sing-box.log")
	ts := lineTsNow()
	lines := []string{
		ts + " INFO network: updated default interface host0, index 2",
		ts + " WARN something odd happened",
	}
	lines = append(lines, connLines(ts, "1001", "my-node", "1.2.3.4:5678", "testsub", "example.com:443")...)
	lines = append(lines, connLines(ts, "1002", "my-node", "5.6.7.8:1111", "testsub", "other.org:80")...)
	appendLines(t, path, lines...)

	ff := newFileFollower(st, path, slog.Default())
	ff.pollOnce(ctx)
	ff.flush(ctx, true)

	if got := countRows(t, st, "core"); got != 2 {
		t.Fatalf("core rows = %d, want 2", got)
	}
	conns, err := st.ListCoreLogs(ctx, store.CoreLogFilter{Kind: "connection", Limit: 1000})
	if err != nil {
		t.Fatalf("list connection: %v", err)
	}
	if len(conns) != 2 {
		t.Fatalf("connection rows = %d, want 2", len(conns))
	}
	bySource := map[string]store.CoreLogRow{}
	for _, r := range conns {
		bySource[r.Source] = r
	}
	first, ok := bySource["1.2.3.4:5678"]
	if !ok {
		t.Fatalf("missing connection from 1.2.3.4:5678: %+v", conns)
	}
	if first.Destination != "example.com:443" || first.User != "testsub" || first.Tag != "my-node" {
		t.Fatalf("grouped row mismatch: %+v", first)
	}

	// The checkpoint must sit exactly at EOF: nothing left unread.
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat log file: %v", err)
	}
	_, ino, off, found, err := st.GetFileState(ctx)
	if err != nil || !found {
		t.Fatalf("file state = found=%v err=%v", found, err)
	}
	if ino != int64(inodeOf(fi)) || off != fi.Size() {
		t.Fatalf("file state = (inode %d, offset %d), want (%d, %d)",
			ino, off, inodeOf(fi), fi.Size())
	}
}

func TestFollowerResumeAndRotation(t *testing.T) {
	ctx := context.Background()
	st := testFileStore(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "sing-box.log")
	ts := lineTsNow()
	appendLines(t, path, connLines(ts, "2001", "n1", "9.9.9.9:2222", "sub1", "a.com:443")...)

	ff := newFileFollower(st, path, slog.Default())
	ff.pollOnce(ctx)
	ff.flush(ctx, true)
	if got := countRows(t, st, "connection"); got != 1 {
		t.Fatalf("after first poll: connection rows = %d, want 1", got)
	}

	// A fresh follower (panel restart) must resume at the saved
	// offset: a poll with no new lines adds nothing.
	ff2 := newFileFollower(st, path, slog.Default())
	ff2.loadState(ctx)
	ff2.pollOnce(ctx)
	ff2.flush(ctx, true)
	if got := countRows(t, st, "connection"); got != 1 {
		t.Fatalf("after resume poll: connection rows = %d, want 1", got)
	}

	// Appended lines are picked up exactly once.
	appendLines(t, path, connLines(ts, "2002", "n1", "9.9.9.9:2223", "sub1", "b.com:443")...)
	ff2.pollOnce(ctx)
	ff2.flush(ctx, true)
	if got := countRows(t, st, "connection"); got != 2 {
		t.Fatalf("after append: connection rows = %d, want 2", got)
	}

	// Inode change (file replaced): the new file is read from 0.
	if err := os.Rename(path, filepath.Join(dir, "sing-box.log.old")); err != nil {
		t.Fatalf("rename: %v", err)
	}
	appendLines(t, path, connLines(ts, "2003", "n1", "9.9.9.9:2224", "sub1", "c.com:443")...)
	ff2.pollOnce(ctx)
	ff2.flush(ctx, true)
	if got := countRows(t, st, "connection"); got != 3 {
		t.Fatalf("after replacement: connection rows = %d, want 3", got)
	}

	// Truncation (copytruncate rotation): restart from 0, no dupes.
	if err := os.Truncate(path, 0); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	appendLines(t, path, connLines(ts, "2004", "n1", "9.9.9.9:2225", "sub1", "d.com:443")...)
	ff2.pollOnce(ctx)
	ff2.flush(ctx, true)
	if got := countRows(t, st, "connection"); got != 4 {
		t.Fatalf("after truncation: connection rows = %d, want 4", got)
	}
}

func TestFollowerRotate(t *testing.T) {
	ctx := context.Background()
	st := testFileStore(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "sing-box.log")
	ts := lineTsNow()
	appendLines(t, path, connLines(ts, "3001", "n1", "1.1.1.1:1111", "sub1", "x.com:443")...)

	ff := newFileFollower(st, path, slog.Default())
	ff.pollOnce(ctx)
	ff.flush(ctx, true)
	ff.rotate(ctx)

	archives, _ := filepath.Glob(filepath.Join(dir, "sing-box-*.log.gz"))
	if len(archives) != 1 {
		t.Fatalf("archives = %v, want exactly 1", archives)
	}
	if fi, err := os.Stat(path); err != nil || fi.Size() != 0 {
		t.Fatalf("live file after rotate: size=%v err=%v, want 0", fi, err)
	}

	// Post-rotation lines flow in exactly once.
	appendLines(t, path, connLines(ts, "3002", "n1", "1.1.1.1:1112", "sub1", "y.com:443")...)
	ff.pollOnce(ctx)
	ff.flush(ctx, true)
	if got := countRows(t, st, "connection"); got != 2 {
		t.Fatalf("after rotate + append: connection rows = %d, want 2", got)
	}
}

func TestParseLineTs(t *testing.T) {
	want := time.Date(2026, 10, 24, 18, 25, 44, 0, time.FixedZone("", 8*3600)).UnixMilli()
	got, ok := parseLineTs("+0800 2026-10-24 18:25:44 INFO hello")
	if !ok || got != want {
		t.Fatalf("parse +0800: got (%d, %v), want (%d, true)", got, ok, want)
	}
	want = time.Date(2026, 10, 24, 18, 25, 44, 0, time.FixedZone("", -5*3600)).UnixMilli()
	got, ok = parseLineTs("-0500 2026-10-24 18:25:44 INFO hello")
	if !ok || got != want {
		t.Fatalf("parse -0500: got (%d, %v), want (%d, true)", got, ok, want)
	}
	if _, ok := parseLineTs("not a log line"); ok {
		t.Fatalf("garbage line parsed as timestamp")
	}
}

func TestFollowerStripsANSI(t *testing.T) {
	ctx := context.Background()
	st := testFileStore(t)
	path := filepath.Join(t.TempDir(), "sing-box.log")
	ts := lineTsNow()
	// sing-box colorizes level and id tokens on stderr; file output
	// is plain so far, but the parser must survive colored lines.
	appendLines(t, path,
		ts+" \x1b[36mINFO\x1b[0m [\x1b[38;5;177m4001\x1b[0m 12ms] inbound/vless-reality[n1]: inbound connection from 9.9.9.9:2222",
		ts+" INFO [4001 15ms] [sub1] inbound connection to z.com:443",
		ts+" INFO [4001 18ms] outbound/direct[direct]: outbound connection to z.com:443",
	)
	ff := newFileFollower(st, path, slog.Default())
	ff.pollOnce(ctx)
	ff.flush(ctx, true)
	rows, err := st.ListCoreLogs(ctx, store.CoreLogFilter{Kind: "connection", Limit: 10})
	if err != nil {
		t.Fatalf("list connection: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("connection rows = %d, want 1", len(rows))
	}
	r := rows[0]
	if r.Level != "INFO" || r.Source != "9.9.9.9:2222" || r.Destination != "z.com:443" || r.User != "sub1" || r.Tag != "n1" {
		t.Fatalf("ANSI row mismatch: %+v", r)
	}
}

// BenchmarkParseLine isolates the per-line cost of the conversion
// the user asked about: strip ANSI, parse the line's own timestamp
// (offset -> absolute ms), and parse the entry. This is the only
// timezone work in the pipeline, and it runs inline in the single
// parse pass — there is no separate conversion pass over the data.
func BenchmarkParseLine(b *testing.B) {
	line := lineTsNow() + " INFO [12345 12ms] inbound/vless-reality[my-node]: inbound connection from 1.2.3.4:5678"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		msg := stripANSI(line)
		tsMs, _ := parseLineTs(msg)
		_ = parseJournalLine(journalLine{tsMs: tsMs, message: msg})
	}
}

// TestFollowerIngestTiming measures the whole read-file -> convert ->
// write-to-panel-logs path on 10,000 lines (2,000 connections plus
// 4,000 core lines). It asserts row counts and logs the wall time;
// it asserts nothing about speed, so it cannot flake on a slow box.
func TestFollowerIngestTiming(t *testing.T) {
	ctx := context.Background()
	st := testFileStore(t)
	path := filepath.Join(t.TempDir(), "sing-box.log")
	ts := lineTsNow()
	lines := make([]string, 0, 10000)
	for i := 0; i < 2000; i++ {
		lines = append(lines, connLines(ts, fmt.Sprintf("9%04d", i), "n1", "1.2.3.4:5678", "sub1", "example.com:443")...)
	}
	for i := 0; i < 4000; i++ {
		lines = append(lines, ts+" INFO network: filler core line")
	}
	appendLines(t, path, lines...)

	ff := newFileFollower(st, path, slog.Default())
	start := time.Now()
	ff.pollOnce(ctx)
	ff.flush(ctx, true)
	elapsed := time.Since(start)
	if got := countRows(t, st, "connection"); got != 2000 {
		t.Fatalf("connection rows = %d, want 2000", got)
	}
	if got := countRows(t, st, "core"); got != 4000 {
		t.Fatalf("core rows = %d, want 4000", got)
	}
	t.Logf("ingest: %d lines -> %d rows in %s (%.1f us/line)",
		len(lines), 6000, elapsed, float64(elapsed.Microseconds())/float64(len(lines)))
}

func TestFollowerPrune(t *testing.T) {
	ctx := context.Background()
	st := testFileStore(t)
	old := time.Now().Add(-100 * 24 * time.Hour).UnixMilli()
	if err := st.InsertCoreLogsWithFilePos(ctx,
		[]store.CoreLogRow{{Ts: old, Kind: "core", Level: "INFO", Message: "ancient"}},
		"/tmp/nonexistent.log", 0, 0); err != nil {
		t.Fatalf("insert old row: %v", err)
	}
	ff := newFileFollower(st, filepath.Join(t.TempDir(), "sing-box.log"), slog.Default())
	ff.prune(ctx, time.Now())
	if got := countRows(t, st, "core"); got != 0 {
		t.Fatalf("after prune: core rows = %d, want 0", got)
	}
	used, err := st.CoreLogsUsedBytes(ctx)
	if err != nil || used <= 0 {
		t.Fatalf("used bytes = %d, err = %v, want > 0", used, err)
	}
}

package logs

// File following: a background follower that turns the panel-managed
// sing-box log file (log.output, see coresync) into the core_logs
// table the Logs page reads.
//
// Why this shape: the core writes to one file the panel owns, so the
// panel no longer depends on journald (systemd's, not the core's) to
// collect logs — that matters for distribution. The follower polls
// the file once a second, parses new lines, groups one connection's
// lines (from / to / outbound) into a single record, and batches rows
// into SQLite. The Logs page then serves indexed SELECTs in
// milliseconds over the full 90-day history.
//
// Position and duplicates: the follower tracks (inode, offset) and
// saves the offset in the same transaction as each batch, so a crash
// re-reads only uncommitted bytes. The checkpoint offset is the
// position after the last COMPLETE line: a trailing partial line is
// held in memory and re-read on the next poll (or after a restart),
// never inserted half-written. A connection whose lines straddle a
// crash may surface as two partial rows; connections complete within
// milliseconds, so this is rare. A connection group flushes when it
// has origin + destination + the outbound line, or 30s after its last
// line, whichever comes first — so any line order still groups.
//
// Rotation: the core opens the log file with O_APPEND (verified
// 2026-10-24 against official sing-box 1.14.3 via /proc fdinfo), so
// rotation is copytruncate — copy to a dated archive, gzip it,
// truncate the live file in place. No core restart, no proxy blip.
// The follower detects the truncation (file smaller than its offset)
// and restarts from 0. Rotation runs daily (panel timezone) or when
// the file passes 100MB; archives older than 30 days are deleted.
// Rotation may drop the sub-second of lines written during the copy
// itself; that is the same class of gap as a crash, and the raw
// archive still holds them.
//
// No backfill: the file only exists because the panel configured
// log.output, so everything in it is post-install by definition. On
// first sight of a file (or after an inode change) the follower reads
// from 0; on restart it resumes at the saved offset.

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"singbox-ui/server/internal/store"
)

const (
	followPollInterval = time.Second
	followBatchSize    = 200
	followStaleAfter   = 30 * time.Second
	followPrunePeriod  = time.Hour
	followPendingCap   = 10000
	followMaxFileSize  = 100 * 1024 * 1024 // rotate early past this
	followArchiveDays  = 30 * 24 * time.Hour
	followDBCapBytes   = 500 * 1024 * 1024 // prune oldest first past this
	followPartialCap   = 1024 * 1024       // drop a runaway partial line past this
	followReadChunk    = 64 * 1024
)

// lineTs matches the timestamp prefix sing-box writes itself:
// "+0800 2026-10-24 18:25:44 ...". The offset is the core host's local
// timezone, so it is parsed from the line, never assumed.
var lineTs = regexp.MustCompile(
	`^([+-])(\d{2})(\d{2}) (\d{4})-(\d{2})-(\d{2}) (\d{2}):(\d{2}):(\d{2})`)

// parseLineTs extracts the line's own timestamp in milliseconds.
// Second precision is all the core writes; that is fine for log rows.
func parseLineTs(msg string) (int64, bool) {
	m := lineTs.FindStringSubmatch(msg)
	if m == nil {
		return 0, false
	}
	nums := make([]int, 0, 9)
	for _, s := range m[2:] {
		n, err := strconv.Atoi(s)
		if err != nil {
			return 0, false
		}
		nums = append(nums, n)
	}
	off := nums[0]*3600 + nums[1]*60
	if m[1] == "-" {
		off = -off
	}
	t := time.Date(nums[2], time.Month(nums[3]), nums[4],
		nums[5], nums[6], nums[7], 0, time.FixedZone("", off))
	return t.UnixMilli(), true
}

type pendingGroup struct {
	entry        Entry
	updated      time.Time
	outboundSeen bool
}

func coreRow(e Entry) store.CoreLogRow {
	return store.CoreLogRow{
		Ts:          e.Ts,
		Kind:        e.Category,
		Level:       e.Level,
		Message:     e.Message,
		Source:      e.Source,
		Destination: e.Destination,
		Tag:         e.Tag,
		User:        e.User,
	}
}

func inodeOf(fi os.FileInfo) uint64 {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Ino)
	}
	return 0
}

type fileFollower struct {
	st   *store.Store
	path string
	log  *slog.Logger
	loc  func(context.Context) *time.Location

	pending       map[string]*pendingGroup
	batch         []store.CoreLogRow
	fh            *os.File
	inode         uint64
	offset        int64 // file position after the last read (includes any partial line)
	partial       []byte
	firstLine     string // first complete line, "" = not yet observed
	savedInode    uint64
	savedOffset   int64
	hasSaved      bool
	lastPrune     time.Time
	lastRotateDay string
}

func newFileFollower(st *store.Store, path string, log *slog.Logger) *fileFollower {
	return &fileFollower{
		st:      st,
		path:    path,
		log:     log,
		loc:     LocationFromStore(st),
		pending: map[string]*pendingGroup{},
	}
}

func (f *fileFollower) closeFile() {
	if f.fh != nil {
		f.fh.Close()
		f.fh = nil
	}
	f.inode = 0
	f.offset = 0
	f.partial = nil
	f.firstLine = ""
}

// readFirstLine returns the file's first complete line, if it has one
// yet, without disturbing the current read position.
func (f *fileFollower) readFirstLine() (string, bool) {
	pos, err := f.fh.Seek(0, io.SeekCurrent)
	if err != nil {
		return "", false
	}
	defer f.fh.Seek(pos, io.SeekStart)
	if _, err := f.fh.Seek(0, io.SeekStart); err != nil {
		return "", false
	}
	buf := make([]byte, 4096)
	n, err := f.fh.Read(buf)
	if n == 0 {
		return "", false
	}
	if idx := bytes.IndexByte(buf[:n], '\n'); idx >= 0 {
		return strings.TrimSuffix(string(buf[:idx]), "\r"), true
	}
	if err == io.EOF {
		// File ends without a newline: the last line is complete
		// as far as the file is concerned.
		return strings.TrimSuffix(string(buf[:n]), "\r"), true
	}
	return "", false
}

// checkRewritten detects a file that was truncated and rewritten
// behind the follower's back: size alone cannot catch it (new content
// can regrow past the old offset within one poll), but the first line
// changes — a normal append never touches it. A differing first line
// resets reading to 0; an unknown fingerprint is learned, not treated
// as a rewrite (the open-time bytes were already handled by the
// normal partial-line path, so re-reading them would duplicate rows).
func (f *fileFollower) checkRewritten() bool {
	first, ok := f.readFirstLine()
	if !ok {
		return false
	}
	if f.firstLine == "" {
		f.firstLine = first
		return false
	}
	if first != f.firstLine {
		f.firstLine = first
		f.offset = 0
		f.partial = nil
		if _, err := f.fh.Seek(0, io.SeekStart); err != nil {
			f.closeFile()
		}
		return true
	}
	return false
}

// checkpoint is the durable read position: everything before it is a
// complete line already consumed (or flushed); the trailing partial
// line is excluded so it is re-read, never half-inserted.
func (f *fileFollower) checkpoint() int64 {
	cp := f.offset - int64(len(f.partial))
	if cp < 0 {
		return 0
	}
	return cp
}

// processLine merges one file line into the pending batch. Core lines
// go straight in; connection lines accumulate per connection id
// until the group is complete (origin + destination + outbound seen).
func (f *fileFollower) processLine(msg string) {
	msg = stripANSI(msg)
	tsMs, ok := parseLineTs(msg)
	if !ok {
		tsMs = time.Now().UnixMilli()
	}
	e := parseJournalLine(journalLine{tsMs: tsMs, message: msg})
	if e.Category == "core" || e.ConnID == "" {
		f.batch = append(f.batch, coreRow(e))
		return
	}
	g, ok := f.pending[e.ConnID]
	if !ok {
		if len(f.pending) >= followPendingCap {
			f.flushOldest()
		}
		g = &pendingGroup{entry: e}
		f.pending[e.ConnID] = g
	} else {
		mergeConnection(&g.entry, &e)
	}
	g.updated = time.Now()
	if strings.HasPrefix(e.Message, "outbound/") {
		g.outboundSeen = true
	}
	if g.entry.Source != "" && g.entry.Destination != "" && g.outboundSeen {
		f.batch = append(f.batch, coreRow(g.entry))
		delete(f.pending, e.ConnID)
	}
}

func (f *fileFollower) flushOldest() {
	var oldestID string
	var oldest time.Time
	for id, g := range f.pending {
		if oldestID == "" || g.updated.Before(oldest) {
			oldestID, oldest = id, g.updated
		}
	}
	if oldestID != "" {
		f.batch = append(f.batch, coreRow(f.pending[oldestID].entry))
		delete(f.pending, oldestID)
	}
}

// flush writes the pending batch (plus stale groups, or all groups
// when all is set) and the file position in one transaction. On
// failure the rows stay queued for the next flush; nothing is
// half-committed, and the saved position stays behind the batch so
// the lines are re-read instead of lost.
func (f *fileFollower) flush(ctx context.Context, all bool) {
	rows := f.batch
	f.batch = nil
	now := time.Now()
	for id, g := range f.pending {
		if all || now.Sub(g.updated) > followStaleAfter {
			rows = append(rows, coreRow(g.entry))
			delete(f.pending, id)
		}
	}
	if err := f.st.InsertCoreLogsWithFilePos(ctx, rows, f.path, int64(f.inode), f.checkpoint()); err != nil {
		f.log.Warn("logs: file follow flush failed", "err", err)
		f.batch = append(rows, f.batch...)
		return
	}
	if time.Since(f.lastPrune) > followPrunePeriod {
		f.prune(ctx, now)
	}
}

// prune enforces retention: 90 days by age, then oldest-first day
// chunks while the table stays past the size cap (busy nodes trade
// history for disk safety instead of growing unbounded).
func (f *fileFollower) prune(ctx context.Context, now time.Time) {
	// Sync retention mirrors core logs (90 days) but in Unix seconds
	// (sync `at` is seconds; core `ts` is milliseconds). A failure
	// here must not block core pruning, so warn and continue.
	if err := f.st.PruneSyncLogs(ctx, now.Add(-store.SyncLogRetention).Unix()); err != nil {
		f.log.Warn("logs: sync prune failed", "err", err)
	}
	if err := f.st.PruneCoreLogs(ctx, now.Add(-store.CoreLogRetention).UnixMilli()); err != nil {
		f.log.Warn("logs: file follow prune failed", "err", err)
		return
	}
	for i := 0; i < 30; i++ {
		used, err := f.st.CoreLogsUsedBytes(ctx)
		if err != nil {
			f.log.Warn("logs: file follow size check failed", "err", err)
			break
		}
		if used <= followDBCapBytes {
			break
		}
		minTs, ok, err := f.st.MinCoreLogsTs(ctx)
		if err != nil || !ok {
			break
		}
		if err := f.st.PruneCoreLogs(ctx, minTs+86400000); err != nil {
			f.log.Warn("logs: file follow cap prune failed", "err", err)
			break
		}
	}
	f.lastPrune = now
}

// consume splits newly read bytes into complete lines. A trailing
// partial line is held for the next poll; it is never parsed early.
func (f *fileFollower) consume(chunk []byte) {
	f.partial = append(f.partial, chunk...)
	for {
		idx := bytes.IndexByte(f.partial, '\n')
		if idx < 0 {
			break
		}
		line := strings.TrimSuffix(string(f.partial[:idx]), "\r")
		f.partial = append([]byte(nil), f.partial[idx+1:]...)
		if line != "" {
			f.processLine(line)
		}
	}
	if len(f.partial) > followPartialCap {
		f.log.Warn("logs: dropping runaway partial log line", "bytes", len(f.partial))
		f.partial = nil
	}
}

// pollOnce reads whatever the file gained since the last poll. A
// missing file is not an error (the core has not created it yet —
// before the first sync, or while the panel waits on a restart); the
// next poll retries. An inode change means the file was replaced, so
// reading restarts from 0; a smaller size means copytruncate rotation,
// same treatment.
func (f *fileFollower) pollOnce(ctx context.Context) {
	fi, err := os.Stat(f.path)
	if err != nil {
		f.closeFile()
		return
	}
	ino := inodeOf(fi)
	if f.fh == nil || ino != f.inode {
		f.closeFile()
		fh, err := os.Open(f.path)
		if err != nil {
			return
		}
		f.fh = fh
		f.inode = ino
		f.partial = nil
		if f.hasSaved && f.savedInode == ino && f.savedOffset > 0 && f.savedOffset <= fi.Size() {
			f.offset = f.savedOffset
		} else {
			f.offset = 0
		}
		if _, err := fh.Seek(f.offset, io.SeekStart); err != nil {
			f.closeFile()
			return
		}
		if first, ok := f.readFirstLine(); ok {
			f.firstLine = first
		}
	} else if f.checkRewritten() {
		// Already reset to 0 inside the check.
	} else if fi.Size() < f.offset {
		// Truncated (rotation): restart from the beginning.
		f.offset = 0
		f.partial = nil
		if _, err := f.fh.Seek(0, io.SeekStart); err != nil {
			f.closeFile()
			return
		}
	}
	if f.fh == nil {
		return // reset path closed the file; the next poll reopens it
	}
	buf := make([]byte, followReadChunk)
	for {
		n, rerr := f.fh.Read(buf)
		if n > 0 {
			f.offset += int64(n)
			f.consume(buf[:n])
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.log.Warn("logs: file follow read failed", "err", rerr)
			f.closeFile()
			return
		}
	}
	if len(f.batch) >= followBatchSize {
		f.flush(ctx, false)
	} else if len(f.batch) > 0 {
		// Polls run every second, so flushing here keeps new rows
		// visible within a second without a separate ticker.
		f.flush(ctx, false)
	}
}

// rotate copies the live file to a dated archive, gzips it, and
// truncates the live file in place. Safe only because the core writes
// with O_APPEND (verified against official 1.14.3); with any other
// writer this would leave sparse holes. The batch is flushed first so
// the archive boundary is clean, and archives older than 30 days go.
func (f *fileFollower) rotate(ctx context.Context) {
	f.flush(ctx, true)
	dir := filepath.Dir(f.path)
	now := time.Now().In(f.loc(ctx))
	archive := filepath.Join(dir, "sing-box-"+now.Format("2006-01-02-150405")+".log.gz")
	src, err := os.Open(f.path)
	if err != nil {
		f.log.Warn("logs: rotation open failed", "err", err)
		return
	}
	dst, err := os.OpenFile(archive, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		src.Close()
		f.log.Warn("logs: rotation archive create failed", "err", err)
		return
	}
	gz := gzip.NewWriter(dst)
	if _, err := io.Copy(gz, src); err != nil {
		src.Close()
		gz.Close()
		dst.Close()
		f.log.Warn("logs: rotation copy failed", "err", err)
		return
	}
	src.Close()
	if err := gz.Close(); err != nil {
		dst.Close()
		f.log.Warn("logs: rotation gzip close failed", "err", err)
		return
	}
	if err := dst.Close(); err != nil {
		f.log.Warn("logs: rotation archive close failed", "err", err)
		return
	}
	if err := os.Truncate(f.path, 0); err != nil {
		f.log.Warn("logs: rotation truncate failed", "err", err)
		return
	}
	f.closeFile()
	// Prune old archives (mtime-based; the live file never matches
	// the archive pattern).
	cutoff := now.Add(-followArchiveDays)
	matches, _ := filepath.Glob(filepath.Join(dir, "sing-box-*.log.gz"))
	for _, m := range matches {
		if fi, err := os.Stat(m); err == nil && fi.ModTime().Before(cutoff) {
			_ = os.Remove(m)
		}
	}
	f.log.Info("logs: rotated core log file", "archive", archive)
}

// rotateIfDue rotates at a day boundary (panel timezone) or when the
// file passes the size cap. A missing or empty file has nothing to
// rotate.
func (f *fileFollower) rotateIfDue(ctx context.Context) {
	day := time.Now().In(f.loc(ctx)).Format("2006-01-02")
	if f.lastRotateDay == "" {
		f.lastRotateDay = day
		return
	}
	fi, err := os.Stat(f.path)
	if err != nil || fi.Size() == 0 {
		return
	}
	if day != f.lastRotateDay || fi.Size() > followMaxFileSize {
		f.rotate(ctx)
		f.lastRotateDay = time.Now().In(f.loc(ctx)).Format("2006-01-02")
	}
}

// loadState reads the durable file position for resume. Only a state
// recorded for this exact path applies; anything else starts from 0.
func (f *fileFollower) loadState(ctx context.Context) {
	if p, ino, off, ok, err := f.st.GetFileState(ctx); err != nil {
		f.log.Warn("logs: file state read failed", "err", err)
	} else if ok && p == f.path {
		f.savedInode, f.savedOffset, f.hasSaved = uint64(ino), off, true
	}
}

// Run follows the file until ctx ends. A missing file, a read error,
// or a failed rotation never kills the loop; the next poll retries.
func (f *fileFollower) Run(ctx context.Context) {
	f.loadState(ctx)
	f.pollOnce(ctx)
	t := time.NewTicker(followPollInterval)
	defer t.Stop()
	defer f.closeFile()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			f.pollOnce(ctx)
			f.rotateIfDue(ctx)
		}
	}
}

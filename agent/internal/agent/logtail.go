package agent

import (
	"os"
	"strings"
	"time"
)

func (t *trafficCollector) tailLog() {
	if t.logPath == "" {
		return
	}
	f, err := os.Open(t.logPath)
	if err != nil {
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return
	}
	if fi.Size() < t.logOff {
		t.logOff = 0 // rotated / truncated
	}
	if _, err := f.Seek(t.logOff, 0); err != nil {
		return
	}
	buf := make([]byte, 0, 1<<20)
	chunk := make([]byte, 65536)
	for {
		n, err := f.Read(chunk)
		if n > 0 {
			buf = append(buf, chunk[:n]...)
		}
		if err != nil {
			break
		}
	}
	t.logOff = fi.Size()
	// Only complete lines; a trailing partial line is re-read
	// next tick (its bytes are before logOff, so re-seek to the
	// last newline instead).
	lastNL := strings.LastIndexByte(string(buf), '\n')
	if lastNL >= 0 {
		t.logOff = t.logOff - int64(len(buf)-lastNL-1)
		buf = buf[:lastNL+1]
	} else {
		t.logOff = fi.Size() - int64(len(buf))
	}
	for _, line := range strings.Split(string(buf), "\n") {
		if line == "" {
			continue
		}
		// Core log lines start "+0000 YYYY-MM-DD HH:MM:SS".
		// Parse it (UTC) for time-based matching below.
		var lineTsMs int64
		if fields := strings.Fields(line); len(fields) >= 3 {
			if ts, err := time.Parse("2006-01-02 15:04:05", fields[1]+" "+fields[2]); err == nil {
				lineTsMs = ts.UnixMilli()
			}
		}
		if m := logFromRe.FindStringSubmatch(line); m != nil {
			lc, ok := t.pending[m[1]]
			if !ok {
				lc = &logConn{}
				t.pending[m[1]] = lc
			}
			lc.source = m[3]
			lc.inbound = m[2]
			if lineTsMs != 0 {
				lc.tsMs = lineTsMs
			}
			lc.seen = time.Now()
			if lc.user != "" {
				t.bySource[lc.source] = lc
			}
			continue
		}
		if m := logToRe.FindStringSubmatch(line); m != nil {
			lc, ok := t.pending[m[1]]
			if !ok {
				lc = &logConn{}
				t.pending[m[1]] = lc
			}
			lc.inbound = m[2]
			lc.user = m[3]
			if lc.tsMs == 0 && lineTsMs != 0 {
				lc.tsMs = lineTsMs
			}
			lc.seen = time.Now()
			if lc.source != "" {
				t.bySource[lc.source] = lc
			}
		}
	}
	// Prune stale pending/source entries (10 minutes).
	cut := time.Now().Add(-10 * time.Minute)
	for id, lc := range t.pending {
		if lc.seen.Before(cut) {
			delete(t.pending, id)
		}
	}
	for src, lc := range t.bySource {
		if lc.seen.Before(cut) {
			delete(t.bySource, src)
		}
	}
}

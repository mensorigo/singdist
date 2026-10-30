package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// TrafficEntry is one accumulated traffic bucket. Inbound is the
// panel inbound name (core tag); User is the core username as
// logged by sing-box, empty for the per-inbound rollup. Up is
// client-to-server bytes, Down is server-to-client bytes, as
// reported by the Clash API for that connection.
type TrafficEntry struct {
	Inbound string `json:"inbound"`
	User    string `json:"user,omitempty"`
	Up      int64  `json:"up"`
	Down    int64  `json:"down"`
}

// TrafficSnapshot is what the panel reads from status: totals
// since the collector started (persisted across agent restarts),
// plus the bytes that could not be attributed to an inbound/user
// (short connections that started and ended between Clash polls,
// or log lines the tail had not seen yet).
type TrafficSnapshot struct {
	Up             int64          `json:"up"`
	Down           int64          `json:"down"`
	UnassignedUp   int64          `json:"unassignedUp"`
	UnassignedDown int64          `json:"unassignedDown"`
	ByInbound      []TrafficEntry `json:"byInbound"`
	ByUser         []TrafficEntry `json:"byUser"`
	UpdatedAt      int64          `json:"updatedAt"`
}

// trafficCollector polls the core's Clash /connections endpoint
// and tails the core log, matching the two by client source
// address (IP:port), which is unique per socket while it is
// alive. Log lines give (inbound, user); Clash gives bytes.
// A connection that appears in Clash but has no log mapping yet
// is retried on later polls; bytes for a connection that starts
// and ends between two polls are lost to the breakdown (they
// still count in the Clash totals, not here), so ByInbound/ByUser
// sums are a lower bound and Unassigned covers the gap honestly.
type trafficCollector struct {
	clashAddr string
	logPath   string
	dataPath  string

	mu       sync.Mutex
	byKey    map[string]*TrafficEntry // inbound\x00user
	byInb    map[string]*TrafficEntry // inbound
	unUp     int64
	unDown   int64
	active   map[string]*activeConn
	logOff   int64
	pending  map[string]*logConn // sing-box connection id
	bySource map[string]*logConn // sourceIP:port
}

type activeConn struct {
	inbound  string
	user     string
	assigned bool
	lastUp   int64
	lastDown int64
}

type logConn struct {
	source  string
	inbound string
	user    string
	seen    time.Time
}

type clashConn struct {
	ID       string `json:"id"`
	Upload   int64  `json:"upload"`
	Download int64  `json:"download"`
	Metadata struct {
		SourceIP string `json:"sourceIP"`
		// Real sing-box Clash API returns sourcePort as a STRING,
		// not a number; typing it as int makes the whole decode
		// fail and drops every connection's bytes.
		SourcePort string `json:"sourcePort"`
	} `json:"metadata"`
}

type clashResp struct {
	Connections []clashConn `json:"connections"`
}

var (
	// +0000 2026-10-30 06:51:23 INFO [1490479969 0ms] inbound/vless[vless-reality]: inbound connection from 1.2.3.4:21612
	// Elapsed is a Go duration string: "0ms", "175ms", but also
	// "1.11s" for slow connections. Matching only \d+ms drops
	// every to-line over 1s, which leaves the user unset and all
	// of that connection's bytes unassigned.
	logFromRe = regexp.MustCompile(`\[(\d+) [^\]]+\] inbound/[\w-]+\[([^\]]+)\]: inbound connection from (\S+)`)
	// ... [1490479969 190ms] inbound/vless[vless-reality]: [test] inbound connection to host:443
	logToRe = regexp.MustCompile(`\[(\d+) [^\]]+\] inbound/[\w-]+\[([^\]]+)\]: \[([^\]]+)\] inbound connection to`)
)

func newTrafficCollector(clashAddr, logPath, dataPath string) *trafficCollector {
	t := &trafficCollector{
		clashAddr: clashAddr,
		logPath:   logPath,
		dataPath:  dataPath,
		byKey:     map[string]*TrafficEntry{},
		byInb:     map[string]*TrafficEntry{},
		active:    map[string]*activeConn{},
		pending:   map[string]*logConn{},
		bySource:  map[string]*logConn{},
	}
	t.load()
	return t
}

type trafficFile struct {
	ByKey  map[string]*TrafficEntry `json:"byKey"`
	ByInb  map[string]*TrafficEntry `json:"byInb"`
	UnUp   int64                    `json:"unUp"`
	UnDown int64                    `json:"unDown"`
}

func (t *trafficCollector) load() {
	if t.dataPath == "" {
		return
	}
	b, err := os.ReadFile(t.dataPath)
	if err != nil {
		return
	}
	var f trafficFile
	if json.Unmarshal(b, &f) != nil {
		return
	}
	t.byKey = f.ByKey
	if t.byKey == nil {
		t.byKey = map[string]*TrafficEntry{}
	}
	t.byInb = f.ByInb
	if t.byInb == nil {
		t.byInb = map[string]*TrafficEntry{}
	}
	t.unUp, t.unDown = f.UnUp, f.UnDown
}

func (t *trafficCollector) saveLocked() {
	if t.dataPath == "" {
		return
	}
	f := trafficFile{ByKey: t.byKey, ByInb: t.byInb, UnUp: t.unUp, UnDown: t.unDown}
	b, err := json.Marshal(f)
	if err != nil {
		return
	}
	dir := filepath.Dir(t.dataPath)
	_ = os.MkdirAll(dir, 0o755)
	tmp, err := os.CreateTemp(dir, ".traffic-*.json")
	if err != nil {
		return
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return
	}
	if err := tmp.Close(); err != nil {
		return
	}
	_ = os.Chmod(tmp.Name(), 0o600)
	_ = os.Rename(tmp.Name(), t.dataPath)
}

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
		if m := logFromRe.FindStringSubmatch(line); m != nil {
			lc, ok := t.pending[m[1]]
			if !ok {
				lc = &logConn{}
				t.pending[m[1]] = lc
			}
			lc.source = m[3]
			lc.inbound = m[2]
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

func (t *trafficCollector) pollClash() {
	if t.clashAddr == "" {
		return
	}
	url := strings.TrimSuffix(t.clashAddr, "/") + "/connections"
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return
	}
	var cr clashResp
	if json.NewDecoder(resp.Body).Decode(&cr) != nil {
		return
	}
	seen := map[string]bool{}
	for _, c := range cr.Connections {
		seen[c.ID] = true
		src := c.Metadata.SourceIP + ":" + c.Metadata.SourcePort
		ac, ok := t.active[c.ID]
		if !ok {
			ac = &activeConn{}
			t.active[c.ID] = ac
		}
		if !ac.assigned {
			if lc, ok := t.bySource[src]; ok {
				ac.inbound, ac.user, ac.assigned = lc.inbound, lc.user, true
			}
		}
		dUp := c.Upload - ac.lastUp
		dDown := c.Download - ac.lastDown
		if dUp < 0 {
			dUp = c.Upload
		}
		if dDown < 0 {
			dDown = c.Download
		}
		ac.lastUp, ac.lastDown = c.Upload, c.Download
		if dUp == 0 && dDown == 0 {
			continue
		}
		if ac.assigned && ac.inbound != "" {
			key := ac.inbound + "\x00" + ac.user
			e, ok := t.byKey[key]
			if !ok {
				e = &TrafficEntry{Inbound: ac.inbound, User: ac.user}
				t.byKey[key] = e
			}
			e.Up += dUp
			e.Down += dDown
			ie, ok := t.byInb[ac.inbound]
			if !ok {
				ie = &TrafficEntry{Inbound: ac.inbound}
				t.byInb[ac.inbound] = ie
			}
			ie.Up += dUp
			ie.Down += dDown
		} else {
			t.unUp += dUp
			t.unDown += dDown
		}
	}
	for id := range t.active {
		if !seen[id] {
			delete(t.active, id)
		}
	}
}


// Run ticks until ctx is done; 5s matches the panel's push poll
// cadence closely enough that short connections are the only
// systematic loss (see the collector comment).
func (t *trafficCollector) Run(ctx context.Context) {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			t.mu.Lock()
			t.tailLog()
			t.pollClash()
			t.saveLocked()
			t.mu.Unlock()
		}
	}
}

func (t *trafficCollector) Snapshot() TrafficSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := TrafficSnapshot{UnassignedUp: t.unUp, UnassignedDown: t.unDown, UpdatedAt: time.Now().Unix()}
	for _, e := range t.byInb {
		s.ByInbound = append(s.ByInbound, *e)
		s.Up += e.Up
		s.Down += e.Down
	}
	for _, e := range t.byKey {
		s.ByUser = append(s.ByUser, *e)
	}
	sort.Slice(s.ByInbound, func(i, j int) bool { return s.ByInbound[i].Inbound < s.ByInbound[j].Inbound })
	sort.Slice(s.ByUser, func(i, j int) bool {
		if s.ByUser[i].Inbound != s.ByUser[j].Inbound {
			return s.ByUser[i].Inbound < s.ByUser[j].Inbound
		}
		return s.ByUser[i].User < s.ByUser[j].User
	})
	return s
}

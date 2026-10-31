package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
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
	Up                  int64          `json:"up"`
	Down                int64          `json:"down"`
	UnassignedUp        int64          `json:"unassignedUp"`
	UnassignedDown      int64          `json:"unassignedDown"`
	ByInbound           []TrafficEntry `json:"byInbound"`
	ByUser              []TrafficEntry `json:"byUser"`
	UnassignedByInbound []TrafficEntry `json:"unassignedByInbound,omitempty"`
	UpdatedAt           int64          `json:"updatedAt"`
}

// trafficCollector polls the core's Clash /connections endpoint
// and tails the core log, matching the two by client source
// address (IP:port), which is unique per socket while it is
// alive. Log lines give (inbound, user); Clash gives bytes.
// A connection that appears in Clash but has no log mapping yet
// is retried on later polls; bytes for a connection that starts
// and ends between two 1s polls are lost to the breakdown (they
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
	unByInb  map[string]*TrafficEntry
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
	// tsMs is the log line's own timestamp (unix ms, UTC), parsed
	// from the "+0000 YYYY-MM-DD HH:MM:SS" prefix; 0 when the
	// line's timestamp could not be parsed. It is the key for
	// time-based matching against the Clash connection start.
	tsMs int64
}

type clashConn struct {
	ID       string `json:"id"`
	Upload   int64  `json:"upload"`
	Download int64  `json:"download"`
	Start    string `json:"start"`
	Metadata struct {
		SourceIP string `json:"sourceIP"`
		// Real sing-box Clash API returns sourcePort as a STRING,
		// not a number; typing it as int makes the whole decode
		// fail and drops every connection's bytes.
		SourcePort string `json:"sourcePort"`
		Type       string `json:"type"`
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
		unByInb:   map[string]*TrafficEntry{},
		active:    map[string]*activeConn{},
		pending:   map[string]*logConn{},
		bySource:  map[string]*logConn{},
	}
	t.load()
	return t
}

// AgentActiveEntry is the persisted form of one live Clash
// connection, mirroring the panel collector: without it an agent
// restart recounts in-flight connections from zero and can lose
// their attribution.
type AgentActiveEntry struct {
	Inbound  string `json:"inbound"`
	User     string `json:"user"`
	Assigned bool   `json:"assigned"`
	LastUp   int64  `json:"lastUp"`
	LastDown int64  `json:"lastDown"`
}

type trafficFile struct {
	ByKey   map[string]*TrafficEntry     `json:"byKey"`
	ByInb   map[string]*TrafficEntry     `json:"byInb"`
	UnUp    int64                        `json:"unUp"`
	UnDown  int64                        `json:"unDown"`
	Active  map[string]*AgentActiveEntry `json:"active,omitempty"`
	UnByInb map[string]*TrafficEntry     `json:"unByInb,omitempty"`
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
	if f.UnByInb != nil {
		t.unByInb = f.UnByInb
	}
	for id, ae := range f.Active {
		if ae == nil {
			continue
		}
		t.active[id] = &activeConn{
			inbound: ae.Inbound, user: ae.User, assigned: ae.Assigned,
			lastUp: ae.LastUp, lastDown: ae.LastDown,
		}
	}
}

func (t *trafficCollector) saveLocked() {
	if t.dataPath == "" {
		return
	}
	active := make(map[string]*AgentActiveEntry, len(t.active))
	for id, ac := range t.active {
		active[id] = &AgentActiveEntry{
			Inbound: ac.inbound, User: ac.user, Assigned: ac.assigned,
			LastUp: ac.lastUp, LastDown: ac.lastDown,
		}
	}
	f := trafficFile{ByKey: t.byKey, ByInb: t.byInb, UnUp: t.unUp, UnDown: t.unDown, Active: active, UnByInb: t.unByInb}
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

// Run ticks until ctx is done; 1s (was 5s) so connections in
// the 1-5s band are caught instead of falling to unassigned.
// Sub-second connections are still the systematic loss.
func (t *trafficCollector) Run(ctx context.Context) {
	tick := time.NewTicker(1 * time.Second)
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
	for _, e := range t.unByInb {
		s.UnassignedByInbound = append(s.UnassignedByInbound, *e)
	}
	sort.Slice(s.UnassignedByInbound, func(i, j int) bool { return s.UnassignedByInbound[i].Inbound < s.UnassignedByInbound[j].Inbound })
	sort.Slice(s.ByInbound, func(i, j int) bool { return s.ByInbound[i].Inbound < s.ByInbound[j].Inbound })
	sort.Slice(s.ByUser, func(i, j int) bool {
		if s.ByUser[i].Inbound != s.ByUser[j].Inbound {
			return s.ByUser[i].Inbound < s.ByUser[j].Inbound
		}
		return s.ByUser[i].User < s.ByUser[j].User
	})
	return s
}

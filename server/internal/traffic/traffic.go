// Package traffic collects per-inbound and per-subscription
// traffic for the LOCAL core, mirroring the node agent's
// collector: it polls the Clash /connections endpoint and matches
// each connection to a core-log record by client source address,
// which gives (inbound tag, core username). Totals are persisted
// to a JSON file in the panel data dir so a panel restart does
// not reset the statistics. Short connections that start and end
// between two 1s polls are lost to the breakdown; their bytes are
// counted in Unassigned, never forced onto a subscription except
// for the sole-subscriber estimate in summary.go.
package traffic

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"singbox-ui/server/internal/store"
)

type Entry struct {
	Inbound string `json:"inbound"`
	User    string `json:"user,omitempty"`
	Up      int64  `json:"up"`
	Down    int64  `json:"down"`
}

type Snapshot struct {
	Up             int64   `json:"up"`
	Down           int64   `json:"down"`
	UnassignedUp   int64   `json:"unassignedUp"`
	UnassignedDown int64   `json:"unassignedDown"`
	ByInbound      []Entry `json:"byInbound"`
	ByUser         []Entry `json:"byUser"`
	// UnassignedByInbound splits the unassigned totals by the
	// inbound tag from the Clash connection type, when known.
	UnassignedByInbound []Entry `json:"unassignedByInbound,omitempty"`
	UpdatedAt           int64   `json:"updatedAt"`
}

type activeConn struct {
	inbound  string
	user     string
	assigned bool
	lastUp   int64
	lastDown int64
}

type clashConn struct {
	ID       string `json:"id"`
	Upload   int64  `json:"upload"`
	Download int64  `json:"download"`
	// Start is the core's connection start time (RFC3339Nano, UTC).
	// It is the key for time-based log matching: a source port
	// reused by a later connection must not be attributed to an
	// earlier log record for the same source address.
	Start    string `json:"start"`
	Metadata struct {
		SourceIP string `json:"sourceIP"`
		// Real sing-box Clash API returns sourcePort as a STRING,
		// not a number; typing it as int makes the whole decode
		// fail and drops every connection's bytes.
		SourcePort string `json:"sourcePort"`
		// Clash "type" is "protocol/tag", e.g. "vless/vless-reality";
		// the tag part identifies the inbound even for a connection
		// whose user could not be matched, so unassigned bytes can
		// still be broken down per inbound.
		Type string `json:"type"`
	} `json:"metadata"`
}

type clashResp struct {
	Connections []clashConn `json:"connections"`
}

// ActiveEntry is the persisted form of one live Clash connection:
// which inbound/user it was attributed to (if any) and how many
// bytes had already been counted for it. Without this, a panel
// restart loses both the attribution and the baseline: the same
// connection is then counted again from zero (double count) and,
// if it started more than the log window ago, can never be
// attributed again (all later bytes fall to unassigned).
type ActiveEntry struct {
	Inbound  string `json:"inbound"`
	User     string `json:"user"`
	Assigned bool   `json:"assigned"`
	LastUp   int64  `json:"lastUp"`
	LastDown int64  `json:"lastDown"`
}

type fileData struct {
	ByKey   map[string]*Entry       `json:"byKey"`
	ByInb   map[string]*Entry       `json:"byInb"`
	UnUp    int64                   `json:"unUp"`
	UnDown  int64                   `json:"unDown"`
	Active  map[string]*ActiveEntry `json:"active,omitempty"`
	UnByInb map[string]*Entry       `json:"unByInb,omitempty"`
}

type Collector struct {
	clashAPI string
	st       *store.Store
	dataPath string

	mu      sync.Mutex
	byKey   map[string]*Entry
	byInb   map[string]*Entry
	unUp    int64
	unDown  int64
	unByInb map[string]*Entry
	active  map[string]*activeConn
	// lastWideLookup throttles the 24h fallback mapping query:
	// it runs at most once per 5 minutes, and only while some
	// live connection is still unassigned.
	lastWideLookup time.Time
}

func New(clashAPI string, st *store.Store, dataPath string) *Collector {
	c := &Collector{
		clashAPI: clashAPI,
		st:       st,
		dataPath: dataPath,
		byKey:    map[string]*Entry{},
		byInb:    map[string]*Entry{},
		unByInb:  map[string]*Entry{},
		active:   map[string]*activeConn{},
	}
	c.load()
	return c
}

func (c *Collector) load() {
	if c.dataPath == "" {
		return
	}
	b, err := os.ReadFile(c.dataPath)
	if err != nil {
		return
	}
	var f fileData
	if json.Unmarshal(b, &f) != nil {
		return
	}
	if f.ByKey != nil {
		c.byKey = f.ByKey
	}
	if f.ByInb != nil {
		c.byInb = f.ByInb
	}
	c.unUp, c.unDown = f.UnUp, f.UnDown
	if f.UnByInb != nil {
		c.unByInb = f.UnByInb
	}
	for id, ae := range f.Active {
		if ae == nil {
			continue
		}
		c.active[id] = &activeConn{
			inbound: ae.Inbound, user: ae.User, assigned: ae.Assigned,
			lastUp: ae.LastUp, lastDown: ae.LastDown,
		}
	}
}

func (c *Collector) saveLocked() {
	if c.dataPath == "" {
		return
	}
	active := make(map[string]*ActiveEntry, len(c.active))
	for id, ac := range c.active {
		active[id] = &ActiveEntry{
			Inbound: ac.inbound, User: ac.user, Assigned: ac.assigned,
			LastUp: ac.lastUp, LastDown: ac.lastDown,
		}
	}
	f := fileData{ByKey: c.byKey, ByInb: c.byInb, UnUp: c.unUp, UnDown: c.unDown, Active: active, UnByInb: c.unByInb}
	b, err := json.Marshal(f)
	if err != nil {
		return
	}
	dir := filepath.Dir(c.dataPath)
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
	_ = os.Rename(tmp.Name(), c.dataPath)
}

// Poll every 1s: a 5s poll misses every connection that starts
// and ends inside the interval; 1s still misses sub-second
// connections, but catches the 1-5s band that used to fall to
// unassigned. Clash /connections is a local in-process endpoint
// and the 60m log query is a few hundred rows, so 1s is cheap.
func (c *Collector) Run(ctx context.Context) {
	tick := time.NewTicker(1 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			c.poll(ctx)
		}
	}
}

// matchRecord picks, for one Clash connection, the log record
// for the same source whose timestamp is closest to the Clash
// start time (within 30s). A record far from the start time is
// a different generation of a reused source port, not this
// connection, so it is rejected rather than misattributed. When
// the Clash start time is unparseable, the caller falls back to
// the latest record for the source.
func matchRecord(records []store.ConnectionRecord, source string, startMs int64) *store.ConnectionRecord {
	var best *store.ConnectionRecord
	var bestDiff int64 = 1 << 62
	for i := range records {
		r := &records[i]
		if r.Source != source {
			continue
		}
		diff := r.Ts - startMs
		if diff < 0 {
			diff = -diff
		}
		if diff < bestDiff {
			bestDiff = diff
			best = r
		}
	}
	if best != nil && bestDiff <= 30_000 {
		return best
	}
	return nil
}

// clashTag extracts the inbound tag from a Clash connection
// type string ("protocol/tag"); "" when the type is absent or
// has no tag part.
func clashTag(typeStr string) string {
	if i := strings.Index(typeStr, "/"); i >= 0 && i+1 < len(typeStr) {
		return typeStr[i+1:]
	}
	return ""
}

func (c *Collector) poll(ctx context.Context) {
	if c.clashAPI == "" || c.st == nil {
		return
	}
	records, mapErr := c.st.RecentConnectionRecords(ctx, time.Now().Add(-60*time.Minute).UnixMilli())
	if mapErr != nil {
		println("traffic: recent connections query failed:", mapErr.Error())
	}
	// Latest-per-source fallback, used only when a Clash start
	// time is unparseable (then time matching is impossible).
	latestBySource := map[string]store.ConnectionRecord{}
	for _, r := range records {
		latestBySource[r.Source] = r
	}
	url := strings.TrimSuffix(c.clashAPI, "/") + "/connections"
	// Proxy must be nil: a stray proxy env would divert a
	// loopback Clash request to a dead egress proxy.
	transport := &http.Transport{Proxy: nil}
	client := &http.Client{Timeout: 3 * time.Second, Transport: transport}
	resp, err := client.Get(url)
	if err != nil {
		println("traffic: clash get failed:", err.Error())
		c.mu.Lock()
		c.saveLocked()
		c.mu.Unlock()
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		println("traffic: clash status:", resp.Status)
		return
	}
	var cr clashResp
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		println("traffic: clash decode failed:", err.Error())
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	seen := map[string]bool{}
	for _, conn := range cr.Connections {
		seen[conn.ID] = true
		src := conn.Metadata.SourceIP + ":" + conn.Metadata.SourcePort
		ac, ok := c.active[conn.ID]
		if !ok {
			ac = &activeConn{}
			c.active[conn.ID] = ac
		}
		if !ac.assigned {
			if startMs, err := time.Parse(time.RFC3339Nano, conn.Start); err == nil {
				if m := matchRecord(records, src, startMs.UnixMilli()); m != nil {
					ac.inbound, ac.user, ac.assigned = m.Tag, m.User, true
				}
			} else if m, ok := latestBySource[src]; ok {
				ac.inbound, ac.user, ac.assigned = m.Tag, m.User, true
			}
		}
		dUp := conn.Upload - ac.lastUp
		dDown := conn.Download - ac.lastDown
		if dUp < 0 {
			dUp = conn.Upload
		}
		if dDown < 0 {
			dDown = conn.Download
		}
		ac.lastUp, ac.lastDown = conn.Upload, conn.Download
		if dUp == 0 && dDown == 0 {
			continue
		}
		if ac.assigned && ac.inbound != "" {
			key := ac.inbound + "\x00" + ac.user
			e, ok := c.byKey[key]
			if !ok {
				e = &Entry{Inbound: ac.inbound, User: ac.user}
				c.byKey[key] = e
			}
			e.Up += dUp
			e.Down += dDown
			ie, ok := c.byInb[ac.inbound]
			if !ok {
				ie = &Entry{Inbound: ac.inbound}
				c.byInb[ac.inbound] = ie
			}
			ie.Up += dUp
			ie.Down += dDown
		} else {
			c.unUp += dUp
			c.unDown += dDown
			if tag := clashTag(conn.Metadata.Type); tag != "" {
				ue, ok := c.unByInb[tag]
				if !ok {
					ue = &Entry{Inbound: tag}
					c.unByInb[tag] = ue
				}
				ue.Up += dUp
				ue.Down += dDown
			}
		}
	}
	// Fallback for connections still unassigned after the 60m
	// window (e.g. a long connection that started before a panel
	// restart and was never assigned): look back 24h, at most
	// once per 5 minutes, so a stuck unassigned connection does
	// not turn every 5s poll into a full-day log scan.
	unassigned := false
	for _, ac := range c.active {
		if !ac.assigned {
			unassigned = true
			break
		}
	}
	if unassigned && time.Since(c.lastWideLookup) >= 5*time.Minute {
		c.lastWideLookup = time.Now()
		if wide, err := c.st.RecentConnectionRecords(ctx, time.Now().Add(-24*time.Hour).UnixMilli()); err == nil {
			for _, conn := range cr.Connections {
				ac := c.active[conn.ID]
				if ac == nil || ac.assigned {
					continue
				}
				src := conn.Metadata.SourceIP + ":" + conn.Metadata.SourcePort
				if startMs, err := time.Parse(time.RFC3339Nano, conn.Start); err == nil {
					if m := matchRecord(wide, src, startMs.UnixMilli()); m != nil {
						ac.inbound, ac.user, ac.assigned = m.Tag, m.User, true
					}
				}
			}
		}
	}
	for id := range c.active {
		if !seen[id] {
			delete(c.active, id)
		}
	}
	c.saveLocked()
}

func (c *Collector) Snapshot() Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := Snapshot{UnassignedUp: c.unUp, UnassignedDown: c.unDown, UpdatedAt: time.Now().Unix()}
	for _, e := range c.byInb {
		s.ByInbound = append(s.ByInbound, *e)
		s.Up += e.Up
		s.Down += e.Down
	}
	for _, e := range c.byKey {
		s.ByUser = append(s.ByUser, *e)
	}
	for _, e := range c.unByInb {
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

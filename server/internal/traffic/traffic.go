// Package traffic collects per-inbound and per-subscription
// traffic for the LOCAL core, mirroring the node agent's
// collector: it polls the Clash /connections endpoint and matches
// each connection to a core-log record by client source address,
// which gives (inbound tag, core username). Totals are persisted
// to a JSON file in the panel data dir so a panel restart does
// not reset the statistics. Short connections that start and end
// between two 5s polls are lost to the breakdown; their bytes are
// counted in Unassigned, never forced onto a subscription.
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
	UpdatedAt      int64   `json:"updatedAt"`
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

type fileData struct {
	ByKey  map[string]*Entry `json:"byKey"`
	ByInb  map[string]*Entry `json:"byInb"`
	UnUp   int64             `json:"unUp"`
	UnDown int64             `json:"unDown"`
}

type Collector struct {
	clashAPI string
	st       *store.Store
	dataPath string

	mu     sync.Mutex
	byKey  map[string]*Entry
	byInb  map[string]*Entry
	unUp   int64
	unDown int64
	active map[string]*activeConn
}

func New(clashAPI string, st *store.Store, dataPath string) *Collector {
	c := &Collector{
		clashAPI: clashAPI,
		st:       st,
		dataPath: dataPath,
		byKey:    map[string]*Entry{},
		byInb:    map[string]*Entry{},
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
}

func (c *Collector) saveLocked() {
	if c.dataPath == "" {
		return
	}
	f := fileData{ByKey: c.byKey, ByInb: c.byInb, UnUp: c.unUp, UnDown: c.unDown}
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

func (c *Collector) Run(ctx context.Context) {
	tick := time.NewTicker(5 * time.Second)
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

func (c *Collector) poll(ctx context.Context) {
	if c.clashAPI == "" || c.st == nil {
		return
	}
	mapping, mapErr := c.st.RecentConnections(ctx, time.Now().Add(-10*time.Minute).UnixMilli())
	if mapErr != nil {
		println("traffic: recent connections query failed:", mapErr.Error())
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
			if m, ok := mapping[src]; ok {
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
	sort.Slice(s.ByInbound, func(i, j int) bool { return s.ByInbound[i].Inbound < s.ByInbound[j].Inbound })
	sort.Slice(s.ByUser, func(i, j int) bool {
		if s.ByUser[i].Inbound != s.ByUser[j].Inbound {
			return s.ByUser[i].Inbound < s.ByUser[j].Inbound
		}
		return s.ByUser[i].User < s.ByUser[j].User
	})
	return s
}


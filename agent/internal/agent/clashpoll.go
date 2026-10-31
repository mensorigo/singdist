package agent

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

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
				// Reject a log entry whose timestamp is far from
				// this connection's start: it is a different
				// generation of a reused source port, and the
				// real log line has not been tailed yet. The
				// connection stays unassigned for this poll and
				// is retried once the tail catches up.
				assign := true
				if lc.tsMs != 0 {
					if start, err := time.Parse(time.RFC3339Nano, c.Start); err == nil {
						diff := lc.tsMs - start.UnixMilli()
						if diff < 0 {
							diff = -diff
						}
						if diff > 30_000 {
							assign = false
						}
					}
				}
				if assign {
					ac.inbound, ac.user, ac.assigned = lc.inbound, lc.user, true
				}
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
			if tag := clashTag(c.Metadata.Type); tag != "" {
				ue, ok := t.unByInb[tag]
				if !ok {
					ue = &TrafficEntry{Inbound: tag}
					t.unByInb[tag] = ue
				}
				ue.Up += dUp
				ue.Down += dDown
			}
		}
	}
	for id := range t.active {
		if !seen[id] {
			delete(t.active, id)
		}
	}
}

func clashTag(typeStr string) string {
	if i := strings.Index(typeStr, "/"); i >= 0 && i+1 < len(typeStr) {
		return typeStr[i+1:]
	}
	return ""
}

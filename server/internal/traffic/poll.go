package traffic

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"singbox-ui/server/internal/store"
)

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
	// Shared client from New: do NOT create a client/transport
	// here, a per-poll Transport leaks one Clash connection
	// per poll (see Collector.httpClient).
	resp, err := c.httpClient.Get(url)
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

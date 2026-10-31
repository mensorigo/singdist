package subs

import (
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"time"
)

func newToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}

// humanBytes renders a byte cap the way the panel talks about it
// (decimal units, 1TB = 10^12); 0 means unlimited.
func humanBytes(b int64) string {
	if b <= 0 {
		return "不限量"
	}
	units := []struct {
		name string
		div  int64
	}{
		{"TB", 1000000000000},
		{"GB", 1000000000},
		{"MB", 1000000},
		{"KB", 1000},
	}
	for _, u := range units {
		if b >= u.div {
			v := float64(b) / float64(u.div)
			if v == float64(int64(v)) {
				return strconv.FormatInt(int64(v), 10) + u.name
			}
			return strconv.FormatFloat(v, 'f', 1, 64) + u.name
		}
	}
	return strconv.FormatInt(b, 10) + "B"
}

// formatExpire renders an expiry instant as a date. ExpireAt is stored
// as end-of-day in the panel timezone, so the UTC date matches the
// panel date for timezones east of UTC.
// formatExpire renders an expiry instant as a date. ExpireAt is stored
// as end-of-day in the panel timezone, so the UTC date matches the
// panel date for timezones east of UTC.
func formatExpire(ts int64) string {
	if ts <= 0 {
		return "永不过期"
	}
	return time.Unix(ts, 0).UTC().Format("2006-01-02")
}

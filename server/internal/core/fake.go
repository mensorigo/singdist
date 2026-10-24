package core

import (
	"context"
	"math"
	"sync"
	"time"
)

// fakeClient produces slowly varying synthetic counters so the panel can be
// developed and demoed without a running sing-box. Selected with
// SINGBOX_UI_CORE=fake.
type fakeClient struct {
	mu        sync.Mutex
	startedAt time.Time
	lastAt    time.Time
	upTotal   int64
	downTotal int64
}

// NewFakeClient returns a CoreClient that fabricates plausible traffic.
func NewFakeClient() CoreClient {
	now := time.Now()
	return &fakeClient{
		startedAt: now,
		lastAt:    now,
		upTotal:   1_284_551_200,   // ~1.2 GiB of "history"
		downTotal: 5_932_884_736,   // ~5.5 GiB
	}
}

// rates returns instantaneous up/down rates (bytes/sec) as smooth
// overlapping sine waves plus a daily-ish swell, deterministic per clock.
func (f *fakeClient) rates(t time.Time) (up, down float64) {
	s := float64(t.Unix()) + float64(t.Nanosecond())/1e9
	up = 420_000 + 380_000*math.Sin(s/17.0) + 120_000*math.Sin(s/3.7+1.3)
	down = 2_600_000 + 2_100_000*math.Sin(s/23.0+0.6) + 500_000*math.Sin(s/5.1)
	if up < 8_000 {
		up = 8_000
	}
	if down < 40_000 {
		down = 40_000
	}
	return up, down
}

func (f *fakeClient) Version(_ context.Context) (string, error) {
	return "sing-box 1.14.3 (fake core)", nil
}

func (f *fakeClient) Totals(_ context.Context) (Totals, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now()
	dt := now.Sub(f.lastAt).Seconds()
	if dt > 0 {
		up, down := f.rates(now)
		f.upTotal += int64(up * dt)
		f.downTotal += int64(down * dt)
		f.lastAt = now
	}
	s := float64(now.Unix())
	conns := int(14 + 9*math.Sin(s/29.0) + 4*math.Sin(s/7.3))
	if conns < 1 {
		conns = 1
	}
	return Totals{
		UploadTotal:   f.upTotal,
		DownloadTotal: f.downTotal,
		ActiveConns:   conns,
	}, nil
}

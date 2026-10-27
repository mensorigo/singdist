package coresync

import (
	"context"
	"sync"
	"time"
)

// Debouncer batches change notifications into one sync push:
//   - push after Quiet (5s) with no new notification (trailing quiet
//     period: each Notify resets the timer);
//   - forced push after Cap (30s) from the first unpushed
//     notification, even while changes keep landing;
//   - on a busy push (a sync is already running), the dirty flag is
//     kept and the push retried after Retry (2s), not queued.
//
// Timer-based, not tick-based: the quiet window is exact, not cut
// into poll intervals. Notify is non-blocking (coalesced): a
// notification arriving while one is already pending just refreshes
// the timestamps; the loop re-arms from the latest change.
const (
	// DebounceQuiet is the trailing quiet period before a push.
	DebounceQuiet = 5 * time.Second
	// DebounceCap is the maximum wait from the first unpushed change.
	DebounceCap = 30 * time.Second
	// DebounceRetry is the wait before retrying a busy push.
	DebounceRetry = 2 * time.Second
)

// RunOutcome tells the debouncer what a push attempt did.
type RunOutcome int

const (
	// RunDone: the push ran (applied, failed, or nothing was needed);
	// the dirty flag clears.
	RunDone RunOutcome = iota
	// RunBusy: a sync is already running; keep dirty, retry soon.
	RunBusy
)

type Debouncer struct {
	run func() RunOutcome

	mu      sync.Mutex
	dirty   bool
	firstAt time.Time
	lastAt  time.Time

	notifyCh chan struct{}
}

func NewDebouncer(run func() RunOutcome) *Debouncer {
	return &Debouncer{run: run, notifyCh: make(chan struct{}, 1)}
}

// Notify marks a change (panel edit via the audit hook, or file drift
// via the poll loop) and resets the quiet timer.
func (d *Debouncer) Notify() {
	now := time.Now()
	d.mu.Lock()
	if !d.dirty {
		d.firstAt = now
	}
	d.dirty = true
	d.lastAt = now
	d.mu.Unlock()
	// Coalesced signal: if one is already pending, the loop will read
	// the fresh timestamps anyway.
	select {
	case d.notifyCh <- struct{}{}:
	default:
	}
}

// Start runs the push loop until ctx ends. Only one loop per
// debouncer; Start is called once by wiring.
func (d *Debouncer) Start(ctx context.Context) {
	go d.loop(ctx)
}

func (d *Debouncer) loop(ctx context.Context) {
	var timer *time.Timer
	var timerCh <-chan time.Time
	arm := func(dur time.Duration) {
		if timer != nil {
			timer.Stop()
		}
		timer = time.NewTimer(dur)
		timerCh = timer.C
	}
	disarm := func() {
		if timer != nil {
			timer.Stop()
			timer = nil
			timerCh = nil
		}
	}
	fire := func() {
		outcome := d.run()
		d.mu.Lock()
		if outcome == RunBusy {
			arm(DebounceRetry)
		} else {
			d.dirty = false
			disarm()
		}
		d.mu.Unlock()
	}
	for {
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		case <-d.notifyCh:
			d.mu.Lock()
			dirty := d.dirty
			capExceeded := dirty && time.Since(d.firstAt) >= DebounceCap
			d.mu.Unlock()
			if !dirty {
				disarm()
				continue
			}
			if capExceeded {
				// Forced push now, even while editing continues.
				if timer != nil {
					timer.Stop()
					timer = nil
					timerCh = nil
				}
				fire()
			} else {
				arm(DebounceQuiet)
			}
		case <-timerCh:
			timer = nil
			timerCh = nil
			d.mu.Lock()
			dirty := d.dirty
			d.mu.Unlock()
			if !dirty {
				continue
			}
			fire()
		}
	}
}

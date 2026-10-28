package nodes

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"singbox-ui/server/internal/certs"
	"singbox-ui/server/internal/logs"
	"singbox-ui/server/internal/store"
)

const (
	// pollInterval is how often the pusher re-examines nodes. The
	// desired-payload hash comparison is cheap (DB reads + cert read),
	// so 5s matches the local drift poll.
	pollInterval = 5 * time.Second
	// statusProbeInterval is how often each node is status-probed for
	// online evidence (the render grace window is 300s, far above it).
	statusProbeInterval = 30 * time.Second
	// pushBackoff is how long a failed push waits before the poller
	// retries it. Manual push-now bypasses it (explicit override).
	pushBackoff = 60 * time.Second
)

// Pusher keeps remote nodes converged with the panel's desired state.
// It never blocks HTTP handlers: the poller runs on a ticker, and
// manual pushes run in goroutines. Per-node try-locks serialize pushes
// to the same node without queueing.
type Pusher struct {
	st      *store.Store
	certMgr *certs.Manager
	client  *Client
	log     *slog.Logger
	logDir  string

	mu    sync.Mutex
	locks map[int64]*sync.Mutex
	probe map[int64]time.Time
}

func NewPusher(st *store.Store, certMgr *certs.Manager, log *slog.Logger, logDir string) *Pusher {
	return &Pusher{
		st:      st,
		certMgr: certMgr,
		client:  NewClient(),
		log:     log,
		logDir:  logDir,
		locks:   map[int64]*sync.Mutex{},
		probe:   map[int64]time.Time{},
	}
}

func (p *Pusher) lockFor(id int64) *sync.Mutex {
	p.mu.Lock()
	defer p.mu.Unlock()
	m, ok := p.locks[id]
	if !ok {
		m = &sync.Mutex{}
		p.locks[id] = m
	}
	return m
}

// Start runs the poll loop until ctx is cancelled.
func (p *Pusher) Start(ctx context.Context) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	p.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.tick(ctx)
		}
	}
}

func (p *Pusher) tick(ctx context.Context) {
	nodes, err := p.st.ListNodes(ctx)
	if err != nil {
		p.log.Warn("node push poll list failed", "error", err)
		return
	}
	for _, n := range nodes {
		n := n
		go p.tickNode(ctx, n)
	}
}

func (p *Pusher) tickNode(ctx context.Context, n store.NodeRow) {
	m := p.lockFor(n.ID)
	if !m.TryLock() {
		return // a push/probe for this node is already running
	}
	defer m.Unlock()

	now := time.Now()
	p.mu.Lock()
	lastProbe := p.probe[n.ID]
	p.mu.Unlock()
	if now.Sub(lastProbe) >= statusProbeInterval {
		p.mu.Lock()
		p.probe[n.ID] = now
		p.mu.Unlock()
		p.probeStatus(ctx, n)
		// Reload: the probe may have updated online state; the
		// push below must use fresh push state.
		fresh, err := p.st.GetNode(ctx, n.ID)
		if err == nil {
			n = *fresh
		}
	}
	if now.Unix() < n.NextRetryAt {
		return
	}
	_, _ = p.push(ctx, n, false)
}

// PushNow forces a push in the background (manual retry / recovery).
// It returns false when a push for the node is already running.
func (p *Pusher) PushNow(ctx context.Context, nodeID int64) (bool, error) {
	n, err := p.st.GetNode(ctx, nodeID)
	if err != nil {
		return false, err
	}
	m := p.lockFor(nodeID)
	if !m.TryLock() {
		return false, nil
	}
	go func() {
		defer m.Unlock()
		_, _ = p.push(context.Background(), *n, true)
	}()
	return true, nil
}

// probeStatus fetches the agent status and records online evidence.
// Failures are silent here (the node page shows the stale timestamp;
// the push path records failures with backoff).
func (p *Pusher) probeStatus(ctx context.Context, n store.NodeRow) {
	st, err := p.client.Status(ctx, n.Address, n.Token)
	if err != nil {
		return
	}
	_ = p.st.RecordNodeOnline(ctx, n.ID, len(st.Inbounds), st.Version, time.Now().Unix())
}

// push builds the desired payload and pushes it unless it matches the
// last successful push (force bypasses that check). It records the
// outcome on the node row and in the audit log.
func (p *Pusher) push(ctx context.Context, n store.NodeRow, force bool) (string, error) {
	now := time.Now().Unix()
	payload, hash, err := BuildPush(ctx, p.st, p.certMgr, n)
	if err != nil {
		p.log.Warn("node push build failed", "node", n.Name, "error", err)
		_ = p.st.RecordNodePushFailure(ctx, n.ID, "error", truncate(err.Error(), 200), now, now+int64(pushBackoff/time.Second))
		p.audit(n, "failed", truncate(err.Error(), 200))
		return "", err
	}
	if !force && hash == n.LastPushHash && n.LastPushHash != "" {
		return "no_change", nil
	}
	res, err := p.client.Push(ctx, n.Address, n.Token, payload)
	if err != nil {
		p.log.Warn("node push failed", "node", n.Name, "error", err)
		_ = p.st.RecordNodePushFailure(ctx, n.ID, "error", truncate(err.Error(), 200), now, now+int64(pushBackoff/time.Second))
		p.audit(n, "failed", truncate(err.Error(), 200))
		return "", err
	}
	switch res.Result {
	case "applied", "no_change":
		_ = p.st.RecordNodePushSuccess(ctx, n.ID, hash, res.Result, now)
		p.audit(n, "success", res.Result)
		return res.Result, nil
	default:
		detail := res.Detail
		if detail == "" {
			detail = res.Result
		}
		if len(res.Notes) > 0 {
			detail += " (" + res.Notes[0] + ")"
		}
		_ = p.st.RecordNodePushFailure(ctx, n.ID, res.Result, truncate(detail, 200), now, now+int64(pushBackoff/time.Second))
		p.audit(n, "failed", truncate(detail, 200))
		return res.Result, errors.New(detail)
	}
}

func (p *Pusher) audit(n store.NodeRow, result, detail string) {
	logs.RecordAudit(context.Background(), p.st, p.log, p.logDir, store.AuditRow{
		At:     time.Now().Unix(),
		Action: "节点推送",
		Target: n.Name,
		Result: result,
		Detail: detail,
	})
}

// Package dash implements the dashboard backend: a sampler that polls the
// core (via core.CoreClient) every 2 seconds and keeps a ring buffer of
// throughput samples, plus host system information read from /proc.
// Traffic deltas are persisted as per-minute buckets (via the Repository
// seam) so the dashboard can show today / all-time totals and a traffic
// trend chart, mirroring the old panel's data model.
package dash

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"singbox-ui/server/internal/core"
)

const (
	tickInterval = 2 * time.Second
	ringSize     = 120 // 120 points * 2s = 4 minutes of history

	// trafficRetention bounds the per-minute bucket history (35 days
	// covers the 30-day trend view plus slack).
	trafficRetention = 35 * 24 * time.Hour

	seedKeyUp   = "traffic_seed_up"
	seedKeyDown = "traffic_seed_down"

	// timezoneSettingKey mirrors settings.keyTimezone; the sampler
	// re-reads it on every tick so a settings change applies without
	// a panel restart.
	timezoneSettingKey = "timezone"
)

// Repository is the persistence seam dash needs from the store layer.
type Repository interface {
	AddTrafficDelta(ctx context.Context, bucketTs, upBytes, downBytes int64) error
	TrafficSum(ctx context.Context, fromTs int64) (upBytes, downBytes int64, err error)
	TrafficEach(ctx context.Context, fromTs int64, fn func(bucketTs, upBytes, downBytes int64) error) error
	PruneTraffic(ctx context.Context, beforeTs int64) error
	GetSetting(ctx context.Context, key string) (string, bool, error)
	SetSetting(ctx context.Context, key, value string) error
}

// Sample is one entry of the throughput ring buffer (bytes per second).
type Sample struct {
	Ts   int64   `json:"ts"`
	Up   float64 `json:"up"`
	Down float64 `json:"down"`
}

// SystemInfo describes the host the panel runs on.
type SystemInfo struct {
	Hostname      string     `json:"hostname"`
	OS            string     `json:"os"`
	CPUPercent    float64    `json:"cpuPercent"`
	LoadAvg       [3]float64 `json:"loadAvg"`
	MemUsedBytes  int64      `json:"memUsedBytes"`
	MemTotalBytes int64      `json:"memTotalBytes"`
	DiskUsedBytes int64      `json:"diskUsedBytes"`
	DiskTotalByte int64      `json:"diskTotalBytes"`
	HostUptimeSec int64      `json:"hostUptimeSecs"`
	NetRxBytes    int64      `json:"netRxBytes"`
	NetTxBytes    int64      `json:"netTxBytes"`
}

// TrafficInfo carries the persisted traffic accounting figures.
type TrafficInfo struct {
	TodayUp   int64 `json:"todayUp"`
	TodayDown int64 `json:"todayDown"`
	TotalUp   int64 `json:"totalUp"`
	TotalDown int64 `json:"totalDown"`
}

// Summary is the JSON payload of GET /api/dashboard/summary.
type Summary struct {
	CoreOnline    bool        `json:"coreOnline"`
	CoreVersion   string      `json:"coreVersion"`
	CoreUptimeSec int64       `json:"coreUptimeSecs"`
	UpRate        float64     `json:"upRate"`
	DownRate      float64     `json:"downRate"`
	UploadTotal   int64       `json:"uploadTotal"`
	DownloadTotal int64       `json:"downloadTotal"`
	ActiveConns   int         `json:"activeConns"`
	DistinctIPs   int         `json:"distinctIPs"`
	Series        []Sample    `json:"series"`
	System        SystemInfo  `json:"system"`
	Traffic       TrafficInfo `json:"traffic"`
	PanelUptimeS  int64       `json:"panelUptimeSecs"`
	Timezone      string      `json:"timezone"`
}

// TrendPoint is one aggregated bucket of the traffic trend series.
type TrendPoint struct {
	Ts   int64 `json:"ts"`
	Up   int64 `json:"up"`
	Down int64 `json:"down"`
}

// Trend is the JSON payload of GET /api/dashboard/trend.
type Trend struct {
	Granularity string       `json:"granularity"`
	Points      []TrendPoint `json:"points"`
}

// panelLocation is the default panel timezone, matching the old
// panel's display timezone. The live value comes from the timezone
// setting; this is only the fallback when none is stored.
func panelLocation() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.Local
	}
	return loc
}

// Sampler polls the core and maintains the rolling dashboard state.
type Sampler struct {
	client core.CoreClient
	repo   Repository
	log    *slog.Logger
	loc    atomic.Pointer[time.Location]

	startedAt time.Time

	mu          sync.RWMutex
	online      bool
	version     string
	coreUptime  int64
	upRate      float64
	downRate    float64
	upTotal     int64
	downTotal   int64
	conns       int
	distinctIPs int
	series      []Sample
	seedUp      int64
	seedDown    int64
	seedLoaded  bool
	lastPruneAt time.Time

	// baseline for rate computation
	haveBase  bool
	lastUp    int64
	lastDown  int64
	lastAt    time.Time
	cpuReader *cpuReader
	hostname  string
}

// NewSampler creates a Sampler reading from client and persisting traffic
// history through repo (repo may be nil, disabling history).
func NewSampler(client core.CoreClient, repo Repository, log *slog.Logger) *Sampler {
	hostname, _ := os.Hostname()
	s := &Sampler{
		client:    client,
		repo:      repo,
		log:       log,
		startedAt: time.Now(),
		series:    make([]Sample, 0, ringSize),
		cpuReader: newCPUReader(),
		hostname:  hostname,
	}
	s.loc.Store(panelLocation())
	return s
}

// currentLoc returns the panel timezone: the stored setting when it
// parses, the default otherwise. The tick loop refreshes it from the
// setting every 2s, so a settings change applies without restart.
func (s *Sampler) currentLoc() *time.Location {
	if loc := s.loc.Load(); loc != nil {
		return loc
	}
	return panelLocation()
}

// Start runs the sampling loop until ctx is cancelled.
func (s *Sampler) Start(ctx context.Context) {
	s.tick(ctx) // sample immediately so the first page load has data
	t := time.NewTicker(tickInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.tick(ctx)
		}
	}
}

func (s *Sampler) tick(ctx context.Context) {
	// Refresh the panel timezone from settings on every tick, ahead
	// of the core call: a settings change must apply without restart
	// even while the core is unreachable (the persistence section
	// below early-returns in that case).
	if s.repo != nil {
		locCtx, locCancel := context.WithTimeout(context.Background(), 3*time.Second)
		if v, ok, err := s.repo.GetSetting(locCtx, timezoneSettingKey); err == nil && ok {
			if loc, lerr := time.LoadLocation(strings.TrimSpace(v)); lerr == nil {
				s.loc.Store(loc)
			}
		}
		locCancel()
	}
	callCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	totals, err := s.client.Totals(callCtx)
	cancel()

	s.mu.Lock()
	now := time.Now()

	if err != nil {
		if s.online {
			s.log.Warn("dash: core became unreachable", "err", err)
		}
		s.online = false
		s.coreUptime = 0
		s.upRate, s.downRate = 0, 0
		// Core unreachable: no connections are observable through it,
		// so both connection figures reset instead of going stale.
		s.conns, s.distinctIPs = 0, 0
		s.pushLocked(Sample{Ts: now.Unix(), Up: 0, Down: 0})
		s.mu.Unlock()
		return
	}

	if version, verr := s.client.Version(ctx); verr == nil && version != "" {
		s.version = version
	}
	if !s.online {
		s.log.Info("dash: core reachable", "version", s.version)
		// Reset rate baseline: a (re)appearing core may have restarted
		// and reset its counters, so the next delta is meaningless.
		s.haveBase = false
	}
	s.online = true
	s.coreUptime = coreUptimeSecs()
	s.upTotal, s.downTotal, s.conns = totals.UploadTotal, totals.DownloadTotal, totals.ActiveConns
	s.distinctIPs = totals.DistinctIPs

	var upDelta, downDelta int64
	var upRate, downRate float64
	if s.haveBase {
		dt := now.Sub(s.lastAt).Seconds()
		ud := nonNegativeDelta(totals.UploadTotal, s.lastUp)
		dd := nonNegativeDelta(totals.DownloadTotal, s.lastDown)
		upDelta, downDelta = int64(ud), int64(dd)
		if dt > 0 {
			upRate = ud / dt
			downRate = dd / dt
		}
	}
	s.upRate, s.downRate = upRate, downRate
	s.lastUp, s.lastDown, s.lastAt = totals.UploadTotal, totals.DownloadTotal, now
	needSeed := s.repo != nil && !s.seedLoaded
	s.haveBase = true
	s.pushLocked(Sample{Ts: now.Unix(), Up: upRate, Down: downRate})
	s.mu.Unlock()

	// Persistence outside the state lock: seed capture, bucket deltas,
	// and hourly pruning of expired buckets.
	if s.repo == nil {
		return
	}
	writeCtx, wCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer wCancel()
	if needSeed {
		s.loadOrInitSeed(writeCtx, totals.UploadTotal, totals.DownloadTotal)
	}
	if upDelta > 0 || downDelta > 0 {
		bucket := now.Truncate(time.Minute).Unix()
		if err := s.repo.AddTrafficDelta(writeCtx, bucket, upDelta, downDelta); err != nil {
			s.log.Warn("dash: persist traffic delta", "err", err)
		}
	}
	s.mu.Lock()
	duePrune := now.Sub(s.lastPruneAt) > time.Hour
	if duePrune {
		s.lastPruneAt = now
	}
	s.mu.Unlock()
	if duePrune {
		cutoff := now.Add(-trafficRetention).Unix()
		if err := s.repo.PruneTraffic(writeCtx, cutoff); err != nil {
			s.log.Warn("dash: prune traffic history", "err", err)
		}
	}
}

// loadOrInitSeed loads the persisted traffic seed (the core counters
// captured when this panel first observed the core, so all-time totals
// include traffic from before per-minute accounting started). When no
// seed exists yet, the current counters become the seed.
func (s *Sampler) loadOrInitSeed(ctx context.Context, upTotal, downTotal int64) {
	get := func(key string) (int64, bool) {
		v, ok, err := s.repo.GetSetting(ctx, key)
		if err != nil || !ok {
			return 0, false
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return 0, false
		}
		return n, true
	}
	up, upOK := get(seedKeyUp)
	down, downOK := get(seedKeyDown)
	if !upOK || !downOK {
		up, down = upTotal, downTotal
		if err := s.repo.SetSetting(ctx, seedKeyUp, strconv.FormatInt(up, 10)); err != nil {
			s.log.Warn("dash: persist traffic seed", "err", err)
			return
		}
		if err := s.repo.SetSetting(ctx, seedKeyDown, strconv.FormatInt(down, 10)); err != nil {
			s.log.Warn("dash: persist traffic seed", "err", err)
			return
		}
	}
	s.mu.Lock()
	s.seedUp, s.seedDown, s.seedLoaded = up, down, true
	s.mu.Unlock()
}

func nonNegativeDelta(now, prev int64) float64 {
	if now < prev {
		return 0 // counter reset (core restart): never report negative rates
	}
	return float64(now - prev)
}

func (s *Sampler) pushLocked(sample Sample) {
	s.series = append(s.series, sample)
	if len(s.series) > ringSize {
		s.series = append([]Sample(nil), s.series[len(s.series)-ringSize:]...)
	}
}

// trafficLocked-free accounting figures, read from the repository.
func (s *Sampler) trafficInfo() TrafficInfo {
	if s.repo == nil {
		return TrafficInfo{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	loc := s.currentLoc()
	now := time.Now().In(loc)
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	todayUp, todayDown, err := s.repo.TrafficSum(ctx, midnight.Unix())
	if err != nil {
		s.log.Warn("dash: traffic sum (today)", "err", err)
		return TrafficInfo{}
	}
	allUp, allDown, err := s.repo.TrafficSum(ctx, 0)
	if err != nil {
		s.log.Warn("dash: traffic sum (all)", "err", err)
		return TrafficInfo{}
	}
	s.mu.RLock()
	seedUp, seedDown := s.seedUp, s.seedDown
	s.mu.RUnlock()
	return TrafficInfo{
		TodayUp:   todayUp,
		TodayDown: todayDown,
		TotalUp:   seedUp + allUp,
		TotalDown: seedDown + allDown,
	}
}

// Summary returns a copy of the current dashboard state.
func (s *Sampler) Summary() Summary {
	s.mu.RLock()
	series := make([]Sample, len(s.series))
	copy(series, s.series)
	sum := Summary{
		CoreOnline:    s.online,
		CoreVersion:   s.version,
		CoreUptimeSec: s.coreUptime,
		UpRate:        s.upRate,
		DownRate:      s.downRate,
		UploadTotal:   s.upTotal,
		DownloadTotal: s.downTotal,
		ActiveConns:   s.conns,
		DistinctIPs:   s.distinctIPs,
		Series:        series,
		PanelUptimeS:  int64(time.Since(s.startedAt).Seconds()),
	}
	s.mu.RUnlock()

	diskUsed, diskTotal := diskUsage("/")
	netRx, netTx := netTotals()
	sum.System = SystemInfo{
		Hostname:      s.hostname,
		OS:            osPrettyName(),
		CPUPercent:    s.cpuReader.Percent(),
		LoadAvg:       loadAvg(),
		MemUsedBytes:  memUsed(),
		MemTotalBytes: memTotal(),
		DiskUsedBytes: diskUsed,
		DiskTotalByte: diskTotal,
		HostUptimeSec: systemUptimeSecs(),
		NetRxBytes:    netRx,
		NetTxBytes:    netTx,
	}
	sum.Traffic = s.trafficInfo()
	sum.Timezone = s.currentLoc().String()
	return sum
}

// Trend aggregates the persisted per-minute buckets into a dense series
// for the requested granularity: minute = last 60 minutes, hour = last
// 24 hours, day = last 30 days (panel timezone). Unknown granularities
// fall back to day.
func (s *Sampler) Trend(granularity string) Trend {
	var window time.Duration
	var bucketSecs int64
	switch granularity {
	case "minute":
		window, bucketSecs = time.Hour, 60
	case "hour":
		window, bucketSecs = 24 * time.Hour, 3600
	default:
		granularity, window, bucketSecs = "day", 30*24*time.Hour, 86400
	}

	now := time.Now()
	from := now.Add(-window)
	start := s.bucketStart(from, granularity)

	points := make([]TrendPoint, 0, int(window.Seconds())/int(bucketSecs)+1)
	index := make(map[int64]int)
	for t := start; t <= now.Unix(); t = s.nextBucket(t, granularity) {
		index[t] = len(points)
		points = append(points, TrendPoint{Ts: t})
	}

	if s.repo != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		err := s.repo.TrafficEach(ctx, start, func(bucketTs, up, down int64) error {
			key := s.bucketStart(time.Unix(bucketTs, 0), granularity)
			if i, ok := index[key]; ok {
				points[i].Up += up
				points[i].Down += down
			}
			return nil
		})
		if err != nil {
			s.log.Warn("dash: traffic trend", "err", err)
		}
	}
	return Trend{Granularity: granularity, Points: points}
}

// bucketStart aligns t to the start of its display bucket.
func (s *Sampler) bucketStart(t time.Time, granularity string) int64 {
	switch granularity {
	case "minute":
		return t.Truncate(time.Minute).Unix()
	case "hour":
		return t.Truncate(time.Hour).Unix()
	default: // day: panel-timezone midnight
		loc := s.currentLoc()
		lt := t.In(loc)
		return time.Date(lt.Year(), lt.Month(), lt.Day(), 0, 0, 0, 0, loc).Unix()
	}
}

func (s *Sampler) nextBucket(ts int64, granularity string) int64 {
	switch granularity {
	case "minute":
		return ts + 60
	case "hour":
		return ts + 3600
	default:
		return time.Unix(ts, 0).In(s.currentLoc()).AddDate(0, 0, 1).Unix()
	}
}

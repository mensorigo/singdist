package dash

import (
	"context"
	"time"

)

// Summary is the JSON payload of GET /api/dashboard/summary.
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

// trafficLocked-free accounting figures, read from the repository.
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

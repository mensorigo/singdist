package dash

import (
	"context"
	"time"
)

// TrendPoint is one aggregated bucket of the traffic trend series.
type TrendPoint struct {
	Ts   int64 `json:"ts"`
	Up   int64 `json:"up"`
	Down int64 `json:"down"`
}

// Trend is the JSON payload of GET /api/dashboard/trend.
// Trend is the JSON payload of GET /api/dashboard/trend.
type Trend struct {
	Granularity string       `json:"granularity"`
	Points      []TrendPoint `json:"points"`
}

// panelLocation is the default panel timezone, matching the old
// panel's display timezone. The live value comes from the timezone
// setting; this is only the fallback when none is stored.
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
		window, bucketSecs = 24*time.Hour, 3600
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

-- Per-minute traffic deltas collected by the dashboard sampler from the
-- core's cumulative counters. Aggregated on read for the dashboard's
-- today / total figures and the traffic trend chart (minute / hour / day).
-- Buckets older than the retention window are pruned by the sampler.
CREATE TABLE IF NOT EXISTS traffic_buckets (
    bucket_ts  INTEGER PRIMARY KEY,
    up_bytes   INTEGER NOT NULL DEFAULT 0,
    down_bytes INTEGER NOT NULL DEFAULT 0
);

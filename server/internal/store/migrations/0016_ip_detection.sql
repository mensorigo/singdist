-- IP detection: per-subscription IP history plus a global IP blacklist.
-- subscription_ips is an aggregate, not an event log: one row per
-- (subscription, IP) with first/last seen. It is fed by two sources:
-- subscription-link fetches (exact per subscription) and core
-- connection-log rows (user + source, aggregated by the follower and
-- by a one-time backfill over existing core_logs).
CREATE TABLE IF NOT EXISTS subscription_ips (
  sub_id INTEGER NOT NULL,
  ip TEXT NOT NULL,
  first_seen_ms INTEGER NOT NULL,
  last_seen_ms INTEGER NOT NULL,
  PRIMARY KEY (sub_id, ip)
);
CREATE INDEX IF NOT EXISTS idx_subscription_ips_sub ON subscription_ips(sub_id);

-- Global blacklist: one row per blocked IP. The generated core config
-- carries a single route rule (source_ip_cidr -> block) for the whole
-- set, so blocking is panel-wide, never per subscription.
CREATE TABLE IF NOT EXISTS ip_blacklist (
  ip TEXT PRIMARY KEY,
  created_ms INTEGER NOT NULL,
  note TEXT NOT NULL DEFAULT ''
);

-- The sync baseline needs to remember which blacklist IPs the panel
-- generated last time: without it, a rule the panel wrote earlier
-- cannot be told apart from a hand-written rule, and unblacklisting
-- would leave the stale rule in the file forever.
ALTER TABLE sync_state ADD COLUMN blacklist_ips TEXT NOT NULL DEFAULT '';

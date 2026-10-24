-- Templates bundle inbounds; a subscription binds one template and is
-- the unit of distribution and governance (one link = one core user).
-- limit_bytes is a single combined up+down cap (0 = unlimited);
-- expire_at is unix seconds (0 = never); used_* are collector-fed and
-- stay 0 until the traffic collector lands.
CREATE TABLE IF NOT EXISTS templates (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  inbound_ids TEXT NOT NULL DEFAULT '[]',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_templates_name ON templates (name COLLATE NOCASE);

CREATE TABLE IF NOT EXISTS subscriptions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  uuid TEXT NOT NULL,
  token TEXT NOT NULL,
  template_id INTEGER NOT NULL,
  limit_bytes INTEGER NOT NULL DEFAULT 0,
  used_up INTEGER NOT NULL DEFAULT 0,
  used_down INTEGER NOT NULL DEFAULT 0,
  expire_at INTEGER NOT NULL DEFAULT 0,
  max_ips INTEGER NOT NULL DEFAULT 0,
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_subscriptions_name ON subscriptions (name COLLATE NOCASE);
CREATE UNIQUE INDEX IF NOT EXISTS idx_subscriptions_token ON subscriptions (token);
CREATE UNIQUE INDEX IF NOT EXISTS idx_subscriptions_uuid ON subscriptions (uuid);

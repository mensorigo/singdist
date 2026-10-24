-- Sync engine bookkeeping: every sync/rollback attempt is logged, and
-- every outgoing core config is archived as a restorable version.
CREATE TABLE IF NOT EXISTS sync_logs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  at INTEGER NOT NULL,
  action TEXT NOT NULL,
  result TEXT NOT NULL,
  hash TEXT NOT NULL DEFAULT '',
  detail TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS config_versions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  at INTEGER NOT NULL,
  hash TEXT NOT NULL,
  file TEXT NOT NULL,
  source TEXT NOT NULL DEFAULT ''
);

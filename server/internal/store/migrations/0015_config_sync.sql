-- Config change log: every external (file-origin) change the sync
-- engine classifies, plus passthrough and dropped-illegal records.
-- Panel's own syncs are NOT logged here (they live in sync_logs).
-- Action values: updated | added | deleted | passthrough |
-- dropped_illegal | reverted | restore_note. Secrets (uuid, password,
-- private keys) are never stored here; those rows carry empty values
-- and a detail note instead.
CREATE TABLE IF NOT EXISTS config_change_logs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  at INTEGER NOT NULL,
  sync_log_id INTEGER NOT NULL DEFAULT 0,
  action TEXT NOT NULL,
  item TEXT NOT NULL,
  old_value TEXT NOT NULL DEFAULT '',
  new_value TEXT NOT NULL DEFAULT '',
  detail TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_config_change_logs_id ON config_change_logs (id DESC);
CREATE INDEX IF NOT EXISTS idx_config_change_logs_sync ON config_change_logs (sync_log_id, id DESC);

-- Single-row sync baseline: the config bytes as last written by a
-- successful sync (or last observed on a no_change), plus the set of
-- inbound tags the panel preserves verbatim (passthrough) rather than
-- manages. The classifier diffs the file against this baseline, so
-- external edits are isolated exactly instead of guessed from panel
-- state. passthrough_tags is a JSON array of strings.
CREATE TABLE IF NOT EXISTS sync_state (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  last_doc TEXT NOT NULL DEFAULT '',
  last_hash TEXT NOT NULL DEFAULT '',
  passthrough_tags TEXT NOT NULL DEFAULT '[]',
  updated_at INTEGER NOT NULL DEFAULT 0
);

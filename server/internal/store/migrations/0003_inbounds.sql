-- Inbounds are the panel's desired-state records for sing-box inbounds.
-- params holds the protocol-specific settings as a JSON object; applying
-- them to the running core is a separate (later) sync step.
CREATE TABLE IF NOT EXISTS inbounds (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  protocol TEXT NOT NULL,
  port INTEGER NOT NULL,
  params TEXT NOT NULL DEFAULT '{}',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_inbounds_name ON inbounds (name COLLATE NOCASE);
CREATE UNIQUE INDEX IF NOT EXISTS idx_inbounds_port ON inbounds (port);

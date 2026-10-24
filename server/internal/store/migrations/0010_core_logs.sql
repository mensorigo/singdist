-- Core + client-connection log records, ingested from the sing-box
-- journal by a background follower (see internal/logs/ingest.go).
-- Panel operations keep their own table (audit_logs, 0009): this table
-- holds only core system lines (kind='core') and grouped connection
-- records (kind='connection'). Rows older than 90 days are pruned by
-- the follower, mirroring the audit retention.
CREATE TABLE IF NOT EXISTS core_logs (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    ts          INTEGER NOT NULL,
    kind        TEXT    NOT NULL,
    level       TEXT    NOT NULL DEFAULT '',
    message     TEXT    NOT NULL DEFAULT '',
    source      TEXT    NOT NULL DEFAULT '',
    destination TEXT    NOT NULL DEFAULT '',
    tag         TEXT    NOT NULL DEFAULT '',
    user        TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_core_logs_kind_id ON core_logs (kind, id DESC);
CREATE INDEX IF NOT EXISTS idx_core_logs_source ON core_logs (source, id DESC);
CREATE INDEX IF NOT EXISTS idx_core_logs_user ON core_logs (user, id DESC);

-- Single-row ingestion checkpoint: the journal cursor the follower has
-- durably consumed, so a panel restart resumes without re-inserting.
CREATE TABLE IF NOT EXISTS log_ingest_state (
    id         INTEGER PRIMARY KEY CHECK (id = 1),
    cursor     TEXT    NOT NULL DEFAULT '',
    updated_at INTEGER NOT NULL DEFAULT 0
);

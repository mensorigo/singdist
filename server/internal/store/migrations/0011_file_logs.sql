-- File following replaces journal ingestion: the follower tracks a
-- panel-managed log file by (inode, offset) instead of a journal
-- cursor. core_logs itself is unchanged; only a time index is added
-- for range queries and pruning.
CREATE INDEX IF NOT EXISTS idx_core_logs_kind_ts ON core_logs (kind, ts DESC);

-- Single-row file-follower checkpoint: which file and byte offset
-- have been durably consumed, so a panel restart resumes without
-- re-inserting (or skipping) lines.
CREATE TABLE IF NOT EXISTS log_file_state (
    id         INTEGER PRIMARY KEY CHECK (id = 1),
    path       TEXT    NOT NULL DEFAULT '',
    inode      INTEGER NOT NULL DEFAULT 0,
    offset     INTEGER NOT NULL DEFAULT 0,
    updated_at INTEGER NOT NULL DEFAULT 0
);

-- log_ingest_state is intentionally NOT dropped: pre-0011 binaries
-- still read the journal cursor from it on rollback, and it is tiny.
-- New code never touches it.

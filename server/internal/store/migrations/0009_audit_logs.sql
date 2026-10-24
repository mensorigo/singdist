-- Panel-operation audit log: one row per mutating API call, written by
-- the audit middleware. Reads are never logged (dashboard polls alone
-- would flood the table). Rows older than 90 days are pruned on write.
CREATE TABLE IF NOT EXISTS audit_logs (
    id     INTEGER PRIMARY KEY AUTOINCREMENT,
    at     INTEGER NOT NULL,
    action TEXT    NOT NULL,
    target TEXT    NOT NULL DEFAULT '',
    result TEXT    NOT NULL,
    detail TEXT    NOT NULL DEFAULT ''
);

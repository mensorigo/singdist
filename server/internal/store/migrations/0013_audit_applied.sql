-- Audit rows gain the sync-coverage state: NULL means not applicable
-- (old rows, auth rows, reads) or unknown; '' means a core-affecting
-- edit not yet covered by a sync (pending); a hash means the sync with
-- that config hash applied it.
ALTER TABLE audit_logs ADD COLUMN applied_hash TEXT;

-- Audit rows gain a change summary: a short old -> new note for safe
-- (non-secret) fields on updates, empty when there is nothing to say.
ALTER TABLE audit_logs ADD COLUMN summary TEXT NOT NULL DEFAULT '';

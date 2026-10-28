-- Blocked sync input: after a post-write runtime failure (restart or
-- health) and rollback, the failed merged-config hash is recorded so
-- auto-sync skips the same input until the panel data or the file
-- changes. A manual sync bypasses the block (explicit override).
-- Empty means no block.
ALTER TABLE sync_state ADD COLUMN blocked_hash TEXT NOT NULL DEFAULT '';

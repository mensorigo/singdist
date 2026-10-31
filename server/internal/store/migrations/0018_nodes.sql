-- Remote nodes (central push targets): one row per agent node.
-- The token is stored in cleartext, like subscription tokens: the panel
-- database is already the secret store, and the token must be presented
-- on every push (it cannot be a one-way hash).
-- tls_mode is reserved for future hardening (pin/mtls); v1 only
-- implements skip-verify. usage_ratio is reserved for future weighted
-- traffic aggregation (default 1.0, unused in v1).
-- last_push_hash is only updated on a successful push, so a failed push
-- retries on the next poll (after next_retry_at). last_online_at is
-- updated by any successful contact (push or status probe); the
-- subscription renderer treats a node as online when it was seen within
-- 300 seconds.
CREATE TABLE IF NOT EXISTS nodes (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL UNIQUE,
	address TEXT NOT NULL,
	token TEXT NOT NULL,
	public_host TEXT NOT NULL DEFAULT '',
	tls_mode TEXT NOT NULL DEFAULT 'skip-verify',
	usage_ratio REAL NOT NULL DEFAULT 1.0,
	last_push_hash TEXT NOT NULL DEFAULT '',
	last_push_at INTEGER NOT NULL DEFAULT 0,
	last_push_result TEXT NOT NULL DEFAULT '',
	last_error TEXT NOT NULL DEFAULT '',
	last_online_at INTEGER NOT NULL DEFAULT 0,
	last_inbound_count INTEGER NOT NULL DEFAULT 0,
	last_version TEXT NOT NULL DEFAULT '',
	next_retry_at INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);

-- Inbound -> remote node assignment. An inbound with no rows here
-- serves locally only (the historical behavior); each row adds one
-- remote node. Local is always implied, never stored.
-- No foreign keys, matching the rest of the schema: orphan rows (from
-- a deleted inbound or node) are inert because every reader joins
-- against the live inbounds/nodes tables.
CREATE TABLE IF NOT EXISTS inbound_nodes (
	inbound_id INTEGER NOT NULL,
	node_id INTEGER NOT NULL,
	PRIMARY KEY (inbound_id, node_id)
);
CREATE INDEX IF NOT EXISTS idx_inbound_nodes_node ON inbound_nodes (node_id);

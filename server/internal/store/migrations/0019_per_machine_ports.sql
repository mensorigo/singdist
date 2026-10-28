-- Per-machine ports: an inbound may run on a subset of machines, and
-- may use a different port on each node.
-- inbound_nodes.port: 0 = use the inbound's own port on that node;
-- >0 = override port for that node only.
-- inbounds.run_local: 1 = the local machine also runs this inbound
-- (the historical behavior, and the default for existing rows);
-- 0 = the local machine skips it (it runs only on its assigned nodes).
ALTER TABLE inbound_nodes ADD COLUMN port INTEGER NOT NULL DEFAULT 0;
ALTER TABLE inbounds ADD COLUMN run_local INTEGER NOT NULL DEFAULT 1;
-- The local port uniqueness only constrains inbounds that actually
-- run locally. Same index name as before, so the conflict-error
-- mapping in inbounds.conflictErr keeps working.
DROP INDEX IF EXISTS idx_inbounds_proto_port;
CREATE UNIQUE INDEX IF NOT EXISTS idx_inbounds_proto_port
    ON inbounds (protocol, port) WHERE run_local = 1;

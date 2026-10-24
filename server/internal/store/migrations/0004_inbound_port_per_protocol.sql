-- Port uniqueness is per transport: VLESS (TCP) and Hysteria2 (UDP) may
-- share a port number; two inbounds of the same protocol may not.
DROP INDEX IF EXISTS idx_inbounds_port;
CREATE UNIQUE INDEX IF NOT EXISTS idx_inbounds_proto_port
    ON inbounds (protocol, port);

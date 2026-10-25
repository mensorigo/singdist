-- Panel-operation audit rows gain the source IP (the TCP peer at request
-- time), so failed logins can be traced to their origin. Old rows keep
-- an empty IP.
ALTER TABLE audit_logs ADD COLUMN ip TEXT NOT NULL DEFAULT '';

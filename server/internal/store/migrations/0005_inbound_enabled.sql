-- Inbounds can be parked without deleting them: a disabled inbound stays
-- in the panel but is excluded when the desired state is applied to the
-- core (sync phase). Existing inbounds default to enabled.
ALTER TABLE inbounds ADD COLUMN enabled INTEGER NOT NULL DEFAULT 1;

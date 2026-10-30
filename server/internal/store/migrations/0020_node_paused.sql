-- Node pause flag (panel-side only): 0 = enabled, 1 = paused.
-- A paused node is skipped by the push poller and excluded from
-- subscription rendering, but nothing is deleted anywhere: its
-- record, assignments, inbound definitions and core config all stay,
-- and enabling restores it. Existing nodes default to enabled.
ALTER TABLE nodes ADD COLUMN paused INTEGER NOT NULL DEFAULT 0;

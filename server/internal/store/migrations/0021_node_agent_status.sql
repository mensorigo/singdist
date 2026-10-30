-- Node agent status snapshot + desired push hash, for the node card's
-- sync status: the 30s status probe stores the agent's full status
-- reply here (core unit, Clash, per-inbound listening, last result,
-- blocked hash) so the card can show it without a live call per list;
-- the pusher stores the desired payload hash here on every build, so
-- the card can tell "push applied and up to date" (desired == last)
-- from "a change is still pending" without rebuilding the payload.
ALTER TABLE nodes ADD COLUMN desired_push_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE nodes ADD COLUMN agent_status TEXT NOT NULL DEFAULT '';
ALTER TABLE nodes ADD COLUMN agent_status_at INTEGER NOT NULL DEFAULT 0;

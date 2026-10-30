package agent

// Version is the agent binary version reported in /api/agent/status so
// the central panel can show it on the node page and warn on mismatch.
// Bump it on releases; it is intentionally a plain constant (no VCS
// stamping: the agent builds with -trimpath from a working tree).
const Version = "0.2.9"

package coresync

import (
	"context"

	"singbox-ui/server/internal/store"
)

// applyImports executes the classifier's import ops one by one.
// Every op is best-effort and post-state verified: a service call
// may report an error from a view-only step (viewByID reads the
// target file) while its store write already landed, so success is
// decided by re-reading the row, not by the call's error alone.
// A failed op never aborts the sync — it is converted to a
// dropped_illegal or reverted row and the next sync retries it.
func (s *Service) log(ctx context.Context, action, result, hash, detail string) (store.SyncLogRow, error) {
	// A sync row must be recorded even when the pipeline ctx timed
	// out (otherwise a timeout leaves no trace at all).
	if ctx.Err() != nil {
		ctx = context.Background()
	}
	row := store.SyncLogRow{At: nowUnix(), Action: action, Result: result, Hash: hash, Detail: detail}
	id, err := s.st.AddSyncLog(ctx, row)
	if err != nil {
		return row, err
	}
	row.ID = id
	return row, nil
}

// ListLog backs the panel's sync log view.
func (s *Service) ListLog(ctx context.Context, limit int) ([]store.SyncLogRow, error) {
	return s.st.ListSyncLogs(ctx, limit)
}

// ListChanges backs the Logs page "config" category.
// ListChanges backs the Logs page "config" category.
func (s *Service) ListChanges(ctx context.Context, f store.ConfigChangeFilter) ([]store.ConfigChangeRow, error) {
	return s.st.ListConfigChanges(ctx, f)
}

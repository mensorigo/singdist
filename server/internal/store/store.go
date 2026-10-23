// Package store is the only package that talks SQL. It owns the SQLite
// database handle and the embedded, sequentially applied migrations.
//
// Other modules never see *sql.DB; they consume the narrow method sets
// exposed here through interfaces they declare themselves.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

// ErrNotFound is returned by lookups whose row does not exist.
var ErrNotFound = errors.New("store: not found")

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Store wraps the database handle.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the SQLite database at path, enables WAL
// and a busy timeout, then applies any pending migrations.
func Open(ctx context.Context, path string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}
	// SQLite + a single writer connection avoids SQLITE_BUSY contention
	// between the sampler and request handlers at panel scale.
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: ping: %w", err)
	}
	s := &Store{db: db}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database handle.
func (s *Store) Close() error { return s.db.Close() }

// migrate applies embedded migrations in filename order, each in its own
// transaction, recording progress in schema_migrations.
func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at INTEGER NOT NULL DEFAULT (unixepoch())
	)`); err != nil {
		return fmt.Errorf("store: create schema_migrations: %w", err)
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("store: read migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		version, err := strconv.Atoi(strings.SplitN(name, "_", 2)[0])
		if err != nil {
			return fmt.Errorf("store: bad migration name %q", name)
		}
		var done int
		err = s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM schema_migrations WHERE version = ?`, version).Scan(&done)
		if err != nil {
			return fmt.Errorf("store: check migration %d: %w", version, err)
		}
		if done > 0 {
			continue
		}
		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("store: read migration %s: %w", name, err)
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("store: begin migration %d: %w", version, err)
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("store: apply migration %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES (?)`, version); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("store: record migration %d: %w", version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("store: commit migration %d: %w", version, err)
		}
	}
	return nil
}

// --- admins ---------------------------------------------------------------

// CountAdmins returns how many admin accounts exist.
func (s *Store) CountAdmins(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM admins`).Scan(&n)
	return n, err
}

// CreateAdmin inserts an admin account and returns its id.
func (s *Store) CreateAdmin(ctx context.Context, username, passwordHash string, now int64) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO admins (username, password_hash, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		username, passwordHash, now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// AdminCredentials returns the id and bcrypt hash for username.
func (s *Store) AdminCredentials(ctx context.Context, username string) (int64, string, bool, error) {
	var id int64
	var hash string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, password_hash FROM admins WHERE username = ?`, username).Scan(&id, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", false, nil
	}
	if err != nil {
		return 0, "", false, err
	}
	return id, hash, true, nil
}

// AdminUsername returns the username for the given admin id.
func (s *Store) AdminUsername(ctx context.Context, id int64) (string, bool, error) {
	var username string
	err := s.db.QueryRowContext(ctx, `SELECT username FROM admins WHERE id = ?`, id).Scan(&username)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return username, true, nil
}

// UpdateAdminPassword replaces the bcrypt hash of an admin.
func (s *Store) UpdateAdminPassword(ctx context.Context, id int64, passwordHash string, now int64) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE admins SET password_hash = ?, updated_at = ? WHERE id = ?`, passwordHash, now, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// --- sessions ---------------------------------------------------------------

// CreateSession stores a session by its token hash.
func (s *Store) CreateSession(ctx context.Context, tokenHash string, adminID, expiresAt, now int64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (token_hash, admin_id, expires_at, created_at) VALUES (?, ?, ?, ?)`,
		tokenHash, adminID, expiresAt, now)
	return err
}

// SessionAdmin returns the admin id and expiry for a token hash.
func (s *Store) SessionAdmin(ctx context.Context, tokenHash string) (int64, int64, bool, error) {
	var adminID, expiresAt int64
	err := s.db.QueryRowContext(ctx,
		`SELECT admin_id, expires_at FROM sessions WHERE token_hash = ?`, tokenHash).Scan(&adminID, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, false, nil
	}
	if err != nil {
		return 0, 0, false, err
	}
	return adminID, expiresAt, true, nil
}

// DeleteSession removes a session if present.
func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
	return err
}

// DeleteExpiredSessions removes sessions whose expiry has passed.
func (s *Store) DeleteExpiredSessions(ctx context.Context, now int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, now)
	return err
}

// --- settings ---------------------------------------------------------------

// GetSetting returns a settings value by key.
func (s *Store) GetSetting(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT v FROM settings WHERE k = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

// SetSetting upserts a settings value.
func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO settings (k, v) VALUES (?, ?) ON CONFLICT(k) DO UPDATE SET v = excluded.v`, key, value)
	return err
}

// --- traffic history --------------------------------------------------------

// AddTrafficDelta adds byte deltas to the minute bucket starting at bucketTs
// (unix seconds, minute-aligned), creating the bucket when needed.
func (s *Store) AddTrafficDelta(ctx context.Context, bucketTs, upBytes, downBytes int64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO traffic_buckets (bucket_ts, up_bytes, down_bytes) VALUES (?, ?, ?)
		 ON CONFLICT(bucket_ts) DO UPDATE SET
		   up_bytes = up_bytes + excluded.up_bytes,
		   down_bytes = down_bytes + excluded.down_bytes`,
		bucketTs, upBytes, downBytes)
	return err
}

// TrafficSum returns the summed bytes in buckets at or after fromTs.
func (s *Store) TrafficSum(ctx context.Context, fromTs int64) (upBytes, downBytes int64, err error) {
	err = s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(up_bytes), 0), COALESCE(SUM(down_bytes), 0)
		 FROM traffic_buckets WHERE bucket_ts >= ?`, fromTs).Scan(&upBytes, &downBytes)
	return upBytes, downBytes, err
}

// TrafficEach streams buckets at or after fromTs, oldest first, invoking fn
// per bucket. Returning an error from fn aborts the scan with that error.
func (s *Store) TrafficEach(ctx context.Context, fromTs int64, fn func(bucketTs, upBytes, downBytes int64) error) error {
	rows, err := s.db.QueryContext(ctx,
		`SELECT bucket_ts, up_bytes, down_bytes FROM traffic_buckets
		 WHERE bucket_ts >= ? ORDER BY bucket_ts`, fromTs)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var ts, up, down int64
		if err := rows.Scan(&ts, &up, &down); err != nil {
			return err
		}
		if err := fn(ts, up, down); err != nil {
			return err
		}
	}
	return rows.Err()
}

// PruneTraffic deletes buckets starting before beforeTs.
func (s *Store) PruneTraffic(ctx context.Context, beforeTs int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM traffic_buckets WHERE bucket_ts < ?`, beforeTs)
	return err
}

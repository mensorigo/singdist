package store

import (
	"context"
	"fmt"
)

// SubscriptionRow is one stored subscription: one link, one core user
// (uuid). Used traffic is fed by the collector (later phase).
type SubscriptionRow struct {
	ID         int64
	Name       string
	UUID       string
	Token      string
	TemplateID int64
	LimitBytes int64
	UsedUp     int64
	UsedDown   int64
	ExpireAt   int64
	MaxIPs     int
	Enabled    bool
	CreatedAt  int64
	UpdatedAt  int64
}

const subscriptionCols = `id, name, uuid, token, template_id, limit_bytes,
	used_up, used_down, expire_at, max_ips, enabled, created_at, updated_at`

func scanSubscription(scan func(dest ...any) error) (SubscriptionRow, error) {
	var r SubscriptionRow
	var enabled int64
	err := scan(&r.ID, &r.Name, &r.UUID, &r.Token, &r.TemplateID, &r.LimitBytes,
		&r.UsedUp, &r.UsedDown, &r.ExpireAt, &r.MaxIPs, &enabled, &r.CreatedAt, &r.UpdatedAt)
	r.Enabled = enabled != 0
	return r, err
}

func (s *Store) ListSubscriptions(ctx context.Context) ([]SubscriptionRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+subscriptionCols+` FROM subscriptions ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("store: list subscriptions: %w", err)
	}
	defer rows.Close()
	out := []SubscriptionRow{}
	for rows.Next() {
		r, err := scanSubscription(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("store: scan subscription: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) GetSubscription(ctx context.Context, id int64) (*SubscriptionRow, error) {
	r, err := scanSubscription(s.db.QueryRowContext(ctx,
		`SELECT `+subscriptionCols+` FROM subscriptions WHERE id = ?`, id).Scan)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// GetSubscriptionByToken resolves the public /sub credential.
func (s *Store) GetSubscriptionByToken(ctx context.Context, token string) (*SubscriptionRow, error) {
	r, err := scanSubscription(s.db.QueryRowContext(ctx,
		`SELECT `+subscriptionCols+` FROM subscriptions WHERE token = ?`, token).Scan)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *Store) CreateSubscription(ctx context.Context, r SubscriptionRow) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO subscriptions (name, uuid, token, template_id, limit_bytes,
			expire_at, max_ips, enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		r.Name, r.UUID, r.Token, r.TemplateID, r.LimitBytes, r.ExpireAt, r.MaxIPs,
		r.CreatedAt, r.UpdatedAt)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateSubscription(ctx context.Context, id int64, name string, templateID, limitBytes, expireAt int64, maxIPs int, now int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE subscriptions SET name = ?, template_id = ?, limit_bytes = ?,
			expire_at = ?, max_ips = ?, updated_at = ? WHERE id = ?`,
		name, templateID, limitBytes, expireAt, maxIPs, now, id)
	return err
}

func (s *Store) DeleteSubscription(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM subscriptions WHERE id = ?`, id)
	return err
}

// SetSubscriptionEnabled flips the enabled flag; returns false when the
// id does not exist.
func (s *Store) SetSubscriptionEnabled(ctx context.Context, id int64, enabled bool, now int64) (bool, error) {
	v := 0
	if enabled {
		v = 1
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE subscriptions SET enabled = ?, updated_at = ? WHERE id = ?`, v, now, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// UpdateSubscriptionUsage writes the collector's per-subscription
// totals (actual client traffic through that subscription, local +
// node, never NIC totals or unassigned bytes). It touches only the
// usage columns so the 30s usage sync does not churn updated_at.
func (s *Store) UpdateSubscriptionUsage(ctx context.Context, id, up, down int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE subscriptions SET used_up = ?, used_down = ? WHERE id = ?`, up, down, id)
	return err
}

// UpdateSubscriptionToken rotates the distribution token (link reset).
func (s *Store) UpdateSubscriptionToken(ctx context.Context, id int64, token string, now int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE subscriptions SET token = ?, updated_at = ? WHERE id = ?`, token, now, id)
	return err
}

// UpdateSubscriptionUUID rotates the core user identity （转换UUID）.
func (s *Store) UpdateSubscriptionUUID(ctx context.Context, id int64, uuid string, now int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE subscriptions SET uuid = ?, updated_at = ? WHERE id = ?`, uuid, now, id)
	return err
}

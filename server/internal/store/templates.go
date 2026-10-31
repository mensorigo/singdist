package store

import (
	"context"
	"fmt"
)

// TemplateRow is one stored template: a named set of inbound ids
// (JSON array). Interpreting membership is the subs package's job.
type TemplateRow struct {
	ID         int64
	Name       string
	InboundIDs string
	Groups     string
	CreatedAt  int64
	UpdatedAt  int64
}

func (s *Store) ListTemplates(ctx context.Context) ([]TemplateRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, inbound_ids, groups, created_at, updated_at
		 FROM templates ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("store: list templates: %w", err)
	}
	defer rows.Close()
	out := []TemplateRow{}
	for rows.Next() {
		var r TemplateRow
		if err := rows.Scan(&r.ID, &r.Name, &r.InboundIDs, &r.Groups, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, fmt.Errorf("store: scan template: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) GetTemplate(ctx context.Context, id int64) (*TemplateRow, error) {
	var r TemplateRow
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, inbound_ids, groups, created_at, updated_at
		 FROM templates WHERE id = ?`, id).
		Scan(&r.ID, &r.Name, &r.InboundIDs, &r.Groups, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *Store) CreateTemplate(ctx context.Context, name, inboundIDs, groups string, now int64) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO templates (name, inbound_ids, groups, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?)`, name, inboundIDs, groups, now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateTemplate(ctx context.Context, id int64, name, inboundIDs, groups string, now int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE templates SET name = ?, inbound_ids = ?, groups = ?, updated_at = ? WHERE id = ?`,
		name, inboundIDs, groups, now, id)
	return err
}

func (s *Store) DeleteTemplate(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM templates WHERE id = ?`, id)
	return err
}

// CountSubscriptionsUsingTemplate backs the delete guard: a template
// bound by subscriptions cannot be removed.
func (s *Store) CountSubscriptionsUsingTemplate(ctx context.Context, templateID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM subscriptions WHERE template_id = ?`, templateID).Scan(&n)
	return n, err
}

// Package templates owns subscription templates: a template is a named
// set of inbounds that subscriptions bind to. It is an independent
// module — subscriptions reference templates by id through the store,
// never through this package.
package templates

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"singbox-ui/server/internal/store"
)

// Error mirrors the other modules' typed API failure.
type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }

func errValidation(msg string) *Error {
	return &Error{Status: 400, Code: "validation", Message: msg}
}

func errNotFound(what string) *Error {
	return &Error{Status: 404, Code: what + "_not_found", Message: what + " not found"}
}

func errConflict(code, msg string) *Error {
	return &Error{Status: 409, Code: code, Message: msg}
}

func nameConflictErr(err error) error {
	msg := err.Error()
	if strings.Contains(msg, "templates.name") || strings.Contains(msg, "idx_templates_name") {
		return errConflict("name_conflict", "another record already uses this name")
	}
	return err
}

type Service struct {
	st *store.Store
}

func NewService(st *store.Store) *Service { return &Service{st: st} }

func nowUnix() int64 { return time.Now().Unix() }

func validateName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 64 {
		return "", errValidation("name must be 1-64 characters")
	}
	return name, nil
}

// --- views --------------------------------------------------------------------

type TemplateView struct {
	ID                int64   `json:"id"`
	Name              string  `json:"name"`
	InboundIDs        []int64 `json:"inboundIds"`
	SubscriptionCount int     `json:"subscriptionCount"`
	CreatedAt         int64   `json:"createdAt"`
	UpdatedAt         int64   `json:"updatedAt"`
}

type TemplateRequest struct {
	Name       string  `json:"name"`
	InboundIDs []int64 `json:"inboundIds"`
}

// --- service ------------------------------------------------------------------

func (s *Service) normalizeInboundIDs(ctx context.Context, ids []int64) ([]int64, string, error) {
	rows, err := s.st.ListInbounds(ctx)
	if err != nil {
		return nil, "", err
	}
	exists := map[int64]bool{}
	for _, r := range rows {
		exists[r.ID] = true
	}
	seen := map[int64]bool{}
	out := []int64{}
	for _, id := range ids {
		if !exists[id] {
			return nil, "", errValidation(fmt.Sprintf("inbound %d not found", id))
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	b, _ := json.Marshal(out)
	return out, string(b), nil
}

func (s *Service) templateView(ctx context.Context, r store.TemplateRow) (TemplateView, error) {
	var ids []int64
	if err := json.Unmarshal([]byte(r.InboundIDs), &ids); err != nil {
		ids = []int64{}
	}
	n, err := s.st.CountSubscriptionsUsingTemplate(ctx, r.ID)
	if err != nil {
		return TemplateView{}, err
	}
	return TemplateView{
		ID: r.ID, Name: r.Name, InboundIDs: ids, SubscriptionCount: n,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}, nil
}

func (s *Service) ListTemplates(ctx context.Context) ([]TemplateView, error) {
	rows, err := s.st.ListTemplates(ctx)
	if err != nil {
		return nil, err
	}
	out := []TemplateView{}
	for _, r := range rows {
		v, err := s.templateView(ctx, r)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *Service) CreateTemplate(ctx context.Context, req TemplateRequest) (TemplateView, error) {
	name, err := validateName(req.Name)
	if err != nil {
		return TemplateView{}, err
	}
	_, idsJSON, err := s.normalizeInboundIDs(ctx, req.InboundIDs)
	if err != nil {
		return TemplateView{}, err
	}
	id, err := s.st.CreateTemplate(ctx, name, idsJSON, nowUnix())
	if err != nil {
		return TemplateView{}, nameConflictErr(err)
	}
	row, err := s.st.GetTemplate(ctx, id)
	if err != nil {
		return TemplateView{}, err
	}
	return s.templateView(ctx, *row)
}

func (s *Service) UpdateTemplate(ctx context.Context, id int64, req TemplateRequest) (TemplateView, string, error) {
	old, err := s.st.GetTemplate(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return TemplateView{}, "", errNotFound("template")
		}
		return TemplateView{}, "", err
	}
	name, err := validateName(req.Name)
	if err != nil {
		return TemplateView{}, "", err
	}
	ids, idsJSON, err := s.normalizeInboundIDs(ctx, req.InboundIDs)
	if err != nil {
		return TemplateView{}, "", err
	}
	if err := s.st.UpdateTemplate(ctx, id, name, idsJSON, nowUnix()); err != nil {
		return TemplateView{}, "", nameConflictErr(err)
	}
	var parts []string
	if old.Name != name {
		parts = append(parts, "名称 "+old.Name+"→"+name)
	}
	var oldIDs []int64
	_ = json.Unmarshal([]byte(old.InboundIDs), &oldIDs)
	if len(oldIDs) != len(ids) {
		parts = append(parts, "成员 "+strconv.Itoa(len(oldIDs))+"→"+strconv.Itoa(len(ids)))
	}
	row, err := s.st.GetTemplate(ctx, id)
	if err != nil {
		return TemplateView{}, "", err
	}
	v, err := s.templateView(ctx, *row)
	return v, strings.Join(parts, "；"), err
}

func (s *Service) DeleteTemplate(ctx context.Context, id int64) error {
	if _, err := s.st.GetTemplate(ctx, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errNotFound("template")
		}
		return err
	}
	n, err := s.st.CountSubscriptionsUsingTemplate(ctx, id)
	if err != nil {
		return err
	}
	if n > 0 {
		return errConflict("template_in_use",
			fmt.Sprintf("template is used by %d subscription(s); unbind them first", n))
	}
	return s.st.DeleteTemplate(ctx, id)
}

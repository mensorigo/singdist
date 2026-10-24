// Package subs owns subscription records: a subscription binds one
// template (owned by internal/templates) and is the unit of
// distribution and governance (one link = one core user). Rendering
// and serving subscription content is a separate later phase; nothing
// here touches the core.
package subs

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"singbox-ui/server/internal/store"

	"github.com/google/uuid"
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

func nameConflictErr(err error, table string) error {
	msg := err.Error()
	if strings.Contains(msg, table+".name") || strings.Contains(msg, "idx_"+table+"_name") {
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

// --- subscriptions ------------------------------------------------------------

type SubscriptionView struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	UUID         string `json:"uuid"`
	Token        string `json:"token"`
	TemplateID   int64  `json:"templateId"`
	TemplateName string `json:"templateName"`
	LimitBytes   int64  `json:"limitBytes"`
	UsedUp       int64  `json:"usedUp"`
	UsedDown     int64  `json:"usedDown"`
	UsedBytes    int64  `json:"usedBytes"`
	ExpireAt     int64  `json:"expireAt"`
	MaxIPs       int    `json:"maxIps"`
	Enabled      bool   `json:"enabled"`
	Status       string `json:"status"` // active | disabled | expired | over_quota
	CreatedAt    int64  `json:"createdAt"`
	UpdatedAt    int64  `json:"updatedAt"`
}

func statusOf(r store.SubscriptionRow, now int64) string {
	switch {
	case !r.Enabled:
		return "disabled"
	case r.ExpireAt > 0 && r.ExpireAt < now:
		return "expired"
	case r.LimitBytes > 0 && r.UsedUp+r.UsedDown >= r.LimitBytes:
		return "over_quota"
	default:
		return "active"
	}
}

func subscriptionView(r store.SubscriptionRow, templateName string, now int64) SubscriptionView {
	return SubscriptionView{
		ID: r.ID, Name: r.Name, UUID: r.UUID, Token: r.Token,
		TemplateID: r.TemplateID, TemplateName: templateName,
		LimitBytes: r.LimitBytes, UsedUp: r.UsedUp, UsedDown: r.UsedDown,
		UsedBytes: r.UsedUp + r.UsedDown,
		ExpireAt:  r.ExpireAt, MaxIPs: r.MaxIPs, Enabled: r.Enabled,
		Status:    statusOf(r, now),
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

type SubscriptionRequest struct {
	Name       string `json:"name"`
	TemplateID int64  `json:"templateId"`
	LimitBytes int64  `json:"limitBytes"`
	ExpireAt   int64  `json:"expireAt"`
	MaxIPs     int    `json:"maxIps"`
}

func (s *Service) validateSubscriptionFields(ctx context.Context, req SubscriptionRequest) (string, error) {
	name, err := validateName(req.Name)
	if err != nil {
		return "", err
	}
	if _, err := s.st.GetTemplate(ctx, req.TemplateID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", errValidation("template not found")
		}
		return "", err
	}
	if req.LimitBytes < 0 {
		return "", errValidation("limitBytes must be >= 0")
	}
	if req.ExpireAt < 0 {
		return "", errValidation("expireAt must be >= 0")
	}
	if req.MaxIPs < 0 {
		return "", errValidation("maxIps must be >= 0")
	}
	return name, nil
}

func (s *Service) ListSubscriptions(ctx context.Context) ([]SubscriptionView, error) {
	rows, err := s.st.ListSubscriptions(ctx)
	if err != nil {
		return nil, err
	}
	templates, err := s.st.ListTemplates(ctx)
	if err != nil {
		return nil, err
	}
	names := map[int64]string{}
	for _, t := range templates {
		names[t.ID] = t.Name
	}
	now := nowUnix()
	out := []SubscriptionView{}
	for _, r := range rows {
		out = append(out, subscriptionView(r, names[r.TemplateID], now))
	}
	return out, nil
}

func (s *Service) viewByID(ctx context.Context, id int64) (SubscriptionView, error) {
	row, err := s.st.GetSubscription(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SubscriptionView{}, errNotFound("subscription")
		}
		return SubscriptionView{}, err
	}
	tplName := ""
	if t, err := s.st.GetTemplate(ctx, row.TemplateID); err == nil {
		tplName = t.Name
	}
	return subscriptionView(*row, tplName, nowUnix()), nil
}

func newToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}

func (s *Service) CreateSubscription(ctx context.Context, req SubscriptionRequest) (SubscriptionView, error) {
	name, err := s.validateSubscriptionFields(ctx, req)
	if err != nil {
		return SubscriptionView{}, err
	}
	now := nowUnix()
	id, err := s.st.CreateSubscription(ctx, store.SubscriptionRow{
		Name: name, UUID: uuid.NewString(), Token: newToken(),
		TemplateID: req.TemplateID, LimitBytes: req.LimitBytes,
		ExpireAt: req.ExpireAt, MaxIPs: req.MaxIPs,
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return SubscriptionView{}, nameConflictErr(err, "subscriptions")
	}
	return s.viewByID(ctx, id)
}

func (s *Service) UpdateSubscription(ctx context.Context, id int64, req SubscriptionRequest) (SubscriptionView, error) {
	if _, err := s.st.GetSubscription(ctx, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SubscriptionView{}, errNotFound("subscription")
		}
		return SubscriptionView{}, err
	}
	name, err := s.validateSubscriptionFields(ctx, req)
	if err != nil {
		return SubscriptionView{}, err
	}
	if err := s.st.UpdateSubscription(ctx, id, name, req.TemplateID, req.LimitBytes, req.ExpireAt, req.MaxIPs, nowUnix()); err != nil {
		return SubscriptionView{}, nameConflictErr(err, "subscriptions")
	}
	return s.viewByID(ctx, id)
}

func (s *Service) DeleteSubscription(ctx context.Context, id int64) error {
	if _, err := s.st.GetSubscription(ctx, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errNotFound("subscription")
		}
		return err
	}
	return s.st.DeleteSubscription(ctx, id)
}

func (s *Service) SetSubscriptionEnabled(ctx context.Context, id int64, enabled bool) (SubscriptionView, error) {
	ok, err := s.st.SetSubscriptionEnabled(ctx, id, enabled, nowUnix())
	if err != nil {
		return SubscriptionView{}, err
	}
	if !ok {
		return SubscriptionView{}, errNotFound("subscription")
	}
	return s.viewByID(ctx, id)
}

// ResetSubscriptionToken rotates the token; the old subscription link
// stops working as soon as distribution serves the new one.
func (s *Service) ResetSubscriptionToken(ctx context.Context, id int64) (SubscriptionView, error) {
	if _, err := s.st.GetSubscription(ctx, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SubscriptionView{}, errNotFound("subscription")
		}
		return SubscriptionView{}, err
	}
	if err := s.st.UpdateSubscriptionToken(ctx, id, newToken(), nowUnix()); err != nil {
		return SubscriptionView{}, err
	}
	return s.viewByID(ctx, id)
}

// ResetSubscriptionUUID rotates the core user identity （转换UUID in the
// old panel). Existing client configs carry the old uuid and stop
// working once the new identity is synced to the core.
func (s *Service) ResetSubscriptionUUID(ctx context.Context, id int64) (SubscriptionView, error) {
	if _, err := s.st.GetSubscription(ctx, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SubscriptionView{}, errNotFound("subscription")
		}
		return SubscriptionView{}, err
	}
	if err := s.st.UpdateSubscriptionUUID(ctx, id, uuid.NewString(), nowUnix()); err != nil {
		return SubscriptionView{}, err
	}
	return s.viewByID(ctx, id)
}

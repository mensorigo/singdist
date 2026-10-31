// Package subs owns subscription records: a subscription binds one
// template (owned by internal/templates) and is the unit of
// distribution and governance (one link = one client identity).
// Password protocols get one core user per (subscription, inbound)
// in coresync generation, so online-IP attribution must sum those
// renamed names too; rendering and serving subscription content is
// a separate later phase; nothing here touches the core.
package subs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
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
func nowUnix() int64                      { return time.Now().Unix() }
func validateName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 64 {
		return "", errValidation("name must be 1-64 characters")
	}
	return name, nil
}

// --- subscriptions ------------------------------------------------------------
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

// OnlineIPResult is the online-IP snapshot: per-subscription distinct-IP
// counts and the panel-wide distinct-IP total (one IP using several
// subscriptions counts once in the total).
// OnlineIPResult is the online-IP snapshot: per-subscription distinct-IP
// counts and the panel-wide distinct-IP total (one IP using several
// subscriptions counts once in the total).
type OnlineIPResult struct {
	Counts map[int64]int `json:"counts"`
	Total  int           `json:"total"`
}

// OnlineIPCounts counts distinct source IPs per subscription from
// connection log rows. The window is a fixed 30s: long enough to smooth
// bursty traffic, short enough to still mean "online". The display
// refresh rate itself follows the dashboard interval setting.
// OnlineIPCounts counts distinct source IPs per subscription from
// connection log rows. The window is a fixed 30s: long enough to smooth
// bursty traffic, short enough to still mean "online". The display
// refresh rate itself follows the dashboard interval setting.
func (s *Service) OnlineIPCounts(ctx context.Context) (OnlineIPResult, error) {
	sinceMs := time.Now().UnixMilli() - 30*1000
	sets, err := s.st.OnlineIPs(ctx, sinceMs)
	if err != nil {
		return OnlineIPResult{}, err
	}
	rows, err := s.st.ListSubscriptions(ctx)
	if err != nil {
		return OnlineIPResult{}, err
	}
	// Core usernames for password protocols are renamed per
	// (subscription, inbound) in coresync generation, so a
	// subscription's IPs are the union over its plain name (vless)
	// and its renamed names on its template's inbounds. This is
	// computed from the stored template/inbound rows, never by
	// parsing a core username, so hyphenated names cannot mislead
	// it. Any load failure falls back to plain-name attribution.
	inboundByID := map[int64]store.InboundRow{}
	if inbounds, err := s.st.ListInbounds(ctx); err == nil {
		for _, ib := range inbounds {
			inboundByID[ib.ID] = ib
		}
	}
	templateInboundIDs := map[int64][]int64{}
	if templates, err := s.st.ListTemplates(ctx); err == nil {
		for _, t := range templates {
			var ids []int64
			if err := json.Unmarshal([]byte(t.InboundIDs), &ids); err == nil {
				templateInboundIDs[t.ID] = ids
			}
		}
	}
	res := OnlineIPResult{Counts: map[int64]int{}}
	union := map[string]struct{}{}
	for _, r := range rows {
		ips := map[string]struct{}{}
		for ip := range sets[r.Name] {
			ips[ip] = struct{}{}
		}
		for _, ibID := range templateInboundIDs[r.TemplateID] {
			if ib, ok := inboundByID[ibID]; ok {
				for ip := range sets[r.Name+"-"+ib.Name] {
					ips[ip] = struct{}{}
				}
			}
		}
		res.Counts[r.ID] = len(ips)
		for ip := range ips {
			union[ip] = struct{}{}
		}
	}
	res.Total = len(union)
	return res, nil
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

// humanBytes renders a byte cap the way the panel talks about it
// (decimal units, 1TB = 10^12); 0 means unlimited.
func (s *Service) UpdateSubscription(ctx context.Context, id int64, req SubscriptionRequest) (SubscriptionView, string, error) {
	old, err := s.viewByID(ctx, id)
	if err != nil {
		return SubscriptionView{}, "", err
	}
	name, err := s.validateSubscriptionFields(ctx, req)
	if err != nil {
		return SubscriptionView{}, "", err
	}
	if err := s.st.UpdateSubscription(ctx, id, name, req.TemplateID, req.LimitBytes, req.ExpireAt, req.MaxIPs, nowUnix()); err != nil {
		return SubscriptionView{}, "", nameConflictErr(err, "subscriptions")
	}
	// Summary covers safe fields only: never uuid/token.
	newTplName := ""
	if req.TemplateID != 0 {
		if t, err := s.st.GetTemplate(ctx, req.TemplateID); err == nil {
			newTplName = t.Name
		}
	}
	var parts []string
	if old.Name != name {
		parts = append(parts, "名称 "+old.Name+"→"+name)
	}
	if old.TemplateName != newTplName {
		parts = append(parts, "模板 "+old.TemplateName+"→"+newTplName)
	}
	if old.LimitBytes != req.LimitBytes {
		parts = append(parts, "上限 "+humanBytes(old.LimitBytes)+"→"+humanBytes(req.LimitBytes))
	}
	if old.ExpireAt != req.ExpireAt {
		parts = append(parts, "到期 "+formatExpire(old.ExpireAt)+"→"+formatExpire(req.ExpireAt))
	}
	if old.MaxIPs != req.MaxIPs {
		parts = append(parts, "最大IP数 "+strconv.Itoa(old.MaxIPs)+"→"+strconv.Itoa(req.MaxIPs))
	}
	v, err := s.viewByID(ctx, id)
	return v, strings.Join(parts, "；"), err
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

func (s *Service) SetSubscriptionEnabled(ctx context.Context, id int64, enabled bool) (SubscriptionView, string, error) {
	old, err := s.st.GetSubscription(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SubscriptionView{}, "", errNotFound("subscription")
		}
		return SubscriptionView{}, "", err
	}
	ok, err := s.st.SetSubscriptionEnabled(ctx, id, enabled, nowUnix())
	if err != nil {
		return SubscriptionView{}, "", err
	}
	if !ok {
		return SubscriptionView{}, "", errNotFound("subscription")
	}
	v, err := s.viewByID(ctx, id)
	if err != nil {
		return SubscriptionView{}, "", err
	}
	summary := ""
	if old.Enabled != enabled {
		if enabled {
			summary = "停用→启用"
		} else {
			summary = "启用→停用"
		}
	}
	return v, summary, nil
}

// ResetSubscriptionToken rotates the token; the old subscription link
// stops working as soon as distribution serves the new one.

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

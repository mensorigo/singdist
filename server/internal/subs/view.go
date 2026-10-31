package subs

import (
	"singbox-ui/server/internal/store"
)

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

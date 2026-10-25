package subs

import (
	"errors"
	"net/http"
	"strconv"

	"singbox-ui/server/internal/apix"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) RegisterProtected(rg *gin.RouterGroup) {
	rg.GET("/subscriptions", h.listSubscriptions)
	rg.GET("/subscriptions/online-ips", h.onlineIPCounts)
	rg.POST("/subscriptions", h.createSubscription)
	rg.PATCH("/subscriptions/:id", h.updateSubscription)
	rg.PATCH("/subscriptions/:id/enabled", h.setSubscriptionEnabled)
	rg.POST("/subscriptions/:id/reset-token", h.resetSubscriptionToken)
	rg.POST("/subscriptions/:id/reset-uuid", h.resetSubscriptionUUID)
	rg.DELETE("/subscriptions/:id", h.deleteSubscription)
}

// --- subscriptions ---

func (h *Handler) listSubscriptions(c *gin.Context) {
	res, err := h.svc.ListSubscriptions(c.Request.Context())
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

func (h *Handler) onlineIPCounts(c *gin.Context) {
	res, err := h.svc.OnlineIPCounts(c.Request.Context())
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

func (h *Handler) createSubscription(c *gin.Context) {
	var req SubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apix.Fail(c, http.StatusBadRequest, "invalid_request", "request body must be JSON with name, templateId, limitBytes, expireAt, maxIps")
		return
	}
	v, err := h.svc.CreateSubscription(c.Request.Context(), req)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, v)
}

func (h *Handler) updateSubscription(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req SubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apix.Fail(c, http.StatusBadRequest, "invalid_request", "request body must be JSON with name, templateId, limitBytes, expireAt, maxIps")
		return
	}
	v, summary, err := h.svc.UpdateSubscription(c.Request.Context(), id, req)
	if err != nil {
		failErr(c, err)
		return
	}
	c.Set("auditSummary", summary)
	c.JSON(http.StatusOK, v)
}

func (h *Handler) setSubscriptionEnabled(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Enabled == nil {
		apix.Fail(c, http.StatusBadRequest, "invalid_request", "request body must be JSON with enabled")
		return
	}
	v, summary, err := h.svc.SetSubscriptionEnabled(c.Request.Context(), id, *req.Enabled)
	if err != nil {
		failErr(c, err)
		return
	}
	c.Set("auditSummary", summary)
	c.JSON(http.StatusOK, v)
}

func (h *Handler) resetSubscriptionToken(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	v, err := h.svc.ResetSubscriptionToken(c.Request.Context(), id)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

func (h *Handler) resetSubscriptionUUID(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	v, err := h.svc.ResetSubscriptionUUID(c.Request.Context(), id)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

func (h *Handler) deleteSubscription(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := h.svc.DeleteSubscription(c.Request.Context(), id); err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// --- helpers ---

func pathID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		apix.Fail(c, http.StatusBadRequest, "invalid_request", "invalid id")
		return 0, false
	}
	return id, true
}

func failErr(c *gin.Context, err error) {
	var se *Error
	if errors.As(err, &se) {
		apix.Fail(c, se.Status, se.Code, se.Message)
		return
	}
	apix.FailInternal(c)
}

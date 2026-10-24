package inbounds

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
	rg.GET("/inbounds", h.list)
	rg.POST("/inbounds", h.create)
	rg.PATCH("/inbounds/:id", h.update)
	rg.PATCH("/inbounds/:id/enabled", h.setEnabled)
	rg.DELETE("/inbounds/:id", h.remove)
	rg.POST("/inbounds/reality-keypair", h.keypair)
}

func (h *Handler) list(c *gin.Context) {
	res, err := h.svc.List(c.Request.Context())
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

func (h *Handler) create(c *gin.Context) {
	var req CreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apix.Fail(c, http.StatusBadRequest, "invalid_request", "request body must be JSON with name, protocol, port, params")
		return
	}
	v, err := h.svc.Create(c.Request.Context(), req)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, v)
}

func (h *Handler) update(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req UpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apix.Fail(c, http.StatusBadRequest, "invalid_request", "request body must be JSON with name, port, params")
		return
	}
	v, err := h.svc.Update(c.Request.Context(), id, req)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

func (h *Handler) setEnabled(c *gin.Context) {
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
	v, err := h.svc.SetEnabled(c.Request.Context(), id, *req.Enabled)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

func (h *Handler) remove(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), id); err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) keypair(c *gin.Context) {
	kp, err := GenerateRealityKeypair()
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, kp)
}

func pathID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		apix.Fail(c, http.StatusBadRequest, "invalid_request", "invalid inbound id")
		return 0, false
	}
	return id, true
}

func failErr(c *gin.Context, err error) {
	var ie *Error
	if errors.As(err, &ie) {
		apix.Fail(c, ie.Status, ie.Code, ie.Message)
		return
	}
	apix.FailInternal(c)
}

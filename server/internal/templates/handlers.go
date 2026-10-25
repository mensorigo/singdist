package templates

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
	rg.GET("/templates", h.listTemplates)
	rg.POST("/templates", h.createTemplate)
	rg.PATCH("/templates/:id", h.updateTemplate)
	rg.DELETE("/templates/:id", h.deleteTemplate)
}

func (h *Handler) listTemplates(c *gin.Context) {
	res, err := h.svc.ListTemplates(c.Request.Context())
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

func (h *Handler) createTemplate(c *gin.Context) {
	var req TemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apix.Fail(c, http.StatusBadRequest, "invalid_request", "request body must be JSON with name, inboundIds")
		return
	}
	v, err := h.svc.CreateTemplate(c.Request.Context(), req)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, v)
}

func (h *Handler) updateTemplate(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req TemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apix.Fail(c, http.StatusBadRequest, "invalid_request", "request body must be JSON with name, inboundIds")
		return
	}
	v, summary, err := h.svc.UpdateTemplate(c.Request.Context(), id, req)
	if err != nil {
		failErr(c, err)
		return
	}
	c.Set("auditSummary", summary)
	c.JSON(http.StatusOK, v)
}

func (h *Handler) deleteTemplate(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := h.svc.DeleteTemplate(c.Request.Context(), id); err != nil {
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

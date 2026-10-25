package coresync

import (
	"errors"
	"net/http"
	"strconv"

	"singbox-ui/server/internal/apix"
	"singbox-ui/server/internal/store"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) RegisterProtected(rg *gin.RouterGroup) {
	rg.GET("/sync/status", h.status)
	rg.POST("/sync", h.sync)
	rg.GET("/sync/log", h.listLog)
	rg.GET("/sync/changes", h.listChanges)
}

func (h *Handler) status(c *gin.Context) {
	v, err := h.svc.Status(c.Request.Context())
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

func (h *Handler) sync(c *gin.Context) {
	row, err := h.svc.Sync(c.Request.Context())
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, row)
}

func (h *Handler) listLog(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	rows, err := h.svc.ListLog(c.Request.Context(), limit)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, rows)
}

// listChanges backs the Logs page "config" category: the per-field
// change log written by each sync (file imports, passthrough,
// drops, reverts). Same cursor shape as the other log lists:
// ?before=<id>&limit=<n>&q=<keyword>.
func (h *Handler) listChanges(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	before, _ := strconv.ParseInt(c.Query("before"), 10, 64)
	rows, err := h.svc.ListChanges(c.Request.Context(), store.ConfigChangeFilter{
		Keyword: c.Query("q"),
		Before:  before,
		Limit:   limit,
	})
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, rows)
}

func failErr(c *gin.Context, err error) {
	var se *Error
	if errors.As(err, &se) {
		apix.Fail(c, se.Status, se.Code, se.Message)
		return
	}
	apix.FailInternal(c)
}

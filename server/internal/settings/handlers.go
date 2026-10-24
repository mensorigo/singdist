package settings

import (
	"errors"
	"net/http"

	"singbox-ui/server/internal/apix"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) RegisterProtected(rg *gin.RouterGroup) {
	rg.GET("/settings", h.get)
	rg.PATCH("/settings", h.update)
}

func (h *Handler) get(c *gin.Context) {
	v, err := h.svc.Get(c.Request.Context())
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

func (h *Handler) update(c *gin.Context) {
	var req Update
	if err := c.ShouldBindJSON(&req); err != nil {
		apix.Fail(c, http.StatusBadRequest, "invalid_request", "request body must be a JSON settings patch")
		return
	}
	v, err := h.svc.Update(c.Request.Context(), req)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

func failErr(c *gin.Context, err error) {
	var se *Error
	if errors.As(err, &se) {
		apix.Fail(c, se.Status, se.Code, se.Message)
		return
	}
	apix.FailInternal(c)
}

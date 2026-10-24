package logs

import (
	"errors"
	"net/http"
	"strconv"

	"singbox-ui/server/internal/apix"

	"github.com/gin-gonic/gin"
)

// Handler serves GET /api/logs.
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) RegisterProtected(rg *gin.RouterGroup) {
	rg.GET("/logs", h.list)
}

func (h *Handler) list(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	res, err := h.svc.List(c.Request.Context(), ListParams{
		Category: c.Query("category"),
		Keyword:  c.Query("q"),
		Level:    c.Query("level"),
		Result:   c.Query("result"),
		Limit:    limit,
		Before:   c.Query("before"),
	})
	if err != nil {
		var le *Error
		if errors.As(err, &le) {
			apix.Fail(c, le.Status, le.Code, le.Message)
			return
		}
		apix.FailInternal(c)
		return
	}
	c.JSON(http.StatusOK, res)
}

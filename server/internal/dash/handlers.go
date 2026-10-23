package dash

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Handler exposes the dashboard HTTP endpoints.
type Handler struct {
	sampler *Sampler
}

// NewHandler creates the dashboard HTTP handler.
func NewHandler(sampler *Sampler) *Handler { return &Handler{sampler: sampler} }

// RegisterProtected registers dashboard endpoints behind the auth guard.
func (h *Handler) RegisterProtected(rg *gin.RouterGroup) {
	rg.GET("/dashboard/summary", h.summary)
	rg.GET("/dashboard/trend", h.trend)
}

func (h *Handler) summary(c *gin.Context) {
	c.JSON(http.StatusOK, h.sampler.Summary())
}

func (h *Handler) trend(c *gin.Context) {
	c.JSON(http.StatusOK, h.sampler.Trend(c.Query("granularity")))
}

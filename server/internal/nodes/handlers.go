package nodes

import (
	"errors"
	"net/http"
	"strconv"

	"singbox-ui/server/internal/apix"

	"github.com/gin-gonic/gin"
)

// Handler serves the node management API.
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) RegisterProtected(rg *gin.RouterGroup) {
	rg.GET("/nodes", h.list)
	rg.POST("/nodes", h.create)
	rg.GET("/nodes/:id", h.get)
	rg.PATCH("/nodes/:id", h.update)
	rg.DELETE("/nodes/:id", h.remove)
	rg.POST("/nodes/:id/push", h.pushNow)
	rg.POST("/nodes/:id/clear-block", h.clearBlock)
	rg.GET("/nodes/:id/status", h.probeStatus)
	rg.GET("/inbounds/:id/nodes", h.getInboundNodes)
	rg.PATCH("/inbounds/:id/nodes", h.setInboundNodes)
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
		apix.Fail(c, http.StatusBadRequest, "invalid_request", "request body must be JSON with name, address, token")
		return
	}
	v, err := h.svc.Create(c.Request.Context(), req)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, v)
}

func (h *Handler) get(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	v, nodeIDs, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"node": v, "inboundIds": nodeIDs})
}

func (h *Handler) update(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req UpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apix.Fail(c, http.StatusBadRequest, "invalid_request", "request body must be JSON")
		return
	}
	v, err := h.svc.Update(c.Request.Context(), id, req)
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
	warning, err := h.svc.Delete(c.Request.Context(), id)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "warning": warning})
}

func (h *Handler) pushNow(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	started, err := h.svc.PushNow(c.Request.Context(), id)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"started": started, "busy": !started})
}

func (h *Handler) clearBlock(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	cleared, started, err := h.svc.ClearBlock(c.Request.Context(), id)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"cleared": cleared, "pushStarted": started})
}

func (h *Handler) probeStatus(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	st, v, err := h.svc.ProbeStatus(c.Request.Context(), id)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": st, "node": v})
}

func (h *Handler) getInboundNodes(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	ids, err := h.svc.GetInboundNodes(c.Request.Context(), id)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"nodeIds": ids})
}

func (h *Handler) setInboundNodes(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req struct {
		NodeIDs []int64 `json:"nodeIds"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		apix.Fail(c, http.StatusBadRequest, "invalid_request", "request body must be JSON with nodeIds")
		return
	}
	ids, err := h.svc.SetInboundNodes(c.Request.Context(), id, req.NodeIDs)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"nodeIds": ids})
}

func pathID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		apix.Fail(c, http.StatusBadRequest, "invalid_request", "invalid id")
		return 0, false
	}
	return id, true
}

func failErr(c *gin.Context, err error) {
	var ne *Error
	if errors.As(err, &ne) {
		apix.Fail(c, ne.Status, ne.Code, ne.Message)
		return
	}
	apix.FailInternal(c)
}

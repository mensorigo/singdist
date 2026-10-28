package nodes

import (
	"errors"
	"net/http"
	"strconv"

	"singbox-ui/server/internal/apix"
	"singbox-ui/server/internal/store"

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
	rg.GET("/nodes/:id/check-port", h.checkNodePort)
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

// inboundNodeAssignmentJSON is one assignment in API shape. Port 0
// means "use the inbound's own port on that node".
type inboundNodeAssignmentJSON struct {
	NodeID int64 `json:"nodeId"`
	Port   int   `json:"port"`
}

func toAssignmentJSON(nodes []store.InboundAssignment) ([]inboundNodeAssignmentJSON, []int64) {
	out := []inboundNodeAssignmentJSON{}
	ids := []int64{}
	for _, n := range nodes {
		out = append(out, inboundNodeAssignmentJSON{NodeID: n.NodeID, Port: n.Port})
		ids = append(ids, n.NodeID)
	}
	return out, ids
}

func (h *Handler) getInboundNodes(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	nodes, err := h.svc.GetInboundNodeAssignments(c.Request.Context(), id)
	if err != nil {
		failErr(c, err)
		return
	}
	assignments, ids := toAssignmentJSON(nodes)
	c.JSON(http.StatusOK, gin.H{"nodeIds": ids, "nodes": assignments})
}

func (h *Handler) setInboundNodes(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req struct {
		NodeIDs []int64 `json:"nodeIds"`
		Nodes   []struct {
			NodeID int64 `json:"nodeId"`
			Port   int   `json:"port"`
		} `json:"nodes"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		apix.Fail(c, http.StatusBadRequest, "invalid_request", "request body must be JSON with nodeIds or nodes")
		return
	}
	// New shape wins; the legacy nodeIds shape (old frontend) maps to
	// port 0 = the inbound's own port.
	assignments := []store.InboundAssignment{}
	if len(req.Nodes) > 0 {
		for _, n := range req.Nodes {
			assignments = append(assignments, store.InboundAssignment{NodeID: n.NodeID, Port: n.Port})
		}
	} else {
		for _, nid := range req.NodeIDs {
			assignments = append(assignments, store.InboundAssignment{NodeID: nid, Port: 0})
		}
	}
	saved, err := h.svc.SetInboundNodes(c.Request.Context(), id, assignments)
	if err != nil {
		failErr(c, err)
		return
	}
	out, ids := toAssignmentJSON(saved)
	c.JSON(http.StatusOK, gin.H{"nodeIds": ids, "nodes": out})
}

func (h *Handler) checkNodePort(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	port, err := strconv.Atoi(c.Query("port"))
	if err != nil || port < 1 || port > 65535 {
		apix.Fail(c, http.StatusBadRequest, "invalid_request", "invalid port")
		return
	}
	chk, err := h.svc.CheckNodePort(c.Request.Context(), id, port, c.Query("transport"))
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, chk)
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

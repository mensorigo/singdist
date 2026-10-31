package nodes

import (
	"errors"
	"fmt"
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
	rg.POST("/nodes/request-pairing-code", h.requestPairingCode)
	rg.PATCH("/nodes/:id", h.update)
	rg.DELETE("/nodes/:id", h.remove)
	rg.POST("/nodes/:id/push", h.pushNow)
	rg.POST("/nodes/:id/clear-block", h.clearBlock)
	rg.GET("/nodes/:id/status", h.probeStatus)
	rg.GET("/nodes/:id/import-scan", h.scanImport)
	rg.POST("/nodes/:id/import", h.importInbounds)
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

// requestPairingCode asks the agent to generate a pairing code.
// The response carries only ok/retry_after: the code itself goes
// to the agent log and is never returned here.
func (h *Handler) requestPairingCode(c *gin.Context) {
	var req struct {
		Address string `json:"address"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		apix.Fail(c, http.StatusBadRequest, "invalid_request", "request body must be JSON with address")
		return
	}
	ok, retry, err := h.svc.RequestPairingCode(c.Request.Context(), req.Address)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": ok, "retry_after": retry})
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
	// A pause toggle gets a readable audit summary; other edits keep
	// the default (no summary). The middleware only uses this on
	// success, so a failed toggle leaves no stale summary behind.
	if req.Paused != nil {
		if *req.Paused {
			c.Set("auditSummary", "暂停节点")
		} else {
			c.Set("auditSummary", "启用节点")
		}
	}
	c.JSON(http.StatusOK, v)
}

func (h *Handler) remove(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	res, err := h.svc.Delete(c.Request.Context(), id)
	if err != nil {
		failErr(c, err)
		return
	}
	// Panel-only delete: nothing was pushed and nothing on the node
	// was stopped, so there is no warning. The summary names what
	// the panel actually removed (assignments + single-machine
	// inbounds); the audit middleware never sees request bodies, so
	// no token can leak through it.
	summary := fmt.Sprintf("分派 %d 个", res.Assignments)
	if len(res.DeletedInbounds) > 0 {
		summary += "，单机入站删除：" + joinNames(res.DeletedInbounds)
	}
	c.Set("auditSummary", summary)
	c.JSON(http.StatusOK, gin.H{"ok": true, "warning": ""})
}

func joinNames(names []string) string {
	out := ""
	for i, n := range names {
		if i > 0 {
			out += "、"
		}
		out += n
	}
	return out
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

func (h *Handler) scanImport(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	res, err := h.svc.ScanImport(c.Request.Context(), id)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

func (h *Handler) importInbounds(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req struct {
		Names []string `json:"names"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		apix.Fail(c, http.StatusBadRequest, "invalid_request", "request body must be JSON with names")
		return
	}
	res, err := h.svc.ImportInbounds(c.Request.Context(), id, req.Names)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
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

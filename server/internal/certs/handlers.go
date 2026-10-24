package certs

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"singbox-ui/server/internal/apix"
)

// Handler exposes the certificate management HTTP endpoints.
type Handler struct {
	mgr *Manager
}

// NewHandler creates the certificate HTTP handler.
func NewHandler(mgr *Manager) *Handler { return &Handler{mgr: mgr} }

// RegisterProtected registers certificate endpoints behind the auth guard.
func (h *Handler) RegisterProtected(rg *gin.RouterGroup) {
	rg.GET("/certs", h.list)
	rg.POST("/certs", h.upload)
	rg.POST("/certs/selfsigned", h.selfSigned)
	rg.POST("/certs/:id/activate", h.activate)
	rg.PATCH("/certs/:id/note", h.updateNote)
	rg.DELETE("/certs/:id", h.remove)
}

func (h *Handler) list(c *gin.Context) {
	versions, err := h.mgr.List()
	if err != nil {
		apix.FailInternal(c)
		return
	}
	active, ok := h.mgr.Active()
	var activeVal any
	if ok {
		activeVal = active
	}
	c.JSON(http.StatusOK, gin.H{"active": activeVal, "versions": versions})
}

type uploadRequest struct {
	CertPem  string `json:"certPem"`
	KeyPem   string `json:"keyPem"`
	Note     string `json:"note"`
	Activate bool   `json:"activate"`
}

func (h *Handler) upload(c *gin.Context) {
	var req uploadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apix.Fail(c, http.StatusBadRequest, "invalid_request", "request body must be JSON with certPem and keyPem")
		return
	}
	if req.CertPem == "" || req.KeyPem == "" {
		apix.Fail(c, http.StatusBadRequest, "validation", "certPem and keyPem are required")
		return
	}
	info, err := h.mgr.Add([]byte(req.CertPem), []byte(req.KeyPem), req.Note, req.Activate)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, info)
}

type selfSignedRequest struct {
	Names    []string `json:"names"`
	Days     int      `json:"days"`
	Note     string   `json:"note"`
	Activate bool     `json:"activate"`
}

func (h *Handler) selfSigned(c *gin.Context) {
	var req selfSignedRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apix.Fail(c, http.StatusBadRequest, "invalid_request", "request body must be JSON with names")
		return
	}
	info, err := h.mgr.GenerateSelfSigned(req.Names, req.Days, req.Note, req.Activate)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, info)
}

func (h *Handler) activate(c *gin.Context) {
	info, err := h.mgr.Activate(c.Param("id"))
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, info)
}

type noteRequest struct {
	Note string `json:"note"`
}

func (h *Handler) updateNote(c *gin.Context) {
	var req noteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apix.Fail(c, http.StatusBadRequest, "invalid_request", "request body must be JSON with note")
		return
	}
	info, err := h.mgr.UpdateNote(c.Param("id"), req.Note)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, info)
}

func (h *Handler) remove(c *gin.Context) {
	if err := h.mgr.Delete(c.Param("id")); err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// failErr maps typed certs errors to their declared status/code and hides
// anything unexpected behind the unified 500.
func failErr(c *gin.Context, err error) {
	var ce *Error
	if errors.As(err, &ce) {
		apix.Fail(c, ce.Status, ce.Code, ce.Message)
		return
	}
	apix.FailInternal(c)
}

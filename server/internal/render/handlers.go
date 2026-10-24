package render

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"singbox-ui/server/internal/apix"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// RegisterPublic mounts the unauthenticated subscription endpoint;
// the token in the path is the credential.
func (h *Handler) RegisterPublic(rg *gin.RouterGroup) {
	rg.GET("/sub/:token", h.serveSub)
}

// RegisterProtected mounts the admin preview under /api.
func (h *Handler) RegisterProtected(rg *gin.RouterGroup) {
	rg.GET("/subscriptions/:id/preview", h.preview)
}

func (h *Handler) serveSub(c *gin.Context) {
	res, err := h.svc.RenderByToken(c.Request.Context(), c.Param("token"), c.Query("format"), c.Request.Host)
	if err != nil {
		var se *Error
		if errors.As(err, &se) {
			c.String(se.Status, se.Message)
			return
		}
		c.String(http.StatusInternalServerError, "render failed")
		return
	}
	writeSubscription(c, res)
}

func (h *Handler) preview(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		apix.Fail(c, http.StatusBadRequest, "invalid_request", "invalid id")
		return
	}
	res, err := h.svc.RenderByID(c.Request.Context(), id, c.Query("format"), c.Request.Host)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"format":      res.Format,
		"contentType": res.ContentType,
		"content":     res.Body,
	})
}

// writeSubscription sets the document body plus the headers clients
// rely on: profile title, update interval and the userinfo traffic
// summary.
func writeSubscription(c *gin.Context, res Result) {
	h := c.Writer.Header()
	h.Set("Content-Type", res.ContentType)
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Disposition", contentDisposition(res.SubName))
	h.Set("Profile-Title", profileTitle(res.SubName))
	if res.UpdateHrs > 0 {
		h.Set("Profile-Update-Interval", strconv.Itoa(res.UpdateHrs))
	}
	if res.ShowInfo {
		parts := []string{
			fmt.Sprintf("upload=%d", res.UsedUp),
			fmt.Sprintf("download=%d", res.UsedDown),
		}
		if res.LimitBytes > 0 {
			parts = append(parts, fmt.Sprintf("total=%d", res.LimitBytes))
		}
		if res.ExpireAt > 0 {
			parts = append(parts, fmt.Sprintf("expire=%d", res.ExpireAt))
		}
		h.Set("Subscription-Userinfo", strings.Join(parts, "; "))
	}
	c.String(http.StatusOK, res.Body)
}

func contentDisposition(name string) string {
	if isASCII(name) {
		return "attachment; filename=" + name
	}
	return "attachment; filename*=UTF-8''" + url.PathEscape(name)
}

func profileTitle(name string) string {
	if isASCII(name) {
		return name
	}
	return "base64:" + base64.StdEncoding.EncodeToString([]byte(name))
}

func isASCII(s string) bool {
	for _, r := range s {
		if r > 127 {
			return false
		}
	}
	return true
}

func failErr(c *gin.Context, err error) {
	var se *Error
	if errors.As(err, &se) {
		apix.Fail(c, se.Status, se.Code, se.Message)
		return
	}
	apix.FailInternal(c)
}

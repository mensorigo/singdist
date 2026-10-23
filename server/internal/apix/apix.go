// Package apix holds the small set of web primitives shared by every HTTP
// handler in the server: the unified error body shape and context keys.
// It contains no business logic and imports no other internal package.
package apix

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Context keys set by the auth guard middleware.
const (
	CtxAdminID   = "apix.admin_id"
	CtxUsername  = "apix.username"
	CtxSessionID = "apix.session_hash"
)

// ErrorBody is the single error shape returned by every API endpoint.
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Fail writes the unified error body and aborts the handler chain.
func Fail(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, ErrorBody{Code: code, Message: message})
}

// FailInternal writes a 500 with a generic message; details belong in logs.
func FailInternal(c *gin.Context) {
	Fail(c, http.StatusInternalServerError, "internal_error", "internal server error")
}

// AdminID returns the authenticated admin id placed by the auth guard.
func AdminID(c *gin.Context) (int64, bool) {
	v, ok := c.Get(CtxAdminID)
	if !ok {
		return 0, false
	}
	id, ok := v.(int64)
	return id, ok
}

// Username returns the authenticated admin username placed by the guard.
func Username(c *gin.Context) string {
	if v, ok := c.Get(CtxUsername); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

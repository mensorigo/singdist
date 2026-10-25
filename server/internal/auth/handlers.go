package auth

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"singbox-ui/server/internal/apix"
)

// Handler exposes the auth HTTP endpoints. Registration is split into a
// public group (login/status) and a protected group (logout/password) so the
// wiring in main stays explicit about what sits behind the auth guard.
type Handler struct {
	svc *Service
}

// NewHandler creates the auth HTTP handler.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// RegisterPublic registers endpoints reachable without a session.
func (h *Handler) RegisterPublic(rg *gin.RouterGroup) {
	rg.POST("/auth/login", h.login)
	rg.GET("/auth/status", h.status)
}

// RegisterProtected registers endpoints that require the auth guard.
func (h *Handler) RegisterProtected(rg *gin.RouterGroup) {
	rg.POST("/auth/logout", h.logout)
	rg.PATCH("/auth/password", h.changePassword)
	rg.PATCH("/auth/username", h.changeUsername)
	rg.PATCH("/auth/account", h.changeAccount)
	rg.POST("/auth/sessions/revoke-others", h.revokeOtherSessions)
}

type loginRequest struct {
	Username string `json:"username" binding:"required,min=1,max=64"`
	Password string `json:"password" binding:"required,min=1,max=256"`
}

func (h *Handler) login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apix.Fail(c, http.StatusBadRequest, "invalid_request", "username and password are required")
		return
	}
	// Only the username, never the password: the audit middleware
	// shows the attempted username on the login row (success or
	// failure) instead of a blank target.
	c.Set("auditUsername", req.Username)
	token, expires, err := h.svc.Login(c.Request.Context(), req.Username, req.Password, c.ClientIP())
	switch {
	case errors.Is(err, ErrRateLimited):
		apix.Fail(c, http.StatusTooManyRequests, "rate_limited", "too many failed attempts, try again later")
		return
	case errors.Is(err, ErrInvalidCredentials):
		apix.Fail(c, http.StatusUnauthorized, "invalid_credentials", "invalid username or password")
		return
	case err != nil:
		apix.FailInternal(c)
		return
	}
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(CookieName, token, int(SessionTTL.Seconds()), "/", "", true, true)
	c.JSON(http.StatusOK, gin.H{
		"username":  req.Username,
		"expiresAt": expires.Unix(),
	})
}

func (h *Handler) status(c *gin.Context) {
	token, err := c.Cookie(CookieName)
	if err != nil || token == "" {
		c.JSON(http.StatusOK, gin.H{"authenticated": false})
		return
	}
	_, username, err := h.svc.ValidateSession(c.Request.Context(), token)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"authenticated": false})
		return
	}
	c.JSON(http.StatusOK, gin.H{"authenticated": true, "username": username})
}

func (h *Handler) logout(c *gin.Context) {
	if token, err := c.Cookie(CookieName); err == nil {
		_ = h.svc.Logout(c.Request.Context(), token)
	}
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(CookieName, "", -1, "/", "", true, true)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

type changePasswordRequest struct {
	CurrentPassword string `json:"currentPassword" binding:"required"`
	NewPassword     string `json:"newPassword" binding:"required,min=8,max=72"`
}

func (h *Handler) changePassword(c *gin.Context) {
	adminID, ok := apix.AdminID(c)
	if !ok {
		apix.Fail(c, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req changePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apix.Fail(c, http.StatusBadRequest, "invalid_request", "currentPassword and a newPassword of 8–72 characters are required")
		return
	}
	err := h.svc.ChangePassword(c.Request.Context(), adminID, req.CurrentPassword, req.NewPassword)
	switch {
	case errors.Is(err, ErrWrongPassword):
		apix.Fail(c, http.StatusBadRequest, "wrong_password", "current password is incorrect")
		return
	case err != nil:
		apix.FailInternal(c)
		return
	}
	// A password change is a security event: every other session stops
	// working. The current session survives (best effort — a revoke
	// failure must not fail a password change that already landed).
	if token, err := c.Cookie(CookieName); err == nil && token != "" {
		_, _ = h.svc.RevokeOtherSessions(c.Request.Context(), adminID, SessionHash(token))
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

type changeUsernameRequest struct {
	Username        string `json:"username" binding:"required"`
	CurrentPassword string `json:"currentPassword" binding:"required"`
}

func (h *Handler) changeUsername(c *gin.Context) {
	adminID, ok := apix.AdminID(c)
	if !ok {
		apix.Fail(c, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req changeUsernameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apix.Fail(c, http.StatusBadRequest, "invalid_request", "username and currentPassword are required")
		return
	}
	username, err := h.svc.ChangeUsername(c.Request.Context(), adminID, req.CurrentPassword, req.Username)
	switch {
	case errors.Is(err, ErrWrongPassword):
		apix.Fail(c, http.StatusBadRequest, "wrong_password", "current password is incorrect")
		return
	case errors.Is(err, ErrInvalidUsername):
		apix.Fail(c, http.StatusBadRequest, "invalid_request", "username must be 1–64 characters without whitespace")
		return
	case errors.Is(err, ErrUsernameTaken):
		apix.Fail(c, http.StatusConflict, "username_taken", "username is already taken")
		return
	case err != nil:
		apix.FailInternal(c)
		return
	}
	c.JSON(http.StatusOK, gin.H{"username": username})
}

type changeAccountRequest struct {
	Username        *string `json:"username"`
	CurrentPassword string  `json:"currentPassword" binding:"required"`
	NewPassword     *string `json:"newPassword" binding:"omitempty,min=8,max=72"`
}

func (h *Handler) changeAccount(c *gin.Context) {
	adminID, ok := apix.AdminID(c)
	if !ok {
		apix.Fail(c, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req changeAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apix.Fail(c, http.StatusBadRequest, "invalid_request", "currentPassword is required, username must be 1–64 characters without whitespace, newPassword 8–72 characters")
		return
	}
	username, err := h.svc.ChangeAccount(c.Request.Context(), adminID, req.CurrentPassword, req.Username, req.NewPassword)
	switch {
	case errors.Is(err, ErrWrongPassword):
		apix.Fail(c, http.StatusBadRequest, "wrong_password", "current password is incorrect")
		return
	case errors.Is(err, ErrInvalidUsername):
		apix.Fail(c, http.StatusBadRequest, "invalid_request", "username must be 1–64 characters without whitespace")
		return
	case errors.Is(err, ErrUsernameTaken):
		apix.Fail(c, http.StatusConflict, "username_taken", "username is already taken")
		return
	case errors.Is(err, ErrInvalidPassword):
		apix.Fail(c, http.StatusBadRequest, "invalid_request", "newPassword must be 8–72 characters")
		return
	case err != nil:
		apix.FailInternal(c)
		return
	}
	// A password change inside the combined update revokes other
	// sessions, exactly like the dedicated password endpoint.
	if req.NewPassword != nil && *req.NewPassword != "" {
		if token, err := c.Cookie(CookieName); err == nil && token != "" {
			_, _ = h.svc.RevokeOtherSessions(c.Request.Context(), adminID, SessionHash(token))
		}
	}
	c.JSON(http.StatusOK, gin.H{"username": username})
}

func (h *Handler) revokeOtherSessions(c *gin.Context) {
	adminID, ok := apix.AdminID(c)
	if !ok {
		apix.Fail(c, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	except := ""
	if token, err := c.Cookie(CookieName); err == nil && token != "" {
		except = SessionHash(token)
	}
	n, err := h.svc.RevokeOtherSessions(c.Request.Context(), adminID, except)
	if err != nil {
		apix.FailInternal(c)
		return
	}
	c.JSON(http.StatusOK, gin.H{"revoked": n})
}

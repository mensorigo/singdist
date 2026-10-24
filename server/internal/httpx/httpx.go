// Package httpx owns HTTP plumbing: the Gin engine, middleware (slog
// request logging, panic recovery producing the unified error shape, the
// session auth guard), static serving of the embedded frontend, and the
// HTTPS listener (with first-run self-signed certificate generation so the
// panel is never served over plaintext).
//
// It knows nothing about business modules: handlers register themselves on
// the router groups main hands them, and the guard validates sessions
// through the TokenValidator interface.
package httpx

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"singbox-ui/server/internal/apix"
)

// TokenValidator resolves a raw session cookie token to an admin identity.
// internal/auth.Service satisfies it.
type TokenValidator interface {
	ValidateSession(ctx context.Context, token string) (adminID int64, username string, err error)
}

// NewEngine builds the Gin engine with logging + recovery middleware.
func NewEngine(log *slog.Logger) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	engine.Use(requestLogger(log), recovery(log))
	return engine
}

func requestLogger(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		log.Info("http request",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"latency_ms", time.Since(start).Milliseconds(),
			"ip", c.ClientIP(),
		)
	}
}

func recovery(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				log.Error("http panic recovered", "panic", r, "path", c.Request.URL.Path)
				if !c.Writer.Written() {
					apix.FailInternal(c)
				}
			}
		}()
		c.Next()
	}
}

// AuthGuard returns middleware that requires a valid session cookie. The
// cookie name is supplied by the caller (owned by the auth module) so this
// package stays free of auth internals.
func AuthGuard(validator TokenValidator, cookieName string) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := c.Cookie(cookieName)
		if err != nil || token == "" {
			apix.Fail(c, http.StatusUnauthorized, "unauthorized", "authentication required")
			return
		}
		adminID, username, err := validator.ValidateSession(c.Request.Context(), token)
		if err != nil {
			apix.Fail(c, http.StatusUnauthorized, "unauthorized", "session expired or invalid")
			return
		}
		c.Set(apix.CtxAdminID, adminID)
		c.Set(apix.CtxUsername, username)
		c.Next()
	}
}

// Serve runs the HTTPS server until ctx is cancelled, then shuts down
// gracefully. Which certificate is presented is delegated to getCert (the
// certs module's GetCertificate), so the panel can hot-swap certificates
// without a restart. Callers must have run EnsurePairFiles beforehand so a
// bootstrap pair is guaranteed to exist. tlsCfg is accepted for call-site
// symmetry with that guarantee step; serving itself uses only getCert.
func Serve(ctx context.Context, log *slog.Logger, addr string, handler http.Handler, getCert func(*tls.ClientHelloInfo) (*tls.Certificate, error), tlsCfg TLSConfig) error {
	_ = tlsCfg
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		TLSConfig: &tls.Config{
			GetCertificate: getCert,
			MinVersion:     tls.VersionTLS12,
		},
	}
	errCh := make(chan error, 1)
	go func() {
		log.Info("https server listening", "addr", addr)
		errCh <- srv.ListenAndServeTLS("", "")
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

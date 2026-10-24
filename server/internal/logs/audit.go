package logs

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"singbox-ui/server/internal/store"

	"github.com/gin-gonic/gin"
)

// actionLabels maps mutating routes to the operation name shown in the
// Logs page. Unmapped mutations fall back to "METHOD /path" so no
// operation ever escapes the audit.
var actionLabels = map[string]string{
	"POST /api/auth/login":                    "登录",
	"POST /api/auth/logout":                   "退出登录",
	"PATCH /api/auth/password":                "修改密码",
	"PATCH /api/auth/username":                "修改用户名",
	"PATCH /api/auth/account":                 "修改账号",
	"POST /api/auth/sessions/revoke-others":   "撤销其他会话",
	"POST /api/inbounds":                      "创建入站",
	"PATCH /api/inbounds/:id":                 "修改入站",
	"PATCH /api/inbounds/:id/enabled":         "启停入站",
	"DELETE /api/inbounds/:id":                "删除入站",
	"POST /api/inbounds/reality-keypair":      "生成 Reality 密钥对",
	"POST /api/subscriptions":                 "创建订阅",
	"PATCH /api/subscriptions/:id":            "修改订阅",
	"PATCH /api/subscriptions/:id/enabled":    "启停订阅",
	"POST /api/subscriptions/:id/reset-token": "转换链接",
	"POST /api/subscriptions/:id/reset-uuid":  "转换UUID",
	"DELETE /api/subscriptions/:id":           "删除订阅",
	"POST /api/templates":                     "创建模板",
	"PATCH /api/templates/:id":                "修改模板",
	"DELETE /api/templates/:id":               "删除模板",
	"POST /api/certs":                         "上传证书",
	"POST /api/certs/selfsigned":              "生成自签名证书",
	"POST /api/certs/:id/activate":            "启用证书",
	"PATCH /api/certs/:id/note":               "修改证书备注",
	"DELETE /api/certs/:id":                   "删除证书",
	"PATCH /api/settings":                     "修改设置",
	"POST /api/sync":                          "同步到核心",
}

// captureWriter tees the response body (capped) so the middleware can
// read the created object's name and the error code without touching
// request bodies — passwords and tokens never come near the audit.
type captureWriter struct {
	gin.ResponseWriter
	buf     bytes.Buffer
	capped  bool
}

func (w *captureWriter) Write(b []byte) (int, error) {
	if !w.capped {
		if w.buf.Len()+len(b) > 64*1024 {
			w.capped = true
		} else {
			w.buf.Write(b)
		}
	}
	return w.ResponseWriter.Write(b)
}

// AuditMiddleware records every mutating /api call and its outcome.
// Reads are never logged: dashboard polling alone would flood the
// table. Recording is best-effort and never fails the request itself.
// When fileDir is non-empty, each row is also appended to
// fileDir/audit.log (a single file) so panel operations survive as a
// plain-text file, not only as database rows.
func AuditMiddleware(st *store.Store, log *slog.Logger, fileDir string) gin.HandlerFunc {
	var fw *auditFileWriter
	if fileDir != "" {
		fw = newAuditFileWriter(fileDir, LocationFromStore(st))
	}
	return func(c *gin.Context) {
		method := c.Request.Method
		if method != http.MethodPost && method != http.MethodPatch &&
			method != http.MethodPut && method != http.MethodDelete {
			c.Next()
			return
		}
		if !strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.Next()
			return
		}
		cw := &captureWriter{ResponseWriter: c.Writer}
		c.Writer = cw
		c.Next()

		status := c.Writer.Status()
		key := method + " " + c.FullPath()
		action, ok := actionLabels[key]
		if !ok {
			// Unmatched routes have an empty FullPath; fall back to
			// the request path so the entry stays readable.
			fp := c.FullPath()
			if fp == "" {
				fp = c.Request.URL.Path
			}
			action = method + " " + fp
		}
		var body struct {
			Name string `json:"name"`
			Code string `json:"code"`
		}
		_ = json.Unmarshal(cw.buf.Bytes(), &body)
		target := body.Name
		if target == "" {
			target = c.Param("id")
		}
		result, detail := "success", ""
		if status >= 400 {
			result, detail = "failed", body.Code
		}
		row := store.AuditRow{
			At:     time.Now().Unix(),
			Action: action,
			Target: target,
			Result: result,
			Detail: detail,
		}
		if err := st.AddAuditLog(c.Request.Context(), row); err != nil {
			log.Warn("logs: audit write failed", "err", err)
		}
		if fw != nil {
			if err := fw.Write(c.Request.Context(), row); err != nil {
				log.Warn("logs: audit file write failed", "err", err)
			}
		}
	}
}

package logs

import (
	"bytes"
	"context"
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
	"POST /api/subscriptions/:id/reset-token": "更换链接",
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
	"GET /api/subscriptions/:id/preview":      "预览订阅",
}

// coreAffectingRoutes are the mutating routes whose edits change (or may
// change) the desired core config, so their audit rows start pending
// until a sync covers them. Auth routes, reset-token (panel-side only),
// the preview read and the sync call itself are not in this set: the
// sync row carries its own applied hash instead. Cert note/upload/
// self-signed/delete never reach the desired config — only activation
// does — so only activation is listed.
var coreAffectingRoutes = map[string]bool{
	"POST /api/inbounds":                     true,
	"PATCH /api/inbounds/:id":                true,
	"PATCH /api/inbounds/:id/enabled":        true,
	"DELETE /api/inbounds/:id":               true,
	"POST /api/subscriptions":                true,
	"PATCH /api/subscriptions/:id":           true,
	"PATCH /api/subscriptions/:id/enabled":   true,
	"POST /api/subscriptions/:id/reset-uuid": true,
	"DELETE /api/subscriptions/:id":          true,
	"POST /api/templates":                    true,
	"PATCH /api/templates/:id":               true,
	"DELETE /api/templates/:id":              true,
	"POST /api/certs/:id/activate":           true,
	"PATCH /api/settings":                    true,
}

func strptr(s string) *string { return &s }

// captureWriter tees the response body (capped) so the middleware can
// read the created object's name and the error code without touching
// request bodies — passwords and tokens never come near the audit.
type captureWriter struct {
	gin.ResponseWriter
	buf    bytes.Buffer
	capped bool
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

// AuditMiddleware records every mutating /api call and its outcome,
// plus the subscription preview — the one read that renders a
// credential. Other reads are never logged: dashboard polling alone
// would flood the table. Recording is best-effort and never fails the
// request itself. When fileDir is non-empty, each row is also appended
// to fileDir/audit.log (a single file) so panel operations survive as
// a plain-text file, not only as database rows.
func AuditMiddleware(st *store.Store, log *slog.Logger, fileDir string) gin.HandlerFunc {
	return func(c *gin.Context) {
		method := c.Request.Method
		key := method + " " + c.FullPath()
		isPreview := key == "GET /api/subscriptions/:id/preview"
		if !isPreview && method != http.MethodPost && method != http.MethodPatch &&
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
			Name   string `json:"name"`
			Code   string `json:"code"`
			Result string `json:"result"`
			Hash   string `json:"hash"`
			Detail string `json:"detail"`
		}
		_ = json.Unmarshal(cw.buf.Bytes(), &body)
		target := body.Name
		if target == "" {
			target = c.Param("id")
		}
		// Login rows carry the attempted username: the login response
		// has no name field, so without this the row would show no
		// target at all. Only the username is ever read, never the
		// password (the handler sets just the username).
		if key == "POST /api/auth/login" {
			if u := c.GetString("auditUsername"); u != "" {
				target = u
			}
		}
		result, detail := "success", ""
		if key == "POST /api/sync" {
			// The sync endpoint always answers 200 with the sync row,
			// so the HTTP status cannot tell success from failure —
			// the row's own result is the truth. The frontend reads
			// it the same way, which is why the status stays 200.
			switch body.Result {
			case "applied", "no_change":
				result = "success"
			default:
				result = "failed"
				detail = body.Detail
				if detail == "" {
					detail = body.Result
				}
			}
			if body.Hash != "" {
				target = body.Hash
				if len(target) > 8 {
					target = target[:8]
				}
			}
		} else if status >= 400 {
			result, detail = "failed", body.Code
		}
		// Sync rows need no coverage state: their target already is
		// the result hash. Only core-affecting edits start pending.
		var appliedHash *string
		if key != "POST /api/sync" && coreAffectingRoutes[key] {
			appliedHash = strptr("")
		}
		// A change summary is only meaningful when the change landed;
		// on failure nothing changed, so a stale summary must not stick.
		summary := ""
		if result == "success" {
			if v, ok := c.Get("auditSummary"); ok {
				summary, _ = v.(string)
			}
		}
		row := store.AuditRow{
			At:     time.Now().Unix(),
			Action: action,
			Target: target,
			Result: result,
			Detail: detail,
			// RemoteIP, not ClientIP: the panel is directly exposed, so
			// the TCP peer is the trustworthy source; a forwarded
			// header would be client-spoofable here.
			IP:          c.RemoteIP(),
			AppliedHash: appliedHash,
			Summary:     summary,
		}
		RecordAudit(c.Request.Context(), st, log, fileDir, row)
	}
}

// RecordAudit writes one audit row to the database and, when fileDir is
// non-empty, appends it to the audit file. Best-effort: a failure is
// logged and never fails the caller. System-triggered rows (the
// auto-sync ticker) use this directly, bypassing the HTTP middleware.
func RecordAudit(ctx context.Context, st *store.Store, log *slog.Logger, fileDir string, row store.AuditRow) {
	if err := st.AddAuditLog(ctx, row); err != nil {
		log.Warn("logs: audit write failed", "err", err)
	}
	if fileDir != "" {
		fw := newAuditFileWriter(fileDir, LocationFromStore(st))
		if err := fw.Write(ctx, row); err != nil {
			log.Warn("logs: audit file write failed", "err", err)
		}
	}
}

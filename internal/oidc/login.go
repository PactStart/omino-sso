package oidc

import (
	"context"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/omino/omino-sso/internal/config"
	"github.com/omino/omino-sso/internal/pkg/clientip"
	"github.com/omino/omino-sso/internal/pkg/cookie"
	"github.com/omino/omino-sso/internal/service"
	"github.com/omino/omino-sso/internal/store/model"
)

const sessionCookie = cookie.Name

var loginTmpl = template.Must(template.New("login").Parse(`<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><title>统一登录</title>
<style>
body{font-family:-apple-system,"PingFang SC",sans-serif;background:#f5f6f8;display:flex;align-items:center;justify-content:center;height:100vh;margin:0}
.card{background:#fff;padding:40px;border-radius:12px;box-shadow:0 8px 30px rgba(0,0,0,.08);width:340px}
h1{font-size:20px;margin:0 0 6px}p.sub{color:#888;font-size:13px;margin:0 0 24px}
label{display:block;font-size:13px;color:#555;margin:14px 0 6px}
input{width:100%;box-sizing:border-box;padding:10px 12px;border:1px solid #dcdfe6;border-radius:6px;font-size:14px}
button{width:100%;margin-top:22px;padding:11px;border:0;border-radius:6px;background:#2563eb;color:#fff;font-size:15px;cursor:pointer}
.err{background:#fef2f2;color:#b91c1c;padding:9px 12px;border-radius:6px;font-size:13px;margin-bottom:8px}
</style></head><body>
<form class="card" method="post" action="/login">
  <h1>统一身份认证</h1>
  <p class="sub">登录后返回 {{.ClientName}}</p>
  {{if .Error}}<div class="err">{{.Error}}</div>{{end}}
  <input type="hidden" name="id" value="{{.ID}}">
  {{if .MFATicket}}
  <input type="hidden" name="ticket" value="{{.MFATicket}}">
  <label>动态验证码</label>
  <input name="code" autocomplete="one-time-code" maxlength="6" autofocus>
  {{else}}
  <label>用户名</label>
  <input name="username" value="{{.Username}}" autocomplete="username" autofocus>
  <label>密码</label>
  <input name="password" type="password" autocomplete="current-password">
  {{end}}
  <button type="submit">{{if .MFATicket}}验证{{else}}登录{{end}}</button>
</form></body></html>`))

type loginData struct {
	ID         string
	ClientName string
	Username   string
	MFATicket  string
	Error      string
}

type loginHandler struct {
	cfg      *config.Config
	st       *Storage
	users    *service.UserService
	sessions *service.SessionService
	clients  *service.ClientService
	audit    *service.Auditor
	callback func(context.Context, string) string
	log      *slog.Logger
}

func newLoginHandler(
	cfg *config.Config,
	st *Storage,
	users *service.UserService,
	sessions *service.SessionService,
	clients *service.ClientService,
	audit *service.Auditor,
	callback func(context.Context, string) string,
	logger *slog.Logger,
) *loginHandler {
	return &loginHandler{
		cfg: cfg, st: st, users: users, sessions: sessions,
		clients: clients, audit: audit, callback: callback, log: logger,
	}
}

func (h *loginHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		h.post(w, r)
		return
	}
	h.get(w, r)
}

func (h *loginHandler) get(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	req, ok := h.st.PendingRequest(id)
	if !ok {
		http.Error(w, "登录请求已过期，请重新发起授权", http.StatusBadRequest)
		return
	}
	clientName := h.clientName(r.Context(), req.ClientID)

	// 已有有效会话则直接完成授权（单点登录）。
	if c, err := r.Cookie(sessionCookie); err == nil {
		if sess, err := h.sessions.Get(r.Context(), c.Value); err == nil {
			if err := h.st.CompleteLogin(r.Context(), id, sess.UserID, time.UnixMilli(sess.AuthTime), sess.AMR); err != nil {
				h.render(w, loginData{ID: id, ClientName: clientName, Error: "登录状态异常，请重试"})
				return
			}
			http.Redirect(w, r, h.callback(r.Context(), id), http.StatusFound)
			return
		}
	}
	h.render(w, loginData{ID: id, ClientName: clientName})
}

func (h *loginHandler) post(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	id := r.PostFormValue("id")
	req, ok := h.st.PendingRequest(id)
	if !ok {
		http.Error(w, "登录请求已过期，请重新发起授权", http.StatusBadRequest)
		return
	}
	clientName := h.clientName(r.Context(), req.ClientID)
	// 与 Gin 侧同一套可信代理解析：无条件信任 XFF 会让审计 IP 可被客户端伪造。
	ip := clientip.FromRequest(r, h.cfg.TrustedProxies)
	ua := r.UserAgent()

	// 第二步：MFA
	if ticket := r.PostFormValue("ticket"); ticket != "" {
		code := r.PostFormValue("code")
		uid, err := h.sessions.ConsumeMFATicket(r.Context(), ticket)
		if err != nil {
			h.render(w, loginData{ID: id, ClientName: clientName, Error: "验证码已过期，请重新登录"})
			return
		}
		u, err := h.users.GetByID(r.Context(), uid)
		if err != nil {
			h.render(w, loginData{ID: id, ClientName: clientName, Error: "用户不存在"})
			return
		}
		secret, err := h.users.DecryptMFASecret(u)
		if err != nil || !totp.Validate(code, secret) {
			h.audit.Emit(r.Context(), service.Event{
				Type: model.EventMFAFailure, UserID: u.ID, Username: u.Username,
				ClientID: req.ClientID, IP: ip, UserAgent: ua, Result: "failure",
			})
			h.render(w, loginData{ID: id, ClientName: clientName, Error: "动态验证码错误", MFATicket: ticket})
			return
		}
		h.audit.Emit(r.Context(), service.Event{
			Type: model.EventMFASuccess, UserID: u.ID, Username: u.Username,
			ClientID: req.ClientID, IP: ip, UserAgent: ua, Result: "success",
		})
		h.finish(w, r, id, u, []string{"pwd", "otp"}, ip, ua, clientName, req.ClientID)
		return
	}

	// 第一步：账号密码
	username := r.PostFormValue("username")
	password := r.PostFormValue("password")
	u, err := h.users.Authenticate(r.Context(), username, password, service.Meta{IP: ip, UserAgent: ua})
	if err != nil {
		h.render(w, loginData{ID: id, ClientName: clientName, Username: username, Error: loginErrText(err)})
		return
	}
	if u.MFAEnabled || u.MFARequired {
		ticket, err := h.sessions.CreateMFATicket(r.Context(), u.ID, 5*time.Minute)
		if err != nil {
			h.render(w, loginData{ID: id, ClientName: clientName, Error: "系统繁忙，请重试"})
			return
		}
		h.render(w, loginData{ID: id, ClientName: clientName, MFATicket: ticket})
		return
	}
	h.finish(w, r, id, u, []string{"pwd"}, ip, ua, clientName, req.ClientID)
}

func (h *loginHandler) finish(w http.ResponseWriter, r *http.Request, id string, u *model.User, amr []string, ip, ua, clientName, clientID string) {
	sess, err := h.sessions.Create(r.Context(), u, amr, false, ip, ua)
	if err != nil {
		h.render(w, loginData{ID: id, ClientName: clientName, Error: "创建会话失败"})
		return
	}
	setSessionCookie(w, h.cfg, sess.SID, h.cfg.SessionTTL)
	if err := h.st.CompleteLogin(r.Context(), id, u.ID, time.UnixMilli(sess.AuthTime), amr); err != nil {
		h.render(w, loginData{ID: id, ClientName: clientName, Error: "登录请求已过期"})
		return
	}
	h.audit.Emit(r.Context(), service.Event{
		Type: model.EventLoginSuccess, UserID: u.ID, Username: u.Username,
		ClientID: clientID, IP: ip, UserAgent: ua, Result: "success",
	})
	http.Redirect(w, r, h.callback(r.Context(), id), http.StatusFound)
}

func (h *loginHandler) clientName(ctx context.Context, clientID string) string {
	if c, err := h.clients.Get(ctx, clientID); err == nil {
		return c.ClientName
	}
	return clientID
}

func (h *loginHandler) render(w http.ResponseWriter, data loginData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = loginTmpl.Execute(w, data)
}

func loginErrText(err error) string {
	switch {
	case errors.Is(err, service.ErrAccountLocked):
		return "账号已锁定，请 15 分钟后再试或联系管理员"
	case errors.Is(err, service.ErrAccountDisabled):
		return "账号已停用，请联系管理员"
	default:
		return "用户名或密码错误"
	}
}

// ---------------- logout ----------------

type logoutHandler struct {
	cfg      *config.Config
	sessions *service.SessionService
}

func newLogoutHandler(cfg *config.Config, sessions *service.SessionService) *logoutHandler {
	return &logoutHandler{cfg: cfg, sessions: sessions}
}

func (h *logoutHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		_ = h.sessions.Revoke(r.Context(), c.Value)
	}
	// 清除与写入共用同一实现（含 Domain/SameSite），否则只会留下 host-only 空 cookie。
	cookie.ClearSession(w, h.cfg)
	next := r.URL.Query().Get("next")
	if next == "" {
		next = "/logged-out"
	} else if u, err := url.Parse(next); err != nil || u.Host != "" {
		next = "/logged-out"
	}
	http.Redirect(w, r, next, http.StatusFound)
}

// ---------------- 共享工具 ----------------

func setSessionCookie(w http.ResponseWriter, cfg *config.Config, sid string, ttl time.Duration) {
	cookie.SetSession(w, cfg, sid, ttl)
}

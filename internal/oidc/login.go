package oidc

import (
	"context"
	"errors"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
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
.hint{background:#fffbeb;color:#92400e;padding:9px 12px;border-radius:6px;font-size:13px;margin-bottom:8px}
</style></head><body>
<form class="card" method="post" action="/login">
  <h1>统一身份认证</h1>
  <p class="sub">登录后返回 {{.ClientName}}</p>
  {{if .Degraded}}<p class="hint">登录状态未能在跳转中延续，请在此直接登录</p>{{end}}
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

// loginExpiredTmpl 是「从门户回跳但授权请求已失效」时的提示页（FR-021 / C1b）。
//
// 与旧的裸 400 文本的区别：用户可能是因找回密码 / 绑定 MFA 在门户停留超过请求有效期，
// 此时必须给出可继续的路径，而不是一句没有出路的错误。由于请求已不存在，IdP 拿不到
// 原应用的地址，因此只能引导「返回原系统重新发起」，并在配置了门户时提供一个重新登录入口。
var loginExpiredTmpl = template.Must(template.New("login-expired").Parse(`<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><title>登录状态已失效</title>
<style>
body{font-family:-apple-system,"PingFang SC",sans-serif;background:#f5f6f8;display:flex;align-items:center;justify-content:center;height:100vh;margin:0}
.card{background:#fff;padding:40px;border-radius:12px;box-shadow:0 8px 30px rgba(0,0,0,.08);width:360px}
h1{font-size:20px;margin:0 0 10px}p{color:#555;font-size:14px;line-height:1.6;margin:0 0 10px}
a.btn{display:inline-block;margin-top:18px;padding:10px 16px;border-radius:6px;background:#2563eb;color:#fff;text-decoration:none;font-size:14px}
</style></head><body>
<div class="card">
  <h1>登录状态已失效</h1>
  <p>本次登录等待时间过长（例如在登录页找回密码或绑定动态验证码），授权请求已过期。</p>
  <p>请返回刚才的系统页面重新发起登录。</p>
  {{if .PortalURL}}<a class="btn" href="{{.PortalURL}}">重新登录</a>{{end}}
</div></body></html>`))

type loginData struct {
	ID         string
	ClientName string
	Username   string
	MFATicket  string
	Error      string
	// Degraded 表示本次是「从门户回跳但没带回会话」的降级渲染（FR-006/FR-020）。
	Degraded bool
	// PortalURL 仅用于过期提示页的「重新登录」入口（可能为空）。
	PortalURL string
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
	marker := r.URL.Query().Get(portalMarkerParam) == portalMarkerValue
	req, reqOK := h.st.PendingRequest(id)

	// 只看「是否带有效会话」，不在这一层做落点判断（落点全部交给 decideStep）。
	var sess *model.Session
	if c, err := r.Cookie(sessionCookie); err == nil {
		if s, err := h.sessions.Get(r.Context(), c.Value); err == nil {
			sess = s
		}
	}

	switch step := decideStep(reqOK, sess != nil, h.cfg.PortalLoginURL != "", marker); step {
	case StepBadRequest:
		// 请求不存在且不是从门户回来：保持改造前的行为。
		http.Error(w, "登录请求已过期，请重新发起授权", http.StatusBadRequest)

	case StepRequestExpired:
		// 从门户回来但请求已失效（多因在门户停留超过有效期）：给出可继续的入口。
		h.renderExpired(w, loginData{PortalURL: portalBase(h.cfg.PortalLoginURL)})

	case StepComplete:
		clientName := h.clientName(r.Context(), req.ClientID)
		if err := h.st.CompleteLogin(r.Context(), id, sess.UserID, time.UnixMilli(sess.AuthTime), sess.AMR); err != nil {
			h.render(w, loginData{ID: id, ClientName: clientName, Error: "登录状态异常，请重试"})
			return
		}
		h.emitSSOLoginSuccess(r, sess.UserID, req.ClientID, sess.AMR, marker)
		http.Redirect(w, r, h.callback(r.Context(), id), http.StatusFound)

	case StepTemplate, StepTemplateDegraded:
		degraded := step == StepTemplateDegraded
		clientName := h.clientName(r.Context(), req.ClientID)
		if degraded {
			// loop guard 触发：回跳到 IdP 仍然没有会话，说明跨站没带回 cookie（域/SameSite/过期）。
			// 只记录可定位的字段，绝不写 sid 或任何凭据。
			h.log.Warn("portal roundtrip without session",
				"reason", "portal_roundtrip_without_session",
				"client_id", req.ClientID,
				"request_id", id,
			)
		}
		h.render(w, loginData{ID: id, ClientName: clientName, Degraded: degraded})

	case StepPortal:
		clientName := h.clientName(r.Context(), req.ClientID)
		target, err := portalRedirectURL(h.cfg.PortalLoginURL, h.cfg.Issuer, id, clientName)
		if err != nil {
			// 配置异常（启动校验通常会先拦下）：退回内置登录页，不让用户卡死。
			h.log.Error("build portal redirect url", "error", err, "request_id", id)
			h.render(w, loginData{ID: id, ClientName: clientName})
			return
		}
		http.Redirect(w, r, target, http.StatusFound)
	}
}

// emitSSOLoginSuccess 记录「凭既有会话直接完成授权」（FR-010）。
// 与表单登录的 login_success 互不替代；审计失败只记日志，绝不阻断 302。
func (h *loginHandler) emitSSOLoginSuccess(r *http.Request, userID, clientID string, amr []string, fromPortal bool) {
	username := ""
	if u, err := h.users.GetByID(r.Context(), userID); err == nil {
		username = u.Username
	}
	h.audit.Emit(r.Context(), ssoLoginEvent(
		userID, username, clientID,
		clientip.FromRequest(r, h.cfg.TrustedProxies), r.UserAgent(),
		amr, fromPortal,
	))
}

// portalBase 去掉门户地址上的查询串与片段，得到「重新登录」入口。
// 配置非法或为空时返回空串（过期页就不渲染该按钮）。
func portalBase(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
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
	renderHTML(w, renderLoginTemplate, data)
}

// renderExpired 渲染「登录状态已失效」提示页（C1b）。
func (h *loginHandler) renderExpired(w http.ResponseWriter, data loginData) {
	renderHTML(w, renderExpiredTemplate, data)
}

// renderHTML 是唯一写响应头的出口，抽出来让模板渲染可以单测（不需要真实 http 栈）。
func renderHTML(w http.ResponseWriter, fn func(io.Writer, loginData) error, data loginData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = fn(w, data)
}

// renderLoginTemplate 渲染内置登录页（含降级态的说明行）。
func renderLoginTemplate(w io.Writer, data loginData) error {
	return loginTmpl.Execute(w, data)
}

// renderExpiredTemplate 渲染过期提示页。
func renderExpiredTemplate(w io.Writer, data loginData) error {
	return loginExpiredTmpl.Execute(w, data)
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

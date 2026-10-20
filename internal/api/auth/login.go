package auth

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/omino/omino-sso/internal/api/dto"
	"github.com/omino/omino-sso/internal/api/response"
	"github.com/omino/omino-sso/internal/config"
	"github.com/omino/omino-sso/internal/service"
	"github.com/omino/omino-sso/internal/store/model"
)

// Handler 处理 /api/v1/auth/*。
type Handler struct {
	cfg      *config.Config
	users    *service.UserService
	sessions *service.SessionService
	audit    *service.Auditor
	log      *slog.Logger
}

// NewHandler 构造。
func NewHandler(cfg *config.Config, users *service.UserService, sessions *service.SessionService, audit *service.Auditor, logger *slog.Logger) *Handler {
	return &Handler{cfg: cfg, users: users, sessions: sessions, audit: audit, log: logger}
}

// LoginRequest 规范 §4.3 请求体。
type LoginRequest struct {
	Username    string `json:"username" binding:"required,min=1,max=64"`
	Password    string `json:"password" binding:"required,min=8,max=128"`
	CaptchaID   string `json:"captchaId"`
	CaptchaCode string `json:"captchaCode"`
	ClientID    string `json:"clientId"`
	RememberMe  bool   `json:"rememberMe"`
}

// Login 账号密码登录。
func (h *Handler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.ErrValidation)
		return
	}
	clientID := req.ClientID
	if clientID == "" {
		clientID = "portal"
	}
	meta := service.Meta{IP: c.ClientIP(), UserAgent: c.Request.UserAgent()}

	u, err := h.users.Authenticate(c.Request.Context(), service.NormalizeUsername(req.Username), req.Password, meta)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrAccountLocked):
			response.Fail(c, response.ErrAccountLocked)
		case errors.Is(err, service.ErrAccountDisabled):
			response.Fail(c, response.ErrAccountDisabled)
		case errors.Is(err, service.ErrInvalidCredentials):
			response.Fail(c, response.ErrInvalidCredentials)
		default:
			h.log.Error("authenticate", "err", err)
			response.Fail(c, response.ErrInternal)
		}
		return
	}

	if service.NeedMFA(u, false) {
		ticket, err := h.sessions.CreateMFATicket(c.Request.Context(), u.ID, 5*time.Minute)
		if err != nil {
			h.log.Error("create mfa ticket", "err", err)
			response.Fail(c, response.ErrInternal)
			return
		}
		// 严格对齐规范 §4.3：二次验证响应只含 mfaRequired / mfaTicket / expiresIn。
		response.Success(c, gin.H{
			"mfaRequired": true,
			"mfaTicket":   ticket,
			"expiresIn":   300,
		})
		return
	}

	h.completeLogin(c, u, []string{"pwd"}, req.RememberMe, clientID)
}

func (h *Handler) completeLogin(c *gin.Context, u *model.User, amr []string, remember bool, clientID string) {
	sess, err := h.sessions.Create(c.Request.Context(), u, amr, remember, c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		h.log.Error("create session", "err", err)
		response.Fail(c, response.ErrInternal)
		return
	}
	ttl := h.cfg.SessionTTL
	if remember {
		ttl = h.cfg.SessionTTLRemember
	}
	setCookie(c, h.cfg, sess.SID, ttl)

	h.audit.Emit(c.Request.Context(), service.Event{
		Type:      model.EventLoginSuccess,
		UserID:    u.ID,
		Username:  u.Username,
		ClientID:  clientID,
		IP:        c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
		Result:    "success",
	})
	response.Success(c, gin.H{
		"mfaRequired": false,
		"mfaTicket":   nil,
		"expiresIn":   0,
		"redirectTo":  "/account",
		"user":        dto.Brief(u),
	})
}

// Logout 登出：失效服务端会话并清除 cookie。
func (h *Handler) Logout(c *gin.Context) {
	sessID, _ := c.Cookie("sid")
	if sessID != "" {
		if err := h.sessions.Revoke(c.Request.Context(), sessID); err != nil {
			h.log.Error("revoke session", "err", err)
		}
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name: "sid", Value: "", Path: "/", MaxAge: -1,
		Domain: h.cfg.CookieDomain, HttpOnly: true, Secure: h.cfg.CookieSecure,
	})
	if u, ok := currentUser(c); ok {
		h.audit.Emit(c.Request.Context(), service.Event{
			Type: model.EventLogout, UserID: u.ID, Username: u.Username,
			IP: c.ClientIP(), UserAgent: c.Request.UserAgent(), Result: "success",
		})
	}
	response.Success(c, nil)
}

func setCookie(c *gin.Context, cfg *config.Config, sid string, ttl time.Duration) {
	sameSite := http.SameSiteLaxMode
	if cfg.CookieSameSite == "Strict" {
		sameSite = http.SameSiteStrictMode
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     "sid",
		Value:    sid,
		Path:     "/",
		Domain:   cfg.CookieDomain,
		HttpOnly: true,
		Secure:   cfg.CookieSecure,
		SameSite: sameSite,
		MaxAge:   int(ttl.Seconds()),
	})
}

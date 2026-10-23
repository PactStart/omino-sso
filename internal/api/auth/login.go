package auth

import (
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/omino/omino-sso/internal/api/dto"
	"github.com/omino/omino-sso/internal/api/middleware"
	"github.com/omino/omino-sso/internal/api/response"
	"github.com/omino/omino-sso/internal/config"
	"github.com/omino/omino-sso/internal/service"
	"github.com/omino/omino-sso/internal/service/notification"
	"github.com/omino/omino-sso/internal/store/model"
)

// Handler 处理 /api/v1/auth/*。
type Handler struct {
	cfg      *config.Config
	users    *service.UserService
	sessions *service.SessionService
	notif    *notification.Service
	captcha  *service.CaptchaService
	audit    *service.Auditor
	log      *slog.Logger
}

// NewHandler 构造。
func NewHandler(
	cfg *config.Config,
	users *service.UserService,
	sessions *service.SessionService,
	notif *notification.Service,
	captcha *service.CaptchaService,
	audit *service.Auditor,
	logger *slog.Logger,
) *Handler {
	return &Handler{
		cfg: cfg, users: users, sessions: sessions, notif: notif,
		captcha: captcha, audit: audit, log: logger,
	}
}

// 登录方式枚举（契约 verification-api.md §2）。
const (
	LoginTypePassword  string = "password"
	LoginTypeEmailCode string = "email_code"
	LoginTypeSMSCode   string = "sms_code"
)

// LoginRequest 规范 §4.3 请求体（005 扩展多方式登录）。
// password 方式下 identifier 与 username 二者传其一；验证码方式下必传 identifier + code。
type LoginRequest struct {
	LoginType   string `json:"loginType" binding:"omitempty,oneof=password email_code sms_code" example:"password"`
	Identifier  string `json:"identifier" binding:"omitempty,max=128" example:"zhangsan"`
	Username    string `json:"username" binding:"omitempty,max=64" example:"zhangsan"`
	Password    string `json:"password" binding:"omitempty,max=128" example:"S3cure!pass"`
	Code        string `json:"code" binding:"omitempty,len=6" example:"123456"`
	CaptchaID   string `json:"captchaId" example:"01J9ZK2Q3W5X7Y9A1B3C5D7E9F"`
	CaptchaCode string `json:"captchaCode" example:"8f3k"`
	ClientID    string `json:"clientId" example:"portal"`
	RememberMe  bool   `json:"rememberMe" example:"false"`
}

// Login 账号密码登录。
//
// @Summary      账号密码登录
// @Description  校验账号密码。账号启用 MFA 时不下发会话，改返回 mfaTicket（300 秒内有效，需再调用 /auth/mfa/verify）；连续失败 5 次锁定 15 分钟。成功时通过 Set-Cookie 下发 HttpOnly 的 sid，响应体不含任何令牌。
// @Description  响应 data 有两种形状：需 MFA 时为 dto.LoginMfaChallenge（mfaRequired/mfaTicket/expiresIn），登录完成时为 dto.LoginResult（含 redirectTo 与 user）；文档只保留后者的 schema。
// @Tags         认证
// @Accept       json
// @Produce      json
// @Param        body  body  LoginRequest  true  "登录请求体"
// @Success      200   {object}  response.Body{data=dto.LoginMfaChallenge}  "需要二次 MFA 校验"
// @Success      200   {object}  response.Body{data=dto.LoginResult}  "登录完成"
// @Failure      400   {object}  response.Body  "VALIDATION_FAILED / AUTH_CAPTCHA_INVALID"
// @Failure      401   {object}  response.Body  "AUTH_INVALID_CREDENTIALS"
// @Failure      403   {object}  response.Body  "AUTH_ACCOUNT_DISABLED"
// @Failure      423   {object}  response.Body  "AUTH_ACCOUNT_LOCKED"
// @Failure      429   {object}  response.Body  "RATE_LIMITED"
// @Failure      500   {object}  response.Body  "INTERNAL_ERROR"
// @Failure      503   {object}  response.Body  "DEPENDENCY_UNAVAILABLE"
// @Router       /auth/login [post]
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

	loginType := req.LoginType
	if loginType == "" {
		loginType = LoginTypePassword
	}
	if loginType != LoginTypePassword {
		// 未登录的验证码登录先过图形验证码，避免批量试码与账号枚举（005 边界用例）。
		if !h.verifyCaptcha(c, req.CaptchaID, req.CaptchaCode) {
			return
		}
		h.loginByCode(c, req, loginType, clientID, meta)
		return
	}

	identifier := strings.TrimSpace(req.Identifier)
	if identifier == "" {
		identifier = req.Username
	}
	if identifier == "" || req.Password == "" {
		response.Fail(c, response.ErrValidation)
		return
	}

	u, err := h.users.AuthenticateByIdentifier(c.Request.Context(), identifier, req.Password, meta)
	if err != nil {
		h.failAuth(c, err)
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
		response.Success(c, dto.LoginMfaChallenge{
			MFARequired: true,
			MFATicket:   ticket,
			ExpiresIn:   300,
		})
		return
	}

	h.completeLogin(c, u, []string{"pwd"}, req.RememberMe, clientID)
}

// loginByCode 邮箱/手机验证码登录。验证码已证明渠道归属，因此豁免 TOTP 二步（澄清 Q6）。
func (h *Handler) loginByCode(c *gin.Context, req LoginRequest, loginType, clientID string, meta service.Meta) {
	target := strings.TrimSpace(req.Identifier)
	if target == "" || len(req.Code) != 6 {
		response.Fail(c, response.ErrValidation)
		return
	}
	channel := notification.ChannelEmail
	amr := []string{"email_otp"}
	if loginType == LoginTypeSMSCode {
		channel = notification.ChannelSMS
		amr = []string{"sms_otp"}
	}
	u, err := h.users.AuthenticateByCode(c.Request.Context(), channel, target, req.Code, meta)
	if err != nil {
		h.failAuth(c, err)
		return
	}
	h.completeLogin(c, u, amr, req.RememberMe, clientID)
}

// failAuth 统一映射认证类错误到登记的错误码。
func (h *Handler) failAuth(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrAccountLocked):
		response.Fail(c, response.ErrAccountLocked)
	case errors.Is(err, service.ErrAccountDisabled):
		response.Fail(c, response.ErrAccountDisabled)
	case errors.Is(err, service.ErrInvalidCredentials):
		response.Fail(c, response.ErrInvalidCredentials)
	case errors.Is(err, notification.ErrCodeInvalid):
		response.Fail(c, response.ErrAuthCodeInvalid)
	case errors.Is(err, notification.ErrTooFrequent):
		response.Fail(c, response.ErrRateLimited.WithMessage("验证码发送过于频繁，请稍后再试"))
	case errors.Is(err, notification.ErrChannelDisabled), errors.Is(err, notification.ErrSendFailed):
		response.Fail(c, response.ErrVerifySendFailed)
	default:
		h.log.Error("authenticate", "err", err)
		response.Fail(c, response.ErrInternal)
	}
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
	response.Success(c, dto.LoginResult{
		MFARequired: false,
		MFATicket:   nil,
		ExpiresIn:   0,
		RedirectTo:  "/account",
		User:        dto.Brief(u),
	})
}

// Logout 登出：失效服务端会话并清除 cookie。
//
// @Summary      登出
// @Description  吊销服务端会话并清除 sid cookie，立即生效（幂等：会话已失效时仍返回成功），并写入 logout 审计事件。
// @Tags         认证
// @Produce      json
// @Security     cookieAuth
// @Success      200  {object}  response.Body  "已登出"
// @Failure      401  {object}  response.Body  "UNAUTHENTICATED"
// @Failure      500  {object}  response.Body  "INTERNAL_ERROR"
// @Failure      503  {object}  response.Body  "DEPENDENCY_UNAVAILABLE"
// @Router       /auth/logout [post]
func (h *Handler) Logout(c *gin.Context) {
	sessID, _ := c.Cookie("sid")
	if sessID != "" {
		if err := h.sessions.Revoke(c.Request.Context(), sessID); err != nil {
			h.log.Error("revoke session", "err", err)
		}
	}
	middleware.ClearSessionCookie(c, h.cfg)
	if u, ok := currentUser(c); ok {
		h.audit.Emit(c.Request.Context(), service.Event{
			Type: model.EventLogout, UserID: u.ID, Username: u.Username,
			IP: c.ClientIP(), UserAgent: c.Request.UserAgent(), Result: "success",
		})
	}
	response.Success(c, nil)
}

// setCookie 保留本包内的调用点，实际写 cookie 统一由 middleware 实现，
// 避免"写入带 Domain、清除不带 Domain"的漂移。
func setCookie(c *gin.Context, cfg *config.Config, sid string, ttl time.Duration) {
	middleware.SetSessionCookie(c, cfg, sid, ttl)
}

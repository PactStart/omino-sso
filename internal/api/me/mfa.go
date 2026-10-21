package me

import (
	"errors"

	"github.com/gin-gonic/gin"

	"github.com/omino/omino-sso/internal/api/middleware"
	"github.com/omino/omino-sso/internal/api/response"
	"github.com/omino/omino-sso/internal/service"
	"github.com/omino/omino-sso/internal/service/notification"
)

// MFABindConfirmRequest 确认绑定 MFA 请求体。
type MFABindConfirmRequest struct {
	MFASetupTicket string `json:"mfaSetupTicket" binding:"required" example:"mfs_5c1e9a7b3d2f"`
	Code           string `json:"code" binding:"required,len=6" example:"123456"`
}

// MFAUnbindRequest 解绑 MFA 请求体。
type MFAUnbindRequest struct {
	Channel string `json:"channel" binding:"required,oneof=sms email" example:"sms"`
	Code    string `json:"code" binding:"required,len=6" example:"123456"`
}

// MFABindStartResult MFA 绑定启动结果。
type MFABindStartResult struct {
	OtpauthURL  string `json:"otpauthUrl" example:"otpauth://totp/omino-sso:zhangsan?secret=JBSWY3DPEHPK3PXP&issuer=omino-sso"`
	SetupTicket string `json:"mfaSetupTicket" example:"mfs_5c1e9a7b3d2f"`
	ExpiresIn   int    `json:"expiresIn" example:"600"`
}

// BoundResult MFA 绑定状态。
type BoundResult struct {
	Bound bool `json:"bound" example:"true"`
}

// StartMFABind 启动 MFA 绑定：生成 TOTP 密钥并返回 otpauth URL 与 setupTicket。
//
// @Summary      启动 MFA 绑定
// @Description  为当前账号生成 TOTP 密钥（暂存 10 分钟，不落库），返回 otpauth:// URL 供前端渲染二维码，以及一次性 mfaSetupTicket。需再调用 /me/mfa/bind/confirm 提交 6 位动态码后才真正启用。已绑定返回 MFA_ALREADY_BOUND。
// @Tags         我的
// @Produce      json
// @Security     cookieAuth
// @Success      200  {object}  response.Body{data=MFABindStartResult}  "success"
// @Failure      401  {object}  response.Body  "UNAUTHENTICATED"
// @Failure      409  {object}  response.Body  "MFA_ALREADY_BOUND"
// @Failure      500  {object}  response.Body  "INTERNAL_ERROR"
// @Failure      503  {object}  response.Body  "DEPENDENCY_UNAVAILABLE"
// @Router       /me/mfa/bind [post]
func (h *Handler) StartMFABind(c *gin.Context) {
	u, ok := middleware.CurrentUser(c)
	if !ok {
		response.Fail(c, response.ErrUnauthenticated)
		return
	}
	res, err := h.mfa.StartBind(c.Request.Context(), u)
	if err != nil {
		if errors.Is(err, service.ErrMFAAlreadyBound) {
			response.Fail(c, response.ErrMFAAlreadyBound)
			return
		}
		h.log.Error("start mfa bind", "err", err)
		response.Fail(c, response.ErrInternal)
		return
	}
	response.Success(c, MFABindStartResult{
		OtpauthURL:  res.OtpauthURL,
		SetupTicket: res.SetupTicket,
		ExpiresIn:   res.ExpiresIn,
	})
}

// ConfirmMFABind 校验 6 位动态码并完成绑定。
//
// @Summary      确认 MFA 绑定
// @Description  消费 mfaSetupTicket 并校验 6 位 TOTP 动态码，通过后加密保存密钥、置 mfa_enabled=true 并写入 mfa_bound 审计事件。ticket 一次性，过期或重复使用返回 AUTH_MFA_TICKET_EXPIRED。
// @Tags         我的
// @Accept       json
// @Produce      json
// @Security     cookieAuth
// @Param        body  body  MFABindConfirmRequest  true  "确认绑定请求体"
// @Success      200   {object}  response.Body{data=BoundResult}  "已绑定"
// @Failure      400   {object}  response.Body  "VALIDATION_FAILED / AUTH_MFA_INVALID / AUTH_MFA_TICKET_EXPIRED"
// @Failure      401   {object}  response.Body  "UNAUTHENTICATED"
// @Failure      500   {object}  response.Body  "INTERNAL_ERROR"
// @Failure      503   {object}  response.Body  "DEPENDENCY_UNAVAILABLE"
// @Router       /me/mfa/bind/confirm [post]
func (h *Handler) ConfirmMFABind(c *gin.Context) {
	u, ok := middleware.CurrentUser(c)
	if !ok {
		response.Fail(c, response.ErrUnauthenticated)
		return
	}
	var req MFABindConfirmRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.ErrValidation)
		return
	}
	err := h.mfa.ConfirmBind(c.Request.Context(), u, req.MFASetupTicket, req.Code,
		service.Meta{IP: c.ClientIP(), UserAgent: c.Request.UserAgent()})
	switch {
	case err == nil:
		response.Success(c, BoundResult{Bound: true})
	case errors.Is(err, service.ErrMFASetupTicketInvalid):
		response.Fail(c, response.ErrMFATicketExpired)
	case errors.Is(err, service.ErrMFAInvalidCode):
		response.Fail(c, response.ErrMFAInvalid)
	default:
		h.log.Error("confirm mfa bind", "err", err)
		response.Fail(c, response.ErrInternal)
	}
}

// UnbindMFA 通过短信/邮箱验证码解绑 MFA。
//
// @Summary      解绑 MFA
// @Description  校验发往当前账号已绑定手机号或邮箱的验证码（scene=mfa_unbind）后清空密钥并置 mfa_enabled=false，写入 mfa_unbound 审计事件。未绑定返回 MFA_NOT_BOUND。
// @Tags         我的
// @Accept       json
// @Produce      json
// @Security     cookieAuth
// @Param        body  body  MFAUnbindRequest  true  "解绑请求体"
// @Success      200   {object}  response.Body{data=BoundResult}  "已解绑"
// @Failure      400   {object}  response.Body  "VALIDATION_FAILED / AUTH_CODE_INVALID / MFA_NOT_BOUND"
// @Failure      401   {object}  response.Body  "UNAUTHENTICATED"
// @Failure      500   {object}  response.Body  "INTERNAL_ERROR"
// @Failure      503   {object}  response.Body  "DEPENDENCY_UNAVAILABLE"
// @Router       /me/mfa/unbind [post]
func (h *Handler) UnbindMFA(c *gin.Context) {
	u, ok := middleware.CurrentUser(c)
	if !ok {
		response.Fail(c, response.ErrUnauthenticated)
		return
	}
	var req MFAUnbindRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.ErrValidation)
		return
	}
	err := h.mfa.Unbind(c.Request.Context(), u, req.Channel, req.Code,
		service.Meta{IP: c.ClientIP(), UserAgent: c.Request.UserAgent()})
	switch {
	case err == nil:
		response.Success(c, BoundResult{Bound: false})
	case errors.Is(err, service.ErrMFANotBound):
		response.Fail(c, response.ErrMFANotBound)
	case errors.Is(err, notification.ErrCodeInvalid), errors.Is(err, service.ErrCodeInvalid):
		response.Fail(c, response.ErrAuthCodeInvalid)
	case errors.Is(err, service.ErrTargetNotBound):
		response.Fail(c, response.ErrValidation.WithMessage("当前账号未绑定该联系方式"))
	case errors.Is(err, service.ErrInvalidTarget):
		response.Fail(c, response.ErrValidation.WithMessage("渠道不合法"))
	default:
		h.log.Error("unbind mfa", "err", err)
		response.Fail(c, response.ErrInternal)
	}
}

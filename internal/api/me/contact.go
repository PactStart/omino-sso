package me

import (
	"errors"

	"github.com/gin-gonic/gin"

	"github.com/omino/omino-sso/internal/api/middleware"
	"github.com/omino/omino-sso/internal/api/response"
	"github.com/omino/omino-sso/internal/service"
	"github.com/omino/omino-sso/internal/service/notification"
)

// BindPhoneRequest 绑定/修改手机号请求体。
type BindPhoneRequest struct {
	Phone string `json:"phone" binding:"required,max=20" example:"13800138000"`
	Code  string `json:"code" binding:"required,len=6" example:"123456"`
}

// BindEmailRequest 绑定/修改邮箱请求体。
type BindEmailRequest struct {
	Email string `json:"email" binding:"required,max=128" example:"zhangsan@example.com"`
	Code  string `json:"code" binding:"required,len=6" example:"123456"`
}

// PhoneResult 绑定结果（脱敏手机号）。
type PhoneResult struct {
	Phone string `json:"phone" example:"138****8000"`
}

// EmailResult 绑定结果（脱敏邮箱）。
type EmailResult struct {
	Email string `json:"email" example:"z***@example.com"`
}

// BindPhone 绑定或修改手机号。
//
// @Summary      绑定/修改手机号
// @Description  校验短信验证码后写入手机号（修改时验证码发自新号码）。手机号全局唯一，已被他人占用返回 VERIFY_TARGET_IN_USE。响应返回服务端脱敏后的手机号。
// @Tags         我的
// @Accept       json
// @Produce      json
// @Security     cookieAuth
// @Param        body  body  BindPhoneRequest  true  "绑定手机号请求体"
// @Success      200   {object}  response.Body{data=PhoneResult}  "success"
// @Failure      400   {object}  response.Body  "VALIDATION_FAILED / AUTH_CODE_INVALID"
// @Failure      401   {object}  response.Body  "UNAUTHENTICATED"
// @Failure      409   {object}  response.Body  "VERIFY_TARGET_IN_USE"
// @Failure      500   {object}  response.Body  "INTERNAL_ERROR"
// @Failure      503   {object}  response.Body  "DEPENDENCY_UNAVAILABLE"
// @Router       /me/phone [post]
func (h *Handler) BindPhone(c *gin.Context) {
	u, ok := middleware.CurrentUser(c)
	if !ok {
		response.Fail(c, response.ErrUnauthenticated)
		return
	}
	var req BindPhoneRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.ErrValidation)
		return
	}
	masked, err := h.users.BindPhone(c.Request.Context(), u.ID, req.Phone, req.Code,
		service.Meta{IP: c.ClientIP(), UserAgent: c.Request.UserAgent()})
	if err != nil {
		h.failContact(c, err)
		return
	}
	response.Success(c, PhoneResult{Phone: masked})
}

// BindEmail 绑定或修改邮箱。
//
// @Summary      绑定/修改邮箱
// @Description  校验邮箱验证码后写入邮箱（修改时验证码发自新邮箱）。邮箱全局唯一，已被他人占用返回 VERIFY_TARGET_IN_USE。响应返回服务端脱敏后的邮箱。
// @Tags         我的
// @Accept       json
// @Produce      json
// @Security     cookieAuth
// @Param        body  body  BindEmailRequest  true  "绑定邮箱请求体"
// @Success      200   {object}  response.Body{data=EmailResult}  "success"
// @Failure      400   {object}  response.Body  "VALIDATION_FAILED / AUTH_CODE_INVALID"
// @Failure      401   {object}  response.Body  "UNAUTHENTICATED"
// @Failure      409   {object}  response.Body  "VERIFY_TARGET_IN_USE"
// @Failure      500   {object}  response.Body  "INTERNAL_ERROR"
// @Failure      503   {object}  response.Body  "DEPENDENCY_UNAVAILABLE"
// @Router       /me/email [post]
func (h *Handler) BindEmail(c *gin.Context) {
	u, ok := middleware.CurrentUser(c)
	if !ok {
		response.Fail(c, response.ErrUnauthenticated)
		return
	}
	var req BindEmailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.ErrValidation)
		return
	}
	masked, err := h.users.BindEmail(c.Request.Context(), u.ID, req.Email, req.Code,
		service.Meta{IP: c.ClientIP(), UserAgent: c.Request.UserAgent()})
	if err != nil {
		h.failContact(c, err)
		return
	}
	response.Success(c, EmailResult{Email: masked})
}

// failContact 统一映射联系方式绑定/解绑类错误。
func (h *Handler) failContact(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrTargetInUse):
		response.Fail(c, response.ErrVerifyTargetUsed)
	case errors.Is(err, notification.ErrCodeInvalid), errors.Is(err, service.ErrCodeInvalid):
		response.Fail(c, response.ErrAuthCodeInvalid)
	case errors.Is(err, notification.ErrTooFrequent):
		response.Fail(c, response.ErrRateLimited.WithMessage("验证码发送过于频繁，请稍后再试"))
	case errors.Is(err, service.ErrInvalidTarget):
		response.Fail(c, response.ErrValidation.WithMessage("手机号或邮箱格式不正确"))
	case errors.Is(err, service.ErrTargetNotBound):
		response.Fail(c, response.ErrValidation.WithMessage("当前账号未绑定该联系方式"))
	case errors.Is(err, notification.ErrChannelDisabled), errors.Is(err, notification.ErrSendFailed):
		response.Fail(c, response.ErrVerifySendFailed)
	default:
		h.log.Error("contact operation", "err", err)
		response.Fail(c, response.ErrInternal)
	}
}

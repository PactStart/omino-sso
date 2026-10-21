package me

import (
	"errors"

	"github.com/gin-gonic/gin"

	"github.com/omino/omino-sso/internal/api/middleware"
	"github.com/omino/omino-sso/internal/api/response"
	"github.com/omino/omino-sso/internal/service"
	"github.com/omino/omino-sso/internal/service/notification"
)

// ResetPasswordRequest 已登录用户通过验证码重置密码请求体。
type ResetPasswordRequest struct {
	Channel     string `json:"channel" binding:"required,oneof=sms email" example:"sms"`
	Code        string `json:"code" binding:"required,len=6" example:"123456"`
	NewPassword string `json:"newPassword" binding:"required,min=8,max=128" example:"N3w!pass2026"`
}

// ResetPassword 已登录用户通过验证码重置密码。
//
// @Summary      重置密码（已登录）
// @Description  向当前账号已绑定的手机号或邮箱发送验证码（scene=find_password），校验通过后更新密码。成功后保留当前会话、吊销该账号其他设备会话，并写入 password_reset 审计事件。
// @Tags         我的
// @Accept       json
// @Produce      json
// @Security     cookieAuth
// @Param        body  body  ResetPasswordRequest  true  "重置密码请求体"
// @Success      200   {object}  response.Body  "已重置"
// @Failure      400   {object}  response.Body  "VALIDATION_FAILED / AUTH_CODE_INVALID / WEAK_PASSWORD"
// @Failure      401   {object}  response.Body  "UNAUTHENTICATED"
// @Failure      500   {object}  response.Body  "INTERNAL_ERROR"
// @Failure      503   {object}  response.Body  "DEPENDENCY_UNAVAILABLE"
// @Router       /me/password/reset [post]
func (h *Handler) ResetPassword(c *gin.Context) {
	u, ok := middleware.CurrentUser(c)
	if !ok {
		response.Fail(c, response.ErrUnauthenticated)
		return
	}
	var req ResetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.ErrValidation)
		return
	}
	sid, _ := c.Cookie(middleware.SessionCookie)
	err := h.users.ResetPasswordWithCode(
		c.Request.Context(), u, req.Channel, req.Code, req.NewPassword, sid,
		service.Meta{IP: c.ClientIP(), UserAgent: c.Request.UserAgent()},
	)
	switch {
	case err == nil:
		response.Success(c, struct{}{})
	case errors.Is(err, service.ErrWeakPassword):
		response.Fail(c, response.ErrWeakPassword)
	case errors.Is(err, service.ErrSamePassword):
		response.Fail(c, response.ErrValidation.WithMessage("新密码不能与原密码相同"))
	case errors.Is(err, notification.ErrCodeInvalid), errors.Is(err, service.ErrCodeInvalid):
		response.Fail(c, response.ErrAuthCodeInvalid)
	case errors.Is(err, service.ErrTargetNotBound):
		response.Fail(c, response.ErrValidation.WithMessage("当前账号未绑定该联系方式，请先到账号中心绑定"))
	case errors.Is(err, service.ErrInvalidTarget):
		response.Fail(c, response.ErrValidation.WithMessage("渠道不合法"))
	default:
		h.log.Error("reset password", "err", err)
		response.Fail(c, response.ErrInternal)
	}
}

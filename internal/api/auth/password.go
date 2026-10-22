package auth

import (
	"errors"

	"github.com/gin-gonic/gin"

	"github.com/omino/omino-sso/internal/api/response"
	"github.com/omino/omino-sso/internal/service"
	"github.com/omino/omino-sso/internal/service/notification"
)

// ResetPasswordRequest 找回（重置）密码请求体（契约 verification-api.md §3）。
type ResetPasswordRequest struct {
	Channel     string `json:"channel" binding:"required,oneof=sms email" example:"email"`
	Target      string `json:"target" binding:"required,max=128" example:"zhangsan@example.com"`
	Code        string `json:"code" binding:"required,len=6" example:"123456"`
	NewPassword string `json:"newPassword" binding:"required,min=8,max=128" example:"N3w!pass2026"`
}

// ResetPassword 未登录通过邮箱/手机验证码重置密码。
//
// @Summary      找回（重置）密码
// @Description  校验邮箱或手机验证码后设置新密码。成功不自动登录（需用新密码重新登录），并吊销该账号全部会话、写入 password_reset 审计事件。未绑定该联系方式的账号统一返回 AUTH_CODE_INVALID，不泄露账号存在性。
// @Tags         认证
// @Accept       json
// @Produce      json
// @Param        body  body  ResetPasswordRequest  true  "重置密码请求体"
// @Success      200   {object}  response.Body  "已重置"
// @Failure      400   {object}  response.Body  "VALIDATION_FAILED / AUTH_CODE_INVALID / WEAK_PASSWORD"
// @Failure      403   {object}  response.Body  "AUTH_ACCOUNT_DISABLED"
// @Failure      429   {object}  response.Body  "RATE_LIMITED"
// @Failure      500   {object}  response.Body  "INTERNAL_ERROR"
// @Failure      503   {object}  response.Body  "DEPENDENCY_UNAVAILABLE"
// @Router       /auth/password/reset [post]
func (h *Handler) ResetPassword(c *gin.Context) {
	var req ResetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.ErrValidation)
		return
	}
	err := h.users.ResetPasswordByCode(
		c.Request.Context(), req.Channel, req.Target, req.Code, req.NewPassword,
		service.Meta{IP: c.ClientIP(), UserAgent: c.Request.UserAgent()},
	)
	switch {
	case err == nil:
		response.Success(c, struct{}{})
	case errors.Is(err, service.ErrWeakPassword):
		response.Fail(c, response.ErrWeakPassword)
	case errors.Is(err, notification.ErrCodeInvalid):
		response.Fail(c, response.ErrAuthCodeInvalid)
	case errors.Is(err, service.ErrAccountDisabled):
		response.Fail(c, response.ErrAccountDisabled)
	case errors.Is(err, service.ErrInvalidTarget):
		response.Fail(c, response.ErrValidation.WithMessage("手机号或邮箱格式不正确"))
	default:
		h.log.Error("reset password", "err", err)
		response.Fail(c, response.ErrInternal)
	}
}

package auth

import (
	"github.com/gin-gonic/gin"
	"github.com/pquerna/otp/totp"

	"github.com/omino/omino-sso/internal/api/dto"
	"github.com/omino/omino-sso/internal/api/middleware"
	"github.com/omino/omino-sso/internal/api/response"
	"github.com/omino/omino-sso/internal/service"
	"github.com/omino/omino-sso/internal/store/model"
)

// MFAVerifyRequest 规范 §4.6。
type MFAVerifyRequest struct {
	MFATicket string `json:"mfaTicket" binding:"required" example:"mft_5c1e9a7b3d2f"`
	Code      string `json:"code" binding:"required,len=6" example:"123456"`
}

// VerifyMFA 校验 TOTP 并完成登录。
//
// @Summary      校验 TOTP 动态码
// @Description  消费 mfaTicket 并校验 6 位 TOTP 动态码。校验通过后创建会话并下发 sid cookie；ticket 一次性，重复使用返回 AUTH_MFA_TICKET_EXPIRED，校验失败写入 mfa_failure 审计事件。
// @Tags         认证
// @Accept       json
// @Produce      json
// @Param        body  body  MFAVerifyRequest  true  "MFA 校验请求体"
// @Success      200   {object}  response.Body{data=dto.MfaResult}  "登录完成"
// @Failure      400   {object}  response.Body  "VALIDATION_FAILED / AUTH_MFA_INVALID / AUTH_MFA_TICKET_EXPIRED"
// @Failure      404   {object}  response.Body  "NOT_FOUND"
// @Failure      429   {object}  response.Body  "RATE_LIMITED"
// @Failure      500   {object}  response.Body  "INTERNAL_ERROR"
// @Failure      503   {object}  response.Body  "DEPENDENCY_UNAVAILABLE"
// @Router       /auth/mfa/verify [post]
func (h *Handler) VerifyMFA(c *gin.Context) {
	var req MFAVerifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.ErrValidation)
		return
	}
	uid, err := h.sessions.ConsumeMFATicket(c.Request.Context(), req.MFATicket)
	if err != nil {
		response.Fail(c, response.ErrMFATicketExpired)
		return
	}
	u, err := h.users.GetByID(c.Request.Context(), uid)
	if err != nil {
		response.Fail(c, response.ErrNotFound)
		return
	}
	secret, err := h.users.DecryptMFASecret(u)
	if err != nil {
		h.log.Error("decrypt mfa secret", "err", err, "user", u.ID)
		response.Fail(c, response.ErrInternal)
		return
	}
	if !totp.Validate(req.Code, secret) {
		h.audit.Emit(c.Request.Context(), service.Event{
			Type: model.EventMFAFailure, UserID: u.ID, Username: u.Username,
			IP: c.ClientIP(), UserAgent: c.Request.UserAgent(), Result: "failure",
		})
		response.Fail(c, response.ErrMFAInvalid)
		return
	}
	h.audit.Emit(c.Request.Context(), service.Event{
		Type: model.EventMFASuccess, UserID: u.ID, Username: u.Username,
		IP: c.ClientIP(), UserAgent: c.Request.UserAgent(), Result: "success",
	})

	sess, err := h.sessions.Create(c.Request.Context(), u, []string{"pwd", "otp"}, false, c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		h.log.Error("create session", "err", err)
		response.Fail(c, response.ErrInternal)
		return
	}
	setCookie(c, h.cfg, sess.SID, h.cfg.SessionTTL)
	response.Success(c, dto.MfaResult{
		RedirectTo: "/account",
		User:       dto.Brief(u),
	})
}

func currentUser(c *gin.Context) (*model.User, bool) {
	return middleware.CurrentUser(c)
}

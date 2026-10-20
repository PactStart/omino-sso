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
	MFATicket string `json:"mfaTicket" binding:"required"`
	Code      string `json:"code" binding:"required,len=6"`
}

// VerifyMFA 校验 TOTP 并完成登录。
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

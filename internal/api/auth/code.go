package auth

import (
	"errors"

	"github.com/gin-gonic/gin"

	"github.com/omino/omino-sso/internal/api/middleware"
	"github.com/omino/omino-sso/internal/api/response"
	"github.com/omino/omino-sso/internal/service/notification"
	"github.com/omino/omino-sso/internal/store/model"
)

// SendCodeResult 验证码签发结果。
type SendCodeResult struct {
	ExpiresIn   int `json:"expiresIn" example:"300"`
	ResendAfter int `json:"resendAfter" example:"60"`
}

// SendCodeRequest 发送验证码请求体（契约 verification-api.md §1）。
type SendCodeRequest struct {
	Scene       string `json:"scene" binding:"required,oneof=bind_phone bind_email login find_password mfa_unbind" example:"bind_phone"`
	Channel     string `json:"channel" binding:"required,oneof=sms email" example:"sms"`
	Target      string `json:"target" binding:"required,max=128" example:"13800138000"`
	CaptchaID   string `json:"captchaId" example:"01J9ZK2Q3W5X7Y9A1B3C5D7E9F"`
	CaptchaCode string `json:"captchaCode" example:"8f3k"`
}

// SendCode 发送短信/邮箱验证码。
//
// @Summary      发送验证码
// @Description  按场景向手机号或邮箱发送 6 位验证码（默认 5 分钟有效，同一目标 60 秒内不可重发）。bind_phone / bind_email 需登录会话且目标已被他人占用时返回 VERIFY_TARGET_IN_USE；login / find_password / mfa_unbind 对未注册或未绑定的目标返回与成功一致的响应但不真实发送（防枚举）。provider=mock 时验证码固定 888888 且不产生真实短信/邮件。
// @Description  图形验证码字段（captchaId/captchaCode）为未登录防刷预留，当前与 /auth/login 一致仅做透传。
// @Tags         认证
// @Accept       json
// @Produce      json
// @Param        body  body  SendCodeRequest  true  "发送验证码请求体"
// @Success      200   {object}  response.Body{data=SendCodeResult}  "已发送（或按防枚举约定静默成功）"
// @Failure      400   {object}  response.Body  "VALIDATION_FAILED"
// @Failure      401   {object}  response.Body  "UNAUTHENTICATED（bind 类场景缺少会话）"
// @Failure      409   {object}  response.Body  "VERIFY_TARGET_IN_USE"
// @Failure      429   {object}  response.Body  "RATE_LIMITED"
// @Failure      500   {object}  response.Body  "INTERNAL_ERROR"
// @Failure      502   {object}  response.Body  "VERIFY_SEND_FAILED"
// @Failure      503   {object}  response.Body  "DEPENDENCY_UNAVAILABLE"
// @Router       /auth/code [post]
func (h *Handler) SendCode(c *gin.Context) {
	var req SendCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.ErrValidation)
		return
	}
	ctx := c.Request.Context()
	user, hasUser := middleware.CurrentUser(c)

	normalized, err := notification.NormalizeTarget(req.Channel, req.Target)
	if err != nil {
		response.Fail(c, response.ErrValidation.WithMessage("手机号或邮箱格式不正确"))
		return
	}
	// 场景与渠道必须匹配（bind_phone 只能走短信，bind_email 只能走邮箱）。
	if want, ok := notification.ChannelForScene(req.Scene); !ok || (want != "" && want != req.Channel) {
		response.Fail(c, response.ErrValidation.WithMessage("场景与渠道不匹配"))
		return
	}

	in := notification.IssueInput{
		Scene:     req.Scene,
		Channel:   req.Channel,
		Target:    normalized,
		IP:        c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
	}

	switch req.Scene {
	case notification.SceneBindPhone, notification.SceneBindEmail:
		if !hasUser {
			response.Fail(c, response.ErrUnauthenticated)
			return
		}
		in.UserID = user.ID
		owner, err := h.users.ContactOwner(ctx, req.Channel, normalized)
		if err != nil {
			h.log.Error("lookup contact owner", "err", err)
			response.Fail(c, response.ErrInternal)
			return
		}
		if owner != nil && owner.ID != user.ID {
			response.Fail(c, response.ErrVerifyTargetUsed)
			return
		}
	case notification.SceneMFAUnbind:
		if !hasUser {
			response.Fail(c, response.ErrUnauthenticated)
			return
		}
		in.UserID = user.ID
		if !mfaUnbindTargetMatches(user, req.Channel, normalized) {
			response.Fail(c, response.ErrValidation.WithMessage("验证码需发送到当前账号已绑定的联系方式"))
			return
		}
	default:
		// login / find_password：防枚举——目标不存在时返回与成功一致的响应但不发送。
		in.UserID = ""
		if hasUser {
			in.UserID = user.ID
		}
		owner, err := h.users.ContactOwner(ctx, req.Channel, normalized)
		if err != nil {
			h.log.Error("lookup contact owner", "err", err)
			response.Fail(c, response.ErrInternal)
			return
		}
		if owner == nil {
			// 防枚举：与真实发送成功时的响应完全一致。
			response.Success(c, SendCodeResult{ExpiresIn: 300, ResendAfter: 60})
			return
		}
		in.UserID = owner.ID
	}

	result, err := h.notif.Issue(ctx, in)
	if err != nil {
		h.failSendCode(c, err)
		return
	}
	response.Success(c, result)
}

func (h *Handler) failSendCode(c *gin.Context, err error) {
	switch {
	case errors.Is(err, notification.ErrTooFrequent):
		response.Fail(c, response.ErrRateLimited.WithMessage("验证码发送过于频繁，请稍后再试"))
	case errors.Is(err, notification.ErrSendFailed), errors.Is(err, notification.ErrChannelDisabled):
		response.Fail(c, response.ErrVerifySendFailed)
	case errors.Is(err, notification.ErrInvalidTarget):
		response.Fail(c, response.ErrValidation.WithMessage("手机号或邮箱格式不正确"))
	default:
		h.log.Error("send verification code", "err", err)
		response.Fail(c, response.ErrInternal)
	}
}

// mfaUnbindTargetMatches 校验解绑验证码的目标与当前账号已绑定的联系方式一致。
func mfaUnbindTargetMatches(u *model.User, channel, target string) bool {
	switch channel {
	case notification.ChannelSMS:
		return u.Phone != nil && *u.Phone == target
	case notification.ChannelEmail:
		return u.Email != nil && *u.Email == target
	default:
		return false
	}
}

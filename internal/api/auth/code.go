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
	Target      string `json:"target" binding:"omitempty,max=128" example:"13800138000"`
	CaptchaID   string `json:"captchaId" example:"01J9ZK2Q3W5X7Y9A1B3C5D7E9F"`
	CaptchaCode string `json:"captchaCode" example:"8f3k"`
}

// SendCode 发送短信/邮箱验证码。
//
// @Summary      发送验证码
// @Description  按场景向手机号或邮箱发送 6 位验证码（默认 5 分钟有效，同一目标 60 秒内不可重发）。bind_phone / bind_email 需登录会话且目标已被他人占用时返回 VERIFY_TARGET_IN_USE；login 场景目标未绑定任何账号时返回 VERIFY_TARGET_NOT_FOUND（2026-10-22 起，为可诊断性放弃该场景的防枚举）；find_password 仍对未注册目标返回与成功一致的响应但不真实发送。provider=mock 时验证码固定 888888 且不产生真实短信/邮件。
// @Description  目标解析：已登录会话的 mfa_unbind 与 find_password 场景忽略请求体 target，一律取该账号已绑定的联系方式（脱敏后不回落浏览器）；未登录的 login / find_password 与 bind_* 场景必须传 target。
// @Description  图形验证码：未登录调用 login / find_password 场景必须携带 captchaId + captchaCode（先调用 GET /auth/captcha），否则返回 AUTH_CAPTCHA_INVALID；已登录调用不校验。
// @Tags         认证
// @Accept       json
// @Produce      json
// @Param        body  body  SendCodeRequest  true  "发送验证码请求体"
// @Success      200   {object}  response.Body{data=SendCodeResult}  "已发送（或按防枚举约定静默成功）"
// @Failure      400   {object}  response.Body  "VALIDATION_FAILED / AUTH_CAPTCHA_INVALID"
// @Failure      401   {object}  response.Body  "UNAUTHENTICATED（bind 类场景缺少会话）"
// @Failure      404   {object}  response.Body  "VERIFY_TARGET_NOT_FOUND"
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

	// 场景与渠道必须匹配（bind_phone 只能走短信，bind_email 只能走邮箱）。
	if want, ok := notification.ChannelForScene(req.Scene); !ok || (want != "" && want != req.Channel) {
		response.Fail(c, response.ErrValidation.WithMessage("场景与渠道不匹配"))
		return
	}

	// 已登录账号的 mfa_unbind / find_password 场景：目标由服务端按会话解析，
	// 不信任客户端传值（联系方式在 /me 中已脱敏，浏览器不再持有完整值）。
	target := req.Target
	sendsToSelf := hasUser && (req.Scene == notification.SceneMFAUnbind || req.Scene == notification.SceneFindPassword)
	if sendsToSelf {
		bound, ok := boundContact(user, req.Channel)
		if !ok {
			response.Fail(c, response.ErrValidation.WithMessage("验证码需发送到当前账号已绑定的联系方式"))
			return
		}
		target = bound
	}

	normalized, err := notification.NormalizeTarget(req.Channel, target)
	if err != nil {
		response.Fail(c, response.ErrValidation.WithMessage("手机号或邮箱格式不正确"))
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
	default:
		// login / find_password。
		if sendsToSelf {
			in.UserID = user.ID
			break
		}
		// 未登录：先过图形验证码再查账号（防批量试码与枚举）。
		if !h.verifyCaptcha(c, req.CaptchaID, req.CaptchaCode) {
			return
		}
		in.UserID = ""
		owner, err := h.users.ContactOwner(ctx, req.Channel, normalized)
		if err != nil {
			h.log.Error("lookup contact owner", "err", err)
			response.Fail(c, response.ErrInternal)
			return
		}
		if owner == nil {
			// login 场景明确告知"未绑定"，否则用户只能看到"发送成功"却永远收不到码
			// （1600 行日志也查不到记录），代价是放弃该场景的防枚举（规范 §4.1）。
			// find_password 仍保持静默成功，避免在找回密码流程泄露注册状态。
			if req.Scene == notification.SceneLogin {
				response.Fail(c, response.ErrVerifyTargetNotFound.WithMessage(
					"该"+channelLabel(req.Channel)+"未绑定任何账号，请用密码登录后到账号中心绑定"))
				return
			}
			// 响应形状与真实发送一致，但阈值取自配置，避免前端倒计时与策略脱节。
			expiresIn, resendAfter := h.notif.Policy()
			response.Success(c, SendCodeResult{ExpiresIn: expiresIn, ResendAfter: resendAfter})
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

// channelLabel 把渠道枚举转成用户可读的称呼，用于错误提示文案。
func channelLabel(channel string) string {
	if channel == notification.ChannelSMS {
		return "手机号"
	}
	return "邮箱"
}

// boundContact 返回账号在该渠道上已绑定的完整联系方式。
func boundContact(u *model.User, channel string) (string, bool) {
	switch channel {
	case notification.ChannelSMS:
		if u.Phone != nil && *u.Phone != "" {
			return *u.Phone, true
		}
	case notification.ChannelEmail:
		if u.Email != nil && *u.Email != "" {
			return *u.Email, true
		}
	}
	return "", false
}

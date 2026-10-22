package auth

import (
	"github.com/gin-gonic/gin"

	"github.com/omino/omino-sso/internal/api/response"
	"github.com/omino/omino-sso/internal/service"
)

// CaptchaResult 图形验证码签发结果。
type CaptchaResult struct {
	CaptchaID string `json:"captchaId" example:"01J9ZK2Q3W5X7Y9A1B3C5D7E9F"`
	Image     string `json:"image" example:"data:image/png;base64,iVBORw0KGgoAAAANSUhEUg..."`
	ExpiresIn int    `json:"expiresIn" example:"120"`
}

// Captcha GET /api/v1/auth/captcha
//
// @Summary      获取图形验证码
// @Description  返回 4 位图形验证码的 id 与可直接用于 <img src> 的 data URL（2 分钟有效、一次有效）。未登录调用 /auth/code（login、find_password 场景）与 /auth/login（email_code、sms_code）时必须携带该 id 与用户输入值，否则返回 AUTH_CAPTCHA_INVALID。已登录会话不校验图形验证码。
// @Tags         认证
// @Produce      json
// @Success      200  {object}  response.Body{data=CaptchaResult}  "success"
// @Failure      429  {object}  response.Body  "RATE_LIMITED"
// @Failure      500  {object}  response.Body  "INTERNAL_ERROR"
// @Failure      503  {object}  response.Body  "DEPENDENCY_UNAVAILABLE"
// @Router       /auth/captcha [get]
func (h *Handler) Captcha(c *gin.Context) {
	cid, image, err := h.captcha.Issue(c.Request.Context())
	if err != nil {
		h.log.Error("issue captcha", "err", err)
		response.Fail(c, response.ErrInternal)
		return
	}
	response.Success(c, CaptchaResult{
		CaptchaID: cid,
		Image:     image,
		ExpiresIn: service.CaptchaTTLSeconds,
	})
}

// verifyCaptcha 校验图形验证码；校验失败时直接写响应并返回 false。
func (h *Handler) verifyCaptcha(c *gin.Context, captchaID, code string) bool {
	if err := h.captcha.Verify(c.Request.Context(), captchaID, code); err != nil {
		response.Fail(c, response.ErrCaptchaInvalid)
		return false
	}
	return true
}

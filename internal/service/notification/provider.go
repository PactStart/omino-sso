// Package notification 提供短信/邮箱验证码的发送抽象与验证码生命周期管理。
//
// 分层约定（AGENTS.md §3）：本包属于 service 层能力，按供应商拆分实现
// （创蓝短信 / SMTP 邮件 / mock 模拟），业务侧只依赖 Provider 接口，
// 供应商密钥与协议细节不外泄到 api 层。
package notification

import (
	"context"
	"errors"
	"net/mail"
	"regexp"
	"strings"

	"github.com/omino/omino-sso/internal/store/model"
)

// 渠道与场景常量，取值与 send_records 数据模型、接口契约保持一致。
const (
	ChannelSMS   = model.SendChannelSMS
	ChannelEmail = model.SendChannelEmail

	SceneBindPhone    = model.SendSceneBindPhone
	SceneBindEmail    = model.SendSceneBindEmail
	SceneLogin        = model.SendSceneLogin
	SceneFindPassword = model.SendSceneFindPassword
	SceneMFAUnbind    = model.SendSceneMFAUnbind
)

// MockCode 为模拟发送时固定的验证码（FR-017）。
const MockCode = "888888"

// 发送相关哨兵错误，由 API 层映射为错误码。
var (
	ErrInvalidTarget = errors.New("invalid notification target")
	ErrTooFrequent   = errors.New("verification code requested too frequently")
	ErrCodeInvalid   = errors.New("verification code invalid or expired")
	ErrSendFailed    = errors.New("send verification code failed")
	ErrNotSupported  = errors.New("channel not supported by provider")
)

// Provider 是发送通道抽象。新增供应商只需实现该接口并在 New 中注册，
// 调用方（验证码服务、业务服务）无需改动。
type Provider interface {
	// Name 返回供应商名，写入 send_records.provider。
	Name() string
	// Send 发送验证码。仅需支持自身渠道，其他渠道返回 ErrNotSupported。
	Send(ctx context.Context, channel, target, code string) error
}

// AuditSink 由 service.Auditor 实现，用于异步投递验证码发送审计。
type AuditSink interface {
	VerificationCodeSent(ctx context.Context, e VerificationSentEvent)
}

// VerificationSentEvent 描述一次验证码发送的审计内容。
type VerificationSentEvent struct {
	UserID    string
	Channel   string
	Scene     string
	Target    string // 已脱敏
	Result    string
	Provider  string
	IP        string
	UserAgent string
}

var phonePattern = regexp.MustCompile(`^1\d{10}$`)

// NormalizeTarget 校验并规范化接收方：手机号（中国大陆 11 位）、邮箱（小写）。
func NormalizeTarget(channel, target string) (string, error) {
	switch channel {
	case ChannelSMS:
		t := strings.TrimSpace(target)
		if !phonePattern.MatchString(t) {
			return "", ErrInvalidTarget
		}
		return t, nil
	case ChannelEmail:
		t := strings.ToLower(strings.TrimSpace(target))
		addr, err := mail.ParseAddress(t)
		if err != nil || addr.Address != t || !strings.Contains(t, "@") {
			return "", ErrInvalidTarget
		}
		return t, nil
	default:
		return "", ErrInvalidTarget
	}
}

// ValidScene 判断场景是否在契约枚举内。
func ValidScene(scene string) bool {
	switch scene {
	case SceneBindPhone, SceneBindEmail, SceneLogin, SceneFindPassword, SceneMFAUnbind:
		return true
	default:
		return false
	}
}

// ChannelForScene 返回场景绑定的渠道；场景与渠道不匹配时返回 false。
func ChannelForScene(scene string) (string, bool) {
	switch scene {
	case SceneBindPhone:
		return ChannelSMS, true
	case SceneBindEmail:
		return ChannelEmail, true
	case SceneLogin, SceneFindPassword, SceneMFAUnbind:
		// 登录与 MFA 解绑支持短信或邮箱两种渠道，由请求的 channel 决定。
		return "", true
	default:
		return "", false
	}
}

// MaskTarget 按渠道脱敏接收方（服务端脱敏，前端不做二次处理）。
func MaskTarget(channel, target string) string {
	switch channel {
	case ChannelSMS:
		return MaskPhone(target)
	case ChannelEmail:
		return MaskEmail(target)
	default:
		return target
	}
}

// MaskPhone 手机号脱敏：13800138000 -> 138****8000。
func MaskPhone(phone string) string {
	if len(phone) != 11 {
		return phone
	}
	return phone[:3] + "****" + phone[7:]
}

// MaskEmail 邮箱脱敏：zhangsan@example.com -> z***@example.com。
func MaskEmail(email string) string {
	at := strings.LastIndex(email, "@")
	if at <= 0 {
		return email
	}
	local, domain := email[:at], email[at:]
	if len(local) <= 1 {
		return local + "***" + domain
	}
	return local[:1] + "***" + domain
}

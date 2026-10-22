package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/omino/omino-sso/internal/config"
	"github.com/omino/omino-sso/internal/pkg/crypto"
	"github.com/omino/omino-sso/internal/service/notification"
	"github.com/omino/omino-sso/internal/store"
	"github.com/omino/omino-sso/internal/store/model"
)

// MFA 绑定/解绑相关错误（005-verification-code-auth）。
var (
	ErrMFAAlreadyBound       = errors.New("mfa already bound")
	ErrMFANotBound           = errors.New("mfa not bound")
	ErrMFAInvalidCode        = errors.New("invalid totp code")
	ErrMFASetupTicketInvalid = errors.New("mfa setup ticket expired or invalid")
)

const (
	mfaSetupKeyPrefix = "mfa:setup:"
	mfaSetupTTL       = 10 * time.Minute
	mfaIssuer         = "omino-sso"
)

// MFAService 负责账号中心内的 TOTP 绑定与解绑（US4）。
// 登录侧的二步校验仍由 api/auth 复用既有流程。
type MFAService struct {
	st    *store.Store
	cfg   *config.Config
	notif *notification.Service
	audit *Auditor
	log   *slog.Logger
}

// NewMFAService 构造。
func NewMFAService(st *store.Store, cfg *config.Config, notif *notification.Service, audit *Auditor, logger *slog.Logger) *MFAService {
	if logger == nil {
		logger = slog.Default()
	}
	return &MFAService{st: st, cfg: cfg, notif: notif, audit: audit, log: logger}
}

// MFABindStart 绑定启动结果：前端用 otpauthUrl 渲染二维码。
type MFABindStart struct {
	OtpauthURL  string `json:"otpauthUrl"`
	SetupTicket string `json:"mfaSetupTicket"`
	ExpiresIn   int    `json:"expiresIn"`
}

// StartBind 生成 TOTP 密钥并暂存到 Redis，返回 otpauth:// URL 与一次性 setupTicket。
// 此时不写库、不置 mfa_enabled，只有确认动态码后才生效（FR-015）。
func (s *MFAService) StartBind(ctx context.Context, u *model.User) (*MFABindStart, error) {
	if u.MFAEnabled {
		return nil, ErrMFAAlreadyBound
	}
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      mfaIssuer,
		AccountName: u.Username,
	})
	if err != nil {
		return nil, err
	}
	ticket := crypto.RandomToken(18)
	if err := s.st.Redis().Set(ctx, mfaSetupKeyPrefix+ticket, key.Secret(), mfaSetupTTL).Err(); err != nil {
		return nil, err
	}
	return &MFABindStart{
		OtpauthURL:  key.URL(),
		SetupTicket: ticket,
		ExpiresIn:   int(mfaSetupTTL.Seconds()),
	}, nil
}

// ConfirmBind 校验 6 位动态码后加密保存密钥并启用 MFA。
func (s *MFAService) ConfirmBind(ctx context.Context, u *model.User, ticket, code string, meta Meta) error {
	secret, err := s.st.Redis().GetDel(ctx, mfaSetupKeyPrefix+ticket).Result()
	if err != nil {
		return ErrMFASetupTicketInvalid
	}
	if !totp.Validate(code, secret) {
		return ErrMFAInvalidCode
	}
	enc, err := crypto.Encrypt(s.cfg.CryptoKey, []byte(secret))
	if err != nil {
		return err
	}
	if err := s.st.UpdateUserFields(ctx, u.ID, map[string]any{
		"mfa_secret":  enc,
		"mfa_enabled": true,
	}); err != nil {
		return err
	}
	s.audit.Emit(ctx, Event{
		Type: model.EventMFABound, UserID: u.ID, Username: u.Username,
		IP: meta.IP, UserAgent: meta.UserAgent, Result: "success",
	})
	return nil
}

// Unbind 通过短信/邮箱验证码解绑 MFA（FR-016），避免设备遗失后账号被锁死。
func (s *MFAService) Unbind(ctx context.Context, u *model.User, channel, code string, meta Meta) error {
	if !u.MFAEnabled {
		return ErrMFANotBound
	}
	if s.notif == nil {
		return notification.ErrChannelDisabled
	}
	var target string
	switch channel {
	case notification.ChannelSMS:
		if u.Phone == nil || *u.Phone == "" {
			return ErrTargetNotBound
		}
		target = *u.Phone
	case notification.ChannelEmail:
		if u.Email == nil || *u.Email == "" {
			return ErrTargetNotBound
		}
		target = *u.Email
	default:
		return ErrInvalidTarget
	}
	if err := s.notif.Verify(ctx, channel, notification.SceneMFAUnbind, target, code); err != nil {
		return err
	}
	if err := s.st.UpdateUserFields(ctx, u.ID, map[string]any{
		"mfa_secret":  nil,
		"mfa_enabled": false,
	}); err != nil {
		return err
	}
	s.audit.Emit(ctx, Event{
		Type: model.EventMFAUnbound, UserID: u.ID, Username: u.Username,
		IP: meta.IP, UserAgent: meta.UserAgent, Result: "success",
		Fields: map[string]any{"channel": channel},
	})
	return nil
}

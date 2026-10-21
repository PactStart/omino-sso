package notification

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/omino/omino-sso/internal/config"
	"github.com/omino/omino-sso/internal/pkg/id"
	"github.com/omino/omino-sso/internal/store"
	"github.com/omino/omino-sso/internal/store/model"
)

// 验证码 Redis key 前缀（TTL + 一次性消费 + 错误计数，决策 4）。
const (
	codeKeyPrefix    = "verify:code:"
	attemptKeyPrefix = "verify:attempt:"
	resendKeyPrefix  = "verify:resend:"
)

// Service 签发与校验验证码，并记录每次发送结果。
type Service struct {
	st  *store.Store
	log *slog.Logger

	sms   Provider
	email Provider
	audit AuditSink

	codeTTL     time.Duration
	resendAfter time.Duration
	maxAttempts int
}

// New 按配置选择供应商实现；未配置的通道为 nil，调用时返回通道未启用错误。
func New(cfg *config.Config, st *store.Store, audit AuditSink, logger *slog.Logger) (*Service, error) {
	if logger == nil {
		logger = slog.Default()
	}
	s := &Service{st: st, log: logger, audit: audit}

	switch cfg.SMS.Provider {
	case "chuanglan":
		s.sms = NewChuanglan(cfg.SMS)
	case "mock":
		s.sms = NewMock()
	}
	switch cfg.Mail.Provider {
	case "smtp":
		p, err := NewSMTP(cfg.Mail)
		if err != nil {
			return nil, err
		}
		s.email = p
	case "mock":
		s.email = NewMock()
	}

	s.codeTTL = cfg.Verify.CodeTTL
	if s.codeTTL <= 0 {
		s.codeTTL = 5 * time.Minute
	}
	s.resendAfter = cfg.Verify.ResendInterval
	if s.resendAfter <= 0 {
		s.resendAfter = 60 * time.Second
	}
	s.maxAttempts = cfg.Verify.MaxAttempts
	if s.maxAttempts <= 0 {
		s.maxAttempts = 5
	}
	return s, nil
}

// IssueInput 签发验证码入参。
type IssueInput struct {
	Scene     string
	Channel   string
	Target    string
	UserID    string
	IP        string
	UserAgent string
}

// IssueResult 签发结果。
type IssueResult struct {
	ExpiresIn   int `json:"expiresIn"`
	ResendAfter int `json:"resendAfter"`
}

// ErrChannelDisabled 通道未配置（provider 为空）。
var ErrChannelDisabled = errors.New("notification channel not configured")

// Issue 生成验证码、写入 Redis、调用供应商发送，并同步写发送记录与审计。
func (s *Service) Issue(ctx context.Context, in IssueInput) (IssueResult, error) {
	if !ValidScene(in.Scene) {
		return IssueResult{}, ErrInvalidTarget
	}
	if want, ok := ChannelForScene(in.Scene); ok && want != "" && want != in.Channel {
		return IssueResult{}, ErrInvalidTarget
	}
	target, err := NormalizeTarget(in.Channel, in.Target)
	if err != nil {
		return IssueResult{}, err
	}
	provider, err := s.provider(in.Channel)
	if err != nil {
		return IssueResult{}, err
	}

	rdb := s.st.Redis()
	resendKey := resendKeyPrefix + in.Scene + ":" + in.Channel + ":" + target
	ok, err := rdb.SetNX(ctx, resendKey, 1, s.resendAfter).Result()
	if err != nil {
		return IssueResult{}, fmt.Errorf("resend guard: %w", err)
	}
	if !ok {
		return IssueResult{}, ErrTooFrequent
	}

	code := s.newCode(provider.Name())
	codeKey := codeKeyPrefix + in.Scene + ":" + in.Channel + ":" + target
	attemptKey := attemptKeyPrefix + in.Scene + ":" + in.Channel + ":" + target
	if err := rdb.Set(ctx, codeKey, code, s.codeTTL).Err(); err != nil {
		_ = rdb.Del(ctx, resendKey).Err()
		return IssueResult{}, fmt.Errorf("store code: %w", err)
	}
	_ = rdb.Del(ctx, attemptKey).Err()

	masked := MaskTarget(in.Channel, target)
	if sendErr := provider.Send(ctx, in.Channel, target, code); sendErr != nil {
		// 发送失败：验证码不可用，同时释放重发间隔允许用户重试。
		_ = rdb.Del(ctx, codeKey).Err()
		_ = rdb.Del(ctx, resendKey).Err()
		s.persist(ctx, in, masked, model.SendResultFailed, provider.Name(), sendErr.Error())
		s.auditSent(ctx, in, masked, model.SendResultFailed, provider.Name())
		s.log.Error("send verification code", "channel", in.Channel, "scene", in.Scene, "provider", provider.Name(), "err", sendErr)
		return IssueResult{}, ErrSendFailed
	}

	s.persist(ctx, in, masked, model.SendResultSuccess, provider.Name(), "")
	s.auditSent(ctx, in, masked, model.SendResultSuccess, provider.Name())
	return IssueResult{
		ExpiresIn:   int(s.codeTTL.Seconds()),
		ResendAfter: int(s.resendAfter.Seconds()),
	}, nil
}

// Verify 校验并消费验证码（一次性）；连续错误达上限后立即失效。
func (s *Service) Verify(ctx context.Context, channel, scene, target, code string) error {
	target, err := NormalizeTarget(channel, target)
	if err != nil {
		return ErrCodeInvalid
	}
	rdb := s.st.Redis()
	codeKey := codeKeyPrefix + scene + ":" + channel + ":" + target
	attemptKey := attemptKeyPrefix + scene + ":" + channel + ":" + target

	stored, err := rdb.Get(ctx, codeKey).Result()
	if errors.Is(err, redis.Nil) {
		return ErrCodeInvalid
	}
	if err != nil {
		return fmt.Errorf("load code: %w", err)
	}
	if subtle.ConstantTimeCompare([]byte(stored), []byte(code)) != 1 {
		n, incrErr := rdb.Incr(ctx, attemptKey).Result()
		if incrErr != nil {
			return fmt.Errorf("increase attempts: %w", incrErr)
		}
		if n == 1 {
			_ = rdb.Expire(ctx, attemptKey, s.codeTTL).Err()
		}
		if n >= int64(s.maxAttempts) {
			_ = rdb.Del(ctx, codeKey).Err()
			_ = rdb.Del(ctx, attemptKey).Err()
		}
		return ErrCodeInvalid
	}

	// 一次性消费：GETDEL 原子取出并删除，避免并发复用。
	consumed, err := rdb.GetDel(ctx, codeKey).Result()
	if errors.Is(err, redis.Nil) {
		return ErrCodeInvalid
	}
	if err != nil {
		return fmt.Errorf("consume code: %w", err)
	}
	if subtle.ConstantTimeCompare([]byte(consumed), []byte(code)) != 1 {
		return ErrCodeInvalid
	}
	_ = rdb.Del(ctx, attemptKey).Err()
	return nil
}

// RecordFilter 发送记录查询条件（管理端）。
type RecordFilter struct {
	Channel  string
	Scene    string
	Result   string
	From     int64
	To       int64
	Page     int
	PageSize int
}

// ListRecords 分页查询发送记录；未指定时间范围时默认最近 30 天，
// 保证查询命中 created_at 前置索引。
func (s *Service) ListRecords(ctx context.Context, f RecordFilter) ([]model.SendRecord, int64, error) {
	if f.To <= 0 {
		f.To = time.Now().UnixMilli()
	}
	if f.From <= 0 {
		f.From = f.To - 30*24*int64(time.Hour/time.Millisecond)
	}
	if f.Page <= 0 {
		f.Page = 1
	}
	if f.PageSize <= 0 {
		f.PageSize = 20
	}
	if f.PageSize > 200 {
		f.PageSize = 200
	}
	return s.st.ListSendRecords(ctx, store.SendRecordFilter{
		Channel:  f.Channel,
		Scene:    f.Scene,
		Result:   f.Result,
		From:     f.From,
		To:       f.To,
		Page:     f.Page,
		PageSize: f.PageSize,
	})
}

// Enabled 报告通道是否已配置。
func (s *Service) Enabled(channel string) bool {
	_, err := s.provider(channel)
	return err == nil
}

func (s *Service) provider(channel string) (Provider, error) {
	switch channel {
	case ChannelSMS:
		if s.sms == nil {
			return nil, ErrChannelDisabled
		}
		return s.sms, nil
	case ChannelEmail:
		if s.email == nil {
			return nil, ErrChannelDisabled
		}
		return s.email, nil
	default:
		return nil, ErrInvalidTarget
	}
}

func (s *Service) newCode(providerName string) string {
	if providerName == model.SendProviderMock {
		return MockCode
	}
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "000000"
	}
	return fmt.Sprintf("%06d", n.Int64())
}

func (s *Service) persist(ctx context.Context, in IssueInput, masked, result, provider, errMsg string) {
	rec := &model.SendRecord{
		ID:        id.New(),
		Channel:   in.Channel,
		Scene:     in.Scene,
		Target:    masked,
		Result:    result,
		Provider:  provider,
		CreatedAt: time.Now().UnixMilli(),
	}
	if errMsg != "" {
		msg := errMsg
		if len(msg) > 255 {
			msg = msg[:255]
		}
		rec.Error = &msg
	}
	// 同步落库，保证 100% 可追溯（决策 9）；写库失败只告警，不掩盖供应商已发送的事实。
	if err := s.st.CreateSendRecord(ctx, rec); err != nil {
		s.log.Error("persist send record", "err", err, "channel", in.Channel, "scene", in.Scene)
	}
}

func (s *Service) auditSent(ctx context.Context, in IssueInput, masked, result, provider string) {
	if s.audit == nil {
		return
	}
	s.audit.VerificationCodeSent(ctx, VerificationSentEvent{
		UserID:    in.UserID,
		Channel:   in.Channel,
		Scene:     in.Scene,
		Target:    masked,
		Result:    result,
		Provider:  provider,
		IP:        in.IP,
		UserAgent: in.UserAgent,
	})
}

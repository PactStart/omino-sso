package service

import (
	"context"
	"errors"
	"time"

	"github.com/omino/omino-sso/internal/config"
	"github.com/omino/omino-sso/internal/pkg/crypto"
	"github.com/omino/omino-sso/internal/store"
	"github.com/omino/omino-sso/internal/store/model"
)

func nowMillis() int64 { return time.Now().UnixMilli() }

// SessionService 负责会话与 MFA 票据。
type SessionService struct {
	st  *store.Store
	cfg *config.Config
}

// NewSessionService 构造。
func NewSessionService(st *store.Store, cfg *config.Config) *SessionService {
	return &SessionService{st: st, cfg: cfg}
}

// Create 创建会话：sid 为 256bit 随机串的十六进制。
func (s *SessionService) Create(ctx context.Context, user *model.User, amr []string, remember bool, ip, ua string) (*model.Session, error) {
	ttl := s.cfg.SessionTTL
	if remember {
		ttl = s.cfg.SessionTTLRemember
	}
	now := nowMillis()
	sess := &model.Session{
		SID:        crypto.RandomHex(32),
		UserID:     user.ID,
		TenantID:   user.TenantID,
		AMR:        amr,
		AuthTime:   now,
		IP:         ip,
		CreatedAt:  now,
		LastSeenAt: now,
		ExpiresAt:  now + ttl.Milliseconds(),
	}
	if ua != "" {
		sess.UserAgent = &ua
	}
	if err := s.st.CreateSession(ctx, sess); err != nil {
		return nil, err
	}
	return sess, nil
}

// Get 读取会话。
func (s *SessionService) Get(ctx context.Context, sid string) (*model.Session, error) {
	return s.st.GetSession(ctx, sid)
}

// Revoke 吊销单个会话。
func (s *SessionService) Revoke(ctx context.Context, sid string) error {
	return s.st.DeleteSession(ctx, sid)
}

// RevokeAll 吊销用户全部会话。
func (s *SessionService) RevokeAll(ctx context.Context, userID string) error {
	return s.st.DeleteSessionsByUser(ctx, userID)
}

// List 列出用户会话。
func (s *SessionService) List(ctx context.Context, userID string) ([]model.Session, error) {
	return s.st.ListSessionsByUser(ctx, userID, nowMillis())
}

// 以下为 MFA 二次校验票据（Redis，TTL 300s）。

const mfaTicketPrefix = "mfa:ticket:"

// CreateMFATicket 用户已通过密码校验但需要 MFA，签发一次性票据。
func (s *SessionService) CreateMFATicket(ctx context.Context, userID string, ttl time.Duration) (string, error) {
	ticket := crypto.RandomToken(18)
	if err := s.st.Redis().Set(ctx, mfaTicketPrefix+ticket, userID, ttl).Err(); err != nil {
		return "", err
	}
	return ticket, nil
}

// ErrMFATicketInvalid 票据不存在或已过期。
var ErrMFATicketInvalid = errors.New("mfa ticket expired or invalid")

// ConsumeMFATicket 读取并删除票据（一次性）。
func (s *SessionService) ConsumeMFATicket(ctx context.Context, ticket string) (string, error) {
	key := mfaTicketPrefix + ticket
	uid, err := s.st.Redis().GetDel(ctx, key).Result()
	if err != nil {
		return "", ErrMFATicketInvalid
	}
	return uid, nil
}

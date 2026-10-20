package oidc

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/oidc/v3/pkg/op"
	"gorm.io/gorm"

	"github.com/omino/omino-sso/internal/config"
	"github.com/omino/omino-sso/internal/pkg/crypto"
	"github.com/omino/omino-sso/internal/pkg/id"
	"github.com/omino/omino-sso/internal/service"
	"github.com/omino/omino-sso/internal/store"
	"github.com/omino/omino-sso/internal/store/model"
)

var (
	// ErrAuthRequestInvalid 待认证请求不存在或已过期。
	ErrAuthRequestInvalid = errors.New("auth request not found or expired")
	// ErrRefreshInvalid refresh token 无效。
	ErrRefreshInvalid = errors.New("invalid refresh token")
)

const authRequestTTL = 10 * time.Minute

// Storage 是 op.Storage 的唯一实现。
// 只做"数据存取 + 业务校验"，不实现协议状态流转（规范 §7 约束 1）。
type Storage struct {
	st     *store.Store
	cfg    *config.Config
	keys   *KeyManager
	audit  *service.Auditor
	issuer string

	// 规范 §7.2 非目标 3：MVP 单副本，待认证请求保存在进程内存。
	pending sync.Map // id -> *pendingRequest
}

type pendingRequest struct {
	req       *authRequest
	expiresAt time.Time
}

// NewStorage 构造存储适配层。
func NewStorage(st *store.Store, cfg *config.Config, keys *KeyManager, audit *service.Auditor) *Storage {
	return &Storage{st: st, cfg: cfg, keys: keys, audit: audit, issuer: strings.TrimSuffix(cfg.Issuer, "/")}
}

// ---------------- op.OPStorage ----------------

func (s *Storage) GetClientByClientID(ctx context.Context, clientID string) (op.Client, error) {
	m, err := s.st.GetClient(ctx, clientID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("client %q not found", clientID)
		}
		return nil, err
	}
	if m.Status != 1 {
		return nil, fmt.Errorf("client %q is disabled", clientID)
	}
	return &clientAdapter{m: m, loginURL: func(reqID string) string {
		return s.issuer + "/login?id=" + url.QueryEscape(reqID)
	}}, nil
}

func (s *Storage) AuthorizeClientIDSecret(ctx context.Context, clientID, clientSecret string) error {
	m, err := s.st.GetClient(ctx, clientID)
	if err != nil {
		return fmt.Errorf("client not found")
	}
	if len(m.ClientSecretHash) == 0 {
		return fmt.Errorf("client has no secret")
	}
	if !crypto.ConstantTimeEqual(string(crypto.SHA256(clientSecret)), string(m.ClientSecretHash)) {
		return fmt.Errorf("invalid client secret")
	}
	return nil
}

func (s *Storage) GetKeyByIDAndClientID(ctx context.Context, keyID, clientID string) (*jose.JSONWebKey, error) {
	return nil, errors.New("jwt profile (private_key_jwt) is not supported")
}

func (s *Storage) ValidateJWTProfileScopes(ctx context.Context, userID string, scopes []string) ([]string, error) {
	return scopes, nil
}

// ---------------- op.AuthStorage ----------------

func (s *Storage) CreateAuthRequest(ctx context.Context, req *oidc.AuthRequest, userID string) (op.AuthRequest, error) {
	a := &authRequest{
		ID:           id.New(),
		ClientID:     req.ClientID,
		RedirectURI:  req.RedirectURI,
		Scopes:       req.Scopes,
		ResponseType: req.ResponseType,
		ResponseMode: req.ResponseMode,
		Nonce:        req.Nonce,
		State:        req.State,
		Subject:      userID,
		Audience:     []string{req.ClientID},
	}
	if req.CodeChallenge != "" {
		a.CodeChall = &oidc.CodeChallenge{Challenge: req.CodeChallenge, Method: req.CodeChallengeMethod}
	}
	s.pending.Store(a.ID, &pendingRequest{req: a, expiresAt: time.Now().Add(authRequestTTL)})
	return a, nil
}

func (s *Storage) AuthRequestByID(ctx context.Context, authRequestID string) (op.AuthRequest, error) {
	v, ok := s.pending.Load(authRequestID)
	if !ok {
		return nil, ErrAuthRequestInvalid
	}
	p := v.(*pendingRequest)
	if time.Now().After(p.expiresAt) {
		s.pending.Delete(authRequestID)
		return nil, ErrAuthRequestInvalid
	}
	return p.req, nil
}

func (s *Storage) AuthRequestByCode(ctx context.Context, code string) (op.AuthRequest, error) {
	row, err := s.st.ConsumeAuthCode(ctx, crypto.SHA256(code), time.Now().UnixMilli())
	if err != nil {
		if errors.Is(err, store.ErrCodeInvalid) {
			return nil, ErrAuthRequestInvalid
		}
		return nil, err
	}
	a := &authRequest{
		ID:           fmt.Sprintf("code-%x", row.CodeHash[:8]),
		ClientID:     row.ClientID,
		RedirectURI:  row.RedirectURI,
		Scopes:       strings.Fields(row.Scope),
		ResponseType: oidc.ResponseTypeCode,
		AuthTime:     time.UnixMilli(row.AuthTime),
		AMR:          row.AMR,
		Audience:     []string{row.ClientID},
		Subject:      row.UserID,
		done:         true,
	}
	if row.Nonce != nil {
		a.Nonce = *row.Nonce
	}
	if row.CodeChallenge != "" {
		a.CodeChall = &oidc.CodeChallenge{
			Challenge: row.CodeChallenge,
			Method:    oidc.CodeChallengeMethod(row.CodeChallengeMethod),
		}
	}
	return a, nil
}

func (s *Storage) SaveAuthCode(ctx context.Context, authRequestID, code string) error {
	v, ok := s.pending.Load(authRequestID)
	if !ok {
		return ErrAuthRequestInvalid
	}
	a := v.(*pendingRequest).req
	if !a.done || a.Subject == "" {
		return errors.New("auth request is not completed")
	}
	challenge := ""
	method := ""
	if a.CodeChall != nil {
		challenge = a.CodeChall.Challenge
		method = string(a.CodeChall.Method)
	}
	rec := store.CodeRecord{
		CodeHash:            crypto.SHA256(code),
		ClientID:            a.ClientID,
		UserID:              a.Subject,
		RedirectURI:         a.RedirectURI,
		Scope:               strings.Join(a.Scopes, " "),
		Nonce:               a.Nonce,
		CodeChallenge:       challenge,
		CodeChallengeMethod: method,
		AuthTime:            a.AuthTime.UnixMilli(),
		AMR:                 a.AMR,
		ExpiresAt:           time.Now().Add(s.cfg.AuthCodeTTL).UnixMilli(),
	}
	if rec.AuthTime == 0 {
		rec.AuthTime = time.Now().UnixMilli()
	}
	return s.st.InsertAuthCode(ctx, rec)
}

func (s *Storage) DeleteAuthRequest(ctx context.Context, authRequestID string) error {
	s.pending.Delete(authRequestID)
	return nil
}

func (s *Storage) CreateAccessToken(ctx context.Context, request op.TokenRequest) (string, time.Time, error) {
	ttl := s.cfg.AccessTokenTTL
	if c, err := s.st.GetClient(ctx, requestClientID(request)); err == nil && c.AccessTokenTTL > 0 {
		ttl = time.Duration(c.AccessTokenTTL) * time.Second
	}
	return id.New(), time.Now().Add(ttl), nil
}

func (s *Storage) CreateAccessAndRefreshTokens(ctx context.Context, request op.TokenRequest, currentRefreshToken string) (string, string, time.Time, error) {
	clientID := requestClientID(request)
	ttl := s.cfg.AccessTokenTTL
	refreshTTL := s.cfg.RefreshTokenTTL
	if c, err := s.st.GetClient(ctx, clientID); err == nil {
		if c.AccessTokenTTL > 0 {
			ttl = time.Duration(c.AccessTokenTTL) * time.Second
		}
		if c.RefreshTokenTTL > 0 {
			refreshTTL = time.Duration(c.RefreshTokenTTL) * time.Second
		}
	}

	setGrantedScope(ctx, request.GetScopes())
	now := time.Now()
	accessID := id.New()
	plain := crypto.RandomToken(32)
	newID := id.New()
	familyID := newID
	rotatedFrom := ""

	if currentRefreshToken != "" {
		old, err := s.st.GetRefreshTokenByHash(ctx, crypto.SHA256(currentRefreshToken))
		if err != nil {
			return "", "", time.Time{}, ErrRefreshInvalid
		}
		familyID = old.FamilyID
		rotatedFrom = old.ID
	}

	scope := strings.Join(request.GetScopes(), " ")
	next := &model.RefreshToken{
		ID:        newID,
		TokenHash: crypto.SHA256(plain),
		FamilyID:  familyID,
		UserID:    request.GetSubject(),
		ClientID:  clientID,
		Scope:     scope,
		AMR:       requestAMR(request),
		AuthTime:  requestAuthTime(request).UnixMilli(),
		ExpiresAt: now.Add(refreshTTL).UnixMilli(),
		CreatedAt: now.UnixMilli(),
	}
	if rotatedFrom != "" {
		next.RotatedFrom = &rotatedFrom
		if err := s.st.RotateRefreshToken(ctx, rotatedFrom, next, now.UnixMilli()); err != nil {
			return "", "", time.Time{}, err
		}
	} else {
		if err := s.st.InsertRefreshToken(ctx, next); err != nil {
			return "", "", time.Time{}, err
		}
	}
	if next.AuthTime == 0 {
		next.AuthTime = now.UnixMilli()
	}
	s.audit.Emit(ctx, service.Event{
		Type: model.EventTokenIssued, UserID: request.GetSubject(), ClientID: clientID,
		Result: "success", Detail: "access_token+refresh_token",
	})
	return accessID, plain, now.Add(ttl), nil
}

func (s *Storage) TokenRequestByRefreshToken(ctx context.Context, refreshToken string) (op.RefreshTokenRequest, error) {
	rt, err := s.st.GetRefreshTokenByHash(ctx, crypto.SHA256(refreshToken))
	if err != nil {
		return nil, ErrRefreshInvalid
	}
	now := time.Now().UnixMilli()
	if rt.Revoked {
		// 重放检测：吊销整条 token 链（规范 §9 第 4 条）。
		_ = s.st.RevokeFamily(ctx, rt.FamilyID, now)
		s.audit.Emit(ctx, service.Event{
			Type: model.EventRefreshReused, UserID: rt.UserID, ClientID: rt.ClientID,
			Result: "failure", Detail: "refresh token reuse detected, family revoked",
		})
		return nil, ErrRefreshInvalid
	}
	if rt.ExpiresAt <= now {
		return nil, ErrRefreshInvalid
	}
	return &refreshTokenRequest{
		TokenID:   rt.ID,
		FamilyID:  rt.FamilyID,
		ClientID:  rt.ClientID,
		Subject:   rt.UserID,
		Scopes:    strings.Fields(rt.Scope),
		AMR:       rt.AMR,
		Audience:  []string{rt.ClientID},
		AuthTime:  time.UnixMilli(rt.AuthTime),
		ExpiresAt: time.UnixMilli(rt.ExpiresAt),
	}, nil
}

func (s *Storage) TerminateSession(ctx context.Context, userID, clientID string) error {
	now := time.Now().UnixMilli()
	if err := s.st.RevokeRefreshTokensByUser(ctx, userID, clientID, now); err != nil {
		return err
	}
	if clientID == "" {
		return s.st.DeleteSessionsByUser(ctx, userID)
	}
	return nil
}

func (s *Storage) RevokeToken(ctx context.Context, tokenOrTokenID string, userID string, clientID string) *oidc.Error {
	now := time.Now().UnixMilli()
	if userID != "" {
		// JWT access token 无状态，无法按 jti 回收；按 RFC 7009 返回成功即可。
		return nil
	}
	_ = s.st.RevokeRefreshTokenByHash(ctx, crypto.SHA256(tokenOrTokenID), now)
	return nil
}

func (s *Storage) GetRefreshTokenInfo(ctx context.Context, clientID string, token string) (string, string, error) {
	rt, err := s.st.GetRefreshTokenByHash(ctx, crypto.SHA256(token))
	if err != nil {
		return "", "", op.ErrInvalidRefreshToken
	}
	return rt.UserID, rt.ID, nil
}

// ---------------- 签名与健康检查 ----------------

func (s *Storage) SigningKey(ctx context.Context) (op.SigningKey, error) { return s.keys.sign, nil }

func (s *Storage) SignatureAlgorithms(ctx context.Context) ([]jose.SignatureAlgorithm, error) {
	return []jose.SignatureAlgorithm{jose.RS256}, nil
}

func (s *Storage) KeySet(ctx context.Context) ([]op.Key, error) {
	return []op.Key{s.keys.pub}, nil
}

// Health 必须廉价：不开启事务、不获取锁（规范 §6.4）。
func (s *Storage) Health(ctx context.Context) error { return s.st.Ping(ctx) }

// ---------------- 登录闭环（非接口方法） ----------------

// CompleteLogin 标记待认证请求已完成，并回写 subject / auth_time / amr。
func (s *Storage) CompleteLogin(ctx context.Context, authRequestID, subject string, authTime time.Time, amr []string) error {
	v, ok := s.pending.Load(authRequestID)
	if !ok {
		return ErrAuthRequestInvalid
	}
	p := v.(*pendingRequest)
	if time.Now().After(p.expiresAt) {
		s.pending.Delete(authRequestID)
		return ErrAuthRequestInvalid
	}
	p.req.Subject = subject
	p.req.AuthTime = authTime
	p.req.AMR = amr
	p.req.done = true
	return nil
}

// PendingRequest 供登录页展示客户端名称。
func (s *Storage) PendingRequest(id string) (*authRequest, bool) {
	v, ok := s.pending.Load(id)
	if !ok {
		return nil, false
	}
	return v.(*pendingRequest).req, true
}

var (
	_ op.Storage                   = (*Storage)(nil)
	_ op.CanSetUserinfoFromRequest = (*Storage)(nil)
)

package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/omino/omino-sso/internal/config"
	"github.com/omino/omino-sso/internal/pkg/clock"
	"github.com/omino/omino-sso/internal/pkg/crypto"
	"github.com/omino/omino-sso/internal/pkg/id"
	"github.com/omino/omino-sso/internal/store"
	"github.com/omino/omino-sso/internal/store/model"
)

// 认证相关哨兵错误，由 API 层映射为错误码。
var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrAccountLocked      = errors.New("account locked")
	ErrAccountDisabled    = errors.New("account disabled")
	ErrUsernameExists     = errors.New("username already exists")
	ErrEmailExists        = errors.New("email already exists")
	ErrWeakPassword       = errors.New("weak password")
	ErrDeptNotFound       = errors.New("department not found")
	ErrSamePassword       = errors.New("new password equals old password")
)

var dummyHash string

// UserService 用户与认证。
type UserService struct {
	st    *store.Store
	cfg   *config.Config
	audit *Auditor
	log   *slog.Logger
}

// NewUserService 构造。
func NewUserService(st *store.Store, cfg *config.Config, audit *Auditor, logger *slog.Logger) *UserService {
	if dummyHash == "" {
		h, err := crypto.HashPassword("dummy-password-for-timing", argonParams(cfg))
		if err == nil {
			dummyHash = h
		}
	}
	return &UserService{st: st, cfg: cfg, audit: audit, log: logger}
}

func argonParams(cfg *config.Config) crypto.Argon2Params {
	p := crypto.DefaultArgon2Params()
	if cfg != nil {
		if cfg.Argon2.MemoryKiB > 0 {
			p.Memory = cfg.Argon2.MemoryKiB
		}
		if cfg.Argon2.Iterations > 0 {
			p.Iterations = cfg.Argon2.Iterations
		}
		if cfg.Argon2.Parallelism > 0 {
			p.Parallelism = cfg.Argon2.Parallelism
		}
	}
	return p
}

// Meta 携带请求上下文（IP / UA）用于审计。
type Meta struct {
	IP        string
	UserAgent string
}

// Authenticate 校验用户名密码。
// 返回 (user, needMFA, error)。账号不存在时也执行一次哈希比较，避免时间侧信道。
func (s *UserService) Authenticate(ctx context.Context, username, password string, meta Meta) (*model.User, error) {
	u, err := s.st.GetUserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			_ = crypto.VerifyPassword(password, dummyHash)
			s.audit.Emit(ctx, Event{
				Type: model.EventLoginFailure, Username: username,
				IP: meta.IP, UserAgent: meta.UserAgent, Result: "failure",
				Detail: "user not found",
			})
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}

	now := nowMillis()
	if u.Status == model.UserStatusDisabled {
		s.audit.Emit(ctx, Event{
			Type: model.EventLoginFailure, UserID: u.ID, Username: u.Username,
			IP: meta.IP, UserAgent: meta.UserAgent, Result: "failure", Detail: "account disabled",
		})
		return nil, ErrAccountDisabled
	}
	if u.LockedUntil != nil && *u.LockedUntil > now {
		s.audit.Emit(ctx, Event{
			Type: model.EventLoginFailure, UserID: u.ID, Username: u.Username,
			IP: meta.IP, UserAgent: meta.UserAgent, Result: "failure", Detail: "account locked",
		})
		return nil, ErrAccountLocked
	}

	if err := crypto.VerifyPassword(password, u.PasswordHash); err != nil {
		lockUntil := now + s.cfg.LoginLockDuration.Milliseconds()
		if err := s.st.IncrFailedAttempts(ctx, u.ID, s.cfg.LoginMaxFailures, lockUntil); err != nil {
			s.log.Error("increase failed attempts", "err", err)
		}
		s.audit.Emit(ctx, Event{
			Type: model.EventLoginFailure, UserID: u.ID, Username: u.Username,
			IP: meta.IP, UserAgent: meta.UserAgent, Result: "failure", Detail: "invalid password",
		})
		if u.FailedAttempts+1 >= int32(s.cfg.LoginMaxFailures) {
			return nil, ErrAccountLocked
		}
		return nil, ErrInvalidCredentials
	}

	if err := s.st.ResetFailedAttempts(ctx, u.ID, now); err != nil {
		s.log.Error("reset failed attempts", "err", err)
	}
	return u, nil
}

// NeedMFA 判断是否需要二次校验。
func NeedMFA(u *model.User, clientMFARequired bool) bool {
	return u.MFAEnabled || u.MFARequired || clientMFARequired
}

// CreateUserInput 创建用户入参。
type CreateUserInput struct {
	Username    string
	DisplayName string
	Email       string
	Phone       string
	Password    string
	DeptID      string
	TenantID    string
	Roles       []string
	MFARequired bool
}

// ValidatePassword 最小强度：8–128，至少含字母与数字。
func ValidatePassword(pw string) bool {
	if len(pw) < 8 || len(pw) > 128 {
		return false
	}
	var hasLetter, hasDigit bool
	for _, r := range pw {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			hasLetter = true
		case r >= '0' && r <= '9':
			hasDigit = true
		}
	}
	return hasLetter && hasDigit
}

// CreateUser 创建用户。
func (s *UserService) CreateUser(ctx context.Context, in CreateUserInput) (*model.User, error) {
	if !ValidatePassword(in.Password) {
		return nil, ErrWeakPassword
	}
	if _, err := s.st.GetOrganization(ctx, in.DeptID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrDeptNotFound
		}
		return nil, err
	}
	if _, err := s.st.GetUserByUsername(ctx, in.Username); err == nil {
		return nil, ErrUsernameExists
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	hash, err := crypto.HashPassword(in.Password, argonParams(s.cfg))
	if err != nil {
		return nil, err
	}
	tenant := in.TenantID
	if tenant == "" {
		tenant = "default"
	}
	roles := in.Roles
	if len(roles) == 0 {
		roles = []string{"user"}
	}
	now := nowMillis()
	u := &model.User{
		ID:                id.New(),
		Username:          in.Username,
		DisplayName:       in.DisplayName,
		PasswordHash:      hash,
		DeptID:            in.DeptID,
		TenantID:          tenant,
		Roles:             roles,
		Status:            model.UserStatusActive,
		MFARequired:       in.MFARequired,
		PasswordUpdatedAt: now,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if in.Email != "" {
		u.Email = &in.Email
	}
	if in.Phone != "" {
		u.Phone = &in.Phone
	}
	if err := s.st.CreateUser(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}

// UpdateUserInput 更新用户入参（全部可选）。
type UpdateUserInput struct {
	DisplayName *string
	Email       *string
	Phone       *string
	DeptID      *string
	Status      *int8
	MFARequired *bool
	Roles       *[]string
}

// UpdateUser 局部更新。
func (s *UserService) UpdateUser(ctx context.Context, userID string, in UpdateUserInput) (*model.User, error) {
	if _, err := s.st.GetUserByID(ctx, userID); err != nil {
		return nil, err
	}
	fields := map[string]any{}
	if in.DisplayName != nil {
		fields["display_name"] = *in.DisplayName
	}
	if in.Email != nil {
		fields["email"] = *in.Email
	}
	if in.Phone != nil {
		fields["phone"] = *in.Phone
	}
	if in.DeptID != nil {
		fields["dept_id"] = *in.DeptID
	}
	if in.Status != nil {
		fields["status"] = *in.Status
	}
	if in.MFARequired != nil {
		fields["mfa_required"] = *in.MFARequired
	}
	if in.Roles != nil {
		fields["roles"] = model.StringList(*in.Roles)
	}
	if len(fields) == 0 {
		return s.st.GetUserByID(ctx, userID)
	}
	if err := s.st.UpdateUserFields(ctx, userID, fields); err != nil {
		return nil, err
	}
	return s.st.GetUserByID(ctx, userID)
}

// ChangePassword 校验旧密码并更新，随后吊销全部会话。
func (s *UserService) ChangePassword(ctx context.Context, userID, oldPwd, newPwd string) error {
	u, err := s.st.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}
	if err := crypto.VerifyPassword(oldPwd, u.PasswordHash); err != nil {
		return ErrInvalidCredentials
	}
	if oldPwd == newPwd {
		return ErrSamePassword
	}
	if !ValidatePassword(newPwd) {
		return ErrWeakPassword
	}
	hash, err := crypto.HashPassword(newPwd, argonParams(s.cfg))
	if err != nil {
		return err
	}
	now := clock.NowMillis()
	if err := s.st.UpdateUserFields(ctx, userID, map[string]any{
		"password_hash":       hash,
		"password_updated_at": now,
	}); err != nil {
		return err
	}
	return s.st.DeleteSessionsByUser(ctx, userID)
}

// DecryptMFASecret 解密 TOTP 密钥。
func (s *UserService) DecryptMFASecret(u *model.User) (string, error) {
	if len(u.MFASecret) == 0 {
		return "", errors.New("mfa secret is empty")
	}
	b, err := crypto.Decrypt(s.cfg.CryptoKey, u.MFASecret)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// NormalizeUsername 用户名去空格，保持大小写。
func NormalizeUsername(s string) string { return strings.TrimSpace(s) }

// GetByID 按 ID 查询用户。
func (s *UserService) GetByID(ctx context.Context, id string) (*model.User, error) {
	return s.st.GetUserByID(ctx, id)
}

// DeptPath 返回从根到当前部门的名称路径。
func (s *UserService) DeptPath(ctx context.Context, deptID string) ([]string, error) {
	if deptID == "" {
		return []string{}, nil
	}
	var names []string
	cur := deptID
	for i := 0; i < 20; i++ {
		o, err := s.st.GetOrganization(ctx, cur)
		if err != nil {
			break
		}
		names = append([]string{o.Name}, names...)
		if o.ParentID == nil || *o.ParentID == "" {
			break
		}
		cur = *o.ParentID
	}
	if names == nil {
		names = []string{}
	}
	return names, nil
}

// LockRemaining 返回锁定剩余时间，便于错误提示。
func LockRemaining(u *model.User) time.Duration {
	if u.LockedUntil == nil {
		return 0
	}
	d := time.Until(clock.FromMillis(*u.LockedUntil))
	if d < 0 {
		return 0
	}
	return d
}

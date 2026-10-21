package service

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/omino/omino-sso/internal/config"
	"github.com/omino/omino-sso/internal/pkg/clock"
	"github.com/omino/omino-sso/internal/pkg/crypto"
	"github.com/omino/omino-sso/internal/pkg/id"
	"github.com/omino/omino-sso/internal/service/notification"
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

	// 联系方式与验证码（005-verification-code-auth）。
	ErrTargetInUse    = errors.New("contact already in use")
	ErrTargetNotBound = errors.New("contact not bound")
	ErrInvalidTarget  = errors.New("invalid contact format")
	ErrCodeInvalid    = notification.ErrCodeInvalid
)

// 账号标识类型（FR-005：按输入格式自动识别）。
type IdentifierKind int

const (
	IdentifierUsername IdentifierKind = iota
	IdentifierEmail
	IdentifierPhone
)

var identifierPhonePattern = regexp.MustCompile(`^1\d{10}$`)

// DetectIdentifier 按正则识别账号标识：11 位手机号 / 含 @ 的邮箱 / 其余按用户名。
func DetectIdentifier(raw string) (IdentifierKind, string) {
	v := strings.TrimSpace(raw)
	if identifierPhonePattern.MatchString(v) {
		return IdentifierPhone, v
	}
	if strings.Contains(v, "@") {
		return IdentifierEmail, strings.ToLower(v)
	}
	return IdentifierUsername, v
}

var dummyHash string

// UserService 用户与认证。
type UserService struct {
	st    *store.Store
	cfg   *config.Config
	audit *Auditor
	log   *slog.Logger
	notif *notification.Service
}

// SetNotification 注入验证码服务（联系方式绑定 / 验证码登录 / 重置密码使用）。
// 独立 setter 而非构造参数，避免既有调用点与测试被迫改动。
func (s *UserService) SetNotification(n *notification.Service) {
	s.notif = n
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

// UserListFilter 管理端用户列表筛选条件。
// api 层只构造该结构并调用 service，不得直接持有 store（宪法原则 III）。
type UserListFilter struct {
	Keyword  string
	DeptID   string
	Status   *int8
	Page     int
	PageSize int
}

// List 分页查询用户，返回当页数据与总数。
func (s *UserService) List(ctx context.Context, f UserListFilter) ([]model.User, int64, error) {
	return s.st.ListUsers(ctx, store.UserFilter{
		Keyword:  f.Keyword,
		DeptID:   f.DeptID,
		Status:   f.Status,
		Page:     f.Page,
		PageSize: f.PageSize,
	})
}

// Meta 携带请求上下文（IP / UA）用于审计。
type Meta struct {
	IP        string
	UserAgent string
}

// Authenticate 校验用户名密码。
// 账号不存在时也执行一次哈希比较，避免时间侧信道。
func (s *UserService) Authenticate(ctx context.Context, username, password string, meta Meta) (*model.User, error) {
	return s.authenticate(ctx, username, func(ctx context.Context) (*model.User, error) {
		return s.st.GetUserByUsername(ctx, username)
	}, password, meta)
}

// AuthenticateByIdentifier 按标识符（用户名/邮箱/手机号）校验密码（FR-005）。
// 三种标识符各自命中唯一索引，识别逻辑由 DetectIdentifier 统一完成。
func (s *UserService) AuthenticateByIdentifier(ctx context.Context, identifier, password string, meta Meta) (*model.User, error) {
	kind, value := DetectIdentifier(identifier)
	return s.authenticate(ctx, value, func(ctx context.Context) (*model.User, error) {
		switch kind {
		case IdentifierPhone:
			return s.st.GetUserByPhone(ctx, value)
		case IdentifierEmail:
			return s.st.GetUserByEmail(ctx, value)
		default:
			return s.st.GetUserByUsername(ctx, value)
		}
	}, password, meta)
}

// authenticate 是密码登录的共用实现：lookup 负责按标识符取用户。
func (s *UserService) authenticate(
	ctx context.Context,
	label string,
	lookup func(context.Context) (*model.User, error),
	password string,
	meta Meta,
) (*model.User, error) {
	u, err := lookup(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			_ = crypto.VerifyPassword(password, dummyHash)
			s.audit.Emit(ctx, Event{
				Type: model.EventLoginFailure, Username: label,
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
// 参数校验与密码哈希在事务外完成；事务内先锁定目标部门行（与部门删除共用
// 同一临界区，避免 dept_id 指向已删除部门），再插入用户。
func (s *UserService) CreateUser(ctx context.Context, in CreateUserInput) (*model.User, error) {
	if !ValidatePassword(in.Password) {
		return nil, ErrWeakPassword
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
	if err := s.st.Transaction(ctx, func(ctx context.Context) error {
		if _, err := s.st.LockOrganization(ctx, in.DeptID); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrDeptNotFound
			}
			return err
		}
		return s.st.CreateUser(ctx, u)
	}); err != nil {
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
	// 提交了 deptId 时在短事务内锁定目标部门并校验存在（行为修正：过去静默写入，
	// 会与部门删除竞态产生悬空引用）。
	if err := s.st.Transaction(ctx, func(ctx context.Context) error {
		if in.DeptID != nil {
			if _, err := s.st.LockOrganization(ctx, *in.DeptID); err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return ErrDeptNotFound
				}
				return err
			}
		}
		return s.st.UpdateUserFields(ctx, userID, fields)
	}); err != nil {
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

// UserByContact 按渠道查询绑定该联系方式的用户。
func (s *UserService) UserByContact(ctx context.Context, channel, target string) (*model.User, error) {
	switch channel {
	case notification.ChannelSMS:
		return s.st.GetUserByPhone(ctx, target)
	case notification.ChannelEmail:
		return s.st.GetUserByEmail(ctx, target)
	default:
		return nil, gorm.ErrRecordNotFound
	}
}

// ContactOwner 返回占用该联系方式的用户；未被占用时返回 (nil, nil)。
// 供 API 层判断占用与存在性，避免 api 直接依赖 gorm。
func (s *UserService) ContactOwner(ctx context.Context, channel, target string) (*model.User, error) {
	u, err := s.UserByContact(ctx, channel, target)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}

// BindPhone 绑定/修改手机号：验证码验证归属 + 唯一性校验（FR-001/003/004）。
// 返回脱敏后的手机号。
func (s *UserService) BindPhone(ctx context.Context, userID, phone, code string, meta Meta) (string, error) {
	return s.bindContact(ctx, userID, notification.ChannelSMS, notification.SceneBindPhone, phone, code, meta)
}

// BindEmail 绑定/修改邮箱（FR-002/003/004）。返回脱敏后的邮箱。
func (s *UserService) BindEmail(ctx context.Context, userID, email, code string, meta Meta) (string, error) {
	return s.bindContact(ctx, userID, notification.ChannelEmail, notification.SceneBindEmail, email, code, meta)
}

func (s *UserService) bindContact(ctx context.Context, userID, channel, scene, target, code string, meta Meta) (string, error) {
	if s.notif == nil {
		return "", notification.ErrChannelDisabled
	}
	normalized, err := notification.NormalizeTarget(channel, target)
	if err != nil {
		return "", ErrInvalidTarget
	}
	// 唯一性校验放在验证码消费之前，冲突时不让用户白白消耗验证码。
	if u, err := s.UserByContact(ctx, channel, normalized); err == nil && u.ID != userID {
		return "", ErrTargetInUse
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", err
	}
	if err := s.notif.Verify(ctx, channel, scene, normalized, code); err != nil {
		return "", err
	}

	switch channel {
	case notification.ChannelSMS:
		err = s.st.UpdatePhone(ctx, userID, normalized)
	default:
		err = s.st.UpdateEmail(ctx, userID, normalized)
	}
	if err != nil {
		// 并发绑定同一联系方式时由唯一索引兜底。
		if errors.Is(err, store.ErrDuplicateKey) {
			return "", ErrTargetInUse
		}
		return "", err
	}

	eventType := model.EventPhoneBound
	if channel == notification.ChannelEmail {
		eventType = model.EventEmailBound
	}
	masked := notification.MaskTarget(channel, normalized)
	username := ""
	if u, err := s.st.GetUserByID(ctx, userID); err == nil {
		username = u.Username
	}
	s.audit.Emit(ctx, Event{
		Type: eventType, UserID: userID, Username: username,
		IP: meta.IP, UserAgent: meta.UserAgent, Result: "success",
		Fields: map[string]any{"channel": channel, "target": masked},
	})
	return masked, nil
}

// AuthenticateByCode 验证码登录（FR-006/007）。验证码已证明渠道归属，调用方不得再要求 TOTP。
func (s *UserService) AuthenticateByCode(ctx context.Context, channel, target, code string, meta Meta) (*model.User, error) {
	if s.notif == nil {
		return nil, notification.ErrChannelDisabled
	}
	normalized, err := notification.NormalizeTarget(channel, target)
	if err != nil {
		return nil, ErrInvalidCredentials
	}
	if err := s.notif.Verify(ctx, channel, notification.SceneLogin, normalized, code); err != nil {
		s.audit.Emit(ctx, Event{
			Type: model.EventLoginFailure, Username: notification.MaskTarget(channel, normalized),
			IP: meta.IP, UserAgent: meta.UserAgent, Result: "failure", Detail: "verification code invalid",
		})
		return nil, err
	}
	u, err := s.UserByContact(ctx, channel, normalized)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			s.audit.Emit(ctx, Event{
				Type: model.EventLoginFailure, Username: notification.MaskTarget(channel, normalized),
				IP: meta.IP, UserAgent: meta.UserAgent, Result: "failure", Detail: "user not found",
			})
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}
	if u.Status == model.UserStatusDisabled {
		return nil, ErrAccountDisabled
	}
	now := nowMillis()
	if u.LockedUntil != nil && *u.LockedUntil > now {
		return nil, ErrAccountLocked
	}
	if err := s.st.ResetFailedAttempts(ctx, u.ID, now); err != nil {
		s.log.Error("reset failed attempts", "err", err)
	}
	return u, nil
}

// ResetPasswordByCode 未登录通过验证码重置密码（FR-009），成功后不自动登录（澄清 Q1）。
func (s *UserService) ResetPasswordByCode(ctx context.Context, channel, target, code, newPassword string, meta Meta) error {
	if s.notif == nil {
		return notification.ErrChannelDisabled
	}
	if !ValidatePassword(newPassword) {
		return ErrWeakPassword
	}
	normalized, err := notification.NormalizeTarget(channel, target)
	if err != nil {
		return ErrInvalidTarget
	}
	// 先消费验证码，目标不存在时统一返回验证码错误，避免泄露账号存在性。
	if err := s.notif.Verify(ctx, channel, notification.SceneFindPassword, normalized, code); err != nil {
		return err
	}
	u, err := s.UserByContact(ctx, channel, normalized)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrCodeInvalid
		}
		return err
	}
	if u.Status == model.UserStatusDisabled {
		return ErrAccountDisabled
	}
	if err := s.setPassword(ctx, u.ID, newPassword); err != nil {
		return err
	}
	if err := s.st.DeleteSessionsByUser(ctx, u.ID); err != nil {
		s.log.Error("revoke sessions after reset", "err", err, "user", u.ID)
	}
	s.auditPasswordReset(ctx, u, meta, "by_code_unauth")
	return nil
}

// ResetPasswordWithCode 已登录用户通过验证码重置密码（FR-010），保留当前会话、
// 吊销其他设备会话。
func (s *UserService) ResetPasswordWithCode(ctx context.Context, u *model.User, channel, code, newPassword, currentSID string, meta Meta) error {
	if s.notif == nil {
		return notification.ErrChannelDisabled
	}
	if !ValidatePassword(newPassword) {
		return ErrWeakPassword
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
	if err := s.notif.Verify(ctx, channel, notification.SceneFindPassword, target, code); err != nil {
		return err
	}
	if crypto.VerifyPassword(newPassword, u.PasswordHash) == nil {
		return ErrSamePassword
	}
	if err := s.setPassword(ctx, u.ID, newPassword); err != nil {
		return err
	}
	if currentSID != "" {
		if err := s.st.DeleteOtherSessions(ctx, u.ID, currentSID); err != nil {
			s.log.Error("revoke other sessions after reset", "err", err, "user", u.ID)
		}
	}
	s.auditPasswordReset(ctx, u, meta, "by_code_authed")
	return nil
}

// setPassword 更新密码哈希并解除锁定计数（不含会话吊销，由调用方决定范围）。
func (s *UserService) setPassword(ctx context.Context, userID, newPassword string) error {
	hash, err := crypto.HashPassword(newPassword, argonParams(s.cfg))
	if err != nil {
		return err
	}
	return s.st.UpdateUserFields(ctx, userID, map[string]any{
		"password_hash":       hash,
		"password_updated_at": clock.NowMillis(),
		"failed_attempts":     0,
		"locked_until":        nil,
		"status":              model.UserStatusActive,
	})
}

func (s *UserService) auditPasswordReset(ctx context.Context, u *model.User, meta Meta, scene string) {
	s.audit.Emit(ctx, Event{
		Type: model.EventPasswordReset, UserID: u.ID, Username: u.Username,
		IP: meta.IP, UserAgent: meta.UserAgent, Result: "success",
		Fields: map[string]any{"scene": scene},
	})
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

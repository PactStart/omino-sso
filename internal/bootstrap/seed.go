package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/omino/omino-sso/internal/config"
	"github.com/omino/omino-sso/internal/pkg/crypto"
	"github.com/omino/omino-sso/internal/pkg/id"
	"github.com/omino/omino-sso/internal/store"
	"github.com/omino/omino-sso/internal/store/model"
)

// DemoMFAHolderAccount 是预置的 MFA 演示账号（仅 dev/test）。
const DemoMFAHolderAccount = "lisi"

// Seed 在 users 表为空时写入种子数据。
// 生产环境（APP_ENV=prod）只创建管理员，不创建演示数据。
func Seed(ctx context.Context, cfg *config.Config, st *store.Store, logger *slog.Logger) error {
	n, err := st.CountUsers(ctx)
	if err != nil {
		return fmt.Errorf("count users: %w", err)
	}
	if n > 0 {
		return nil
	}
	// 整批种子数据在同一事务内写入，避免半成品（例如组织写入成功、用户失败）。
	return st.Transaction(ctx, func(ctx context.Context) error {
		return seedAll(ctx, cfg, st, logger)
	})
}

func seedAll(ctx context.Context, cfg *config.Config, st *store.Store, logger *slog.Logger) error {
	now := time.Now().UnixMilli()
	rootID := id.New()
	techID := id.New()
	platformID := id.New()

	orgs := []model.Organization{
		{ID: rootID, Name: "总公司", Path: "/" + rootID + "/", Sort: 1, TenantID: "default", CreatedAt: now},
		{ID: techID, ParentID: &rootID, Name: "技术中心", Path: "/" + rootID + "/" + techID + "/", Sort: 2, TenantID: "default", CreatedAt: now},
		{ID: platformID, ParentID: &techID, Name: "平台组", Path: "/" + rootID + "/" + techID + "/" + platformID + "/", Sort: 3, TenantID: "default", CreatedAt: now},
	}
	for i := range orgs {
		if err := st.DB(ctx).Create(&orgs[i]).Error; err != nil {
			return fmt.Errorf("seed organization: %w", err)
		}
	}

	params := crypto.DefaultArgon2Params()
	if cfg.Argon2.MemoryKiB > 0 {
		params.Memory = cfg.Argon2.MemoryKiB
	}
	if cfg.Argon2.Iterations > 0 {
		params.Iterations = cfg.Argon2.Iterations
	}
	if cfg.Argon2.Parallelism > 0 {
		params.Parallelism = cfg.Argon2.Parallelism
	}

	adminName := cfg.BootstrapUsername
	adminPwd := cfg.BootstrapPassword
	if adminName == "" || adminPwd == "" {
		return fmt.Errorf("ADMIN_BOOTSTRAP_USERNAME / ADMIN_BOOTSTRAP_PASSWORD are required for initial seeding")
	}
	if err := createUser(ctx, st, params, adminName, "系统管理员", adminPwd, adminName+"@example.com", rootID, []string{"user", "admin"}, nil, now); err != nil {
		return err
	}
	logger.Info("seed: admin created", "username", adminName)

	if cfg.AppEnv == "prod" {
		return seedClients(ctx, st, now, logger)
	}

	// 演示账号：普通用户
	if err := createUser(ctx, st, params, "zhangsan", "张三", "Demo@12345", "zhangsan@example.com", platformID, []string{"user"}, nil, now); err != nil {
		return err
	}
	// 演示账号：已绑定 MFA
	key, err := totp.Generate(totp.GenerateOpts{Issuer: "omino-sso", AccountName: DemoMFAHolderAccount})
	if err != nil {
		return fmt.Errorf("generate totp: %w", err)
	}
	enc, err := crypto.Encrypt(cfg.CryptoKey, []byte(key.Secret()))
	if err != nil {
		return fmt.Errorf("encrypt totp: %w", err)
	}
	if err := createUser(ctx, st, params, DemoMFAHolderAccount, "李四", "Demo@12345", "lisi@example.com", platformID, []string{"user"}, enc, now); err != nil {
		return err
	}
	logger.Info("seed: demo users created", "password", "Demo@12345", "mfa_account", DemoMFAHolderAccount, "mfa_secret", key.Secret())

	return seedClients(ctx, st, now, logger)
}

func createUser(ctx context.Context, st *store.Store, params crypto.Argon2Params,
	username, displayName, password, email, deptID string, roles []string, mfaSecret []byte, now int64) error {
	hash, err := crypto.HashPassword(password, params)
	if err != nil {
		return err
	}
	u := &model.User{
		ID:                id.New(),
		Username:          username,
		DisplayName:       displayName,
		Email:             &email,
		PasswordHash:      hash,
		DeptID:            deptID,
		TenantID:          "default",
		Roles:             roles,
		Status:            model.UserStatusActive,
		PasswordUpdatedAt: now,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if mfaSecret != nil {
		u.MFAEnabled = true
		u.MFASecret = mfaSecret
	}
	if err := st.CreateUser(ctx, u); err != nil {
		return fmt.Errorf("seed user %s: %w", username, err)
	}
	return nil
}

func ptr(s string) *string { return &s }

func seedClients(ctx context.Context, st *store.Store, now int64, logger *slog.Logger) error {
	portal := &model.Client{
		ClientID:        "portal",
		ClientName:      "统一门户",
		Description:     ptr("统一身份认证门户，管理个人信息、会话与授权"),
		LogoURL:         ptr("https://cdn.example.com/logo/portal.svg"),
		IsPublic:        true,
		RedirectURIs:    []string{"http://localhost:5173/callback"},
		PostLogoutURIs:  []string{"http://localhost:5173/"},
		GrantTypes:      []string{"authorization_code", "refresh_token"},
		Scopes:          []string{"openid", "profile", "email", "offline_access"},
		AccessTokenTTL:  900,
		RefreshTokenTTL: 2592000,
		TenantID:        "default",
		Status:          1,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	demo := &model.Client{
		ClientID:         "demo-app",
		ClientName:       "演示应用",
		Description:      ptr("用于验证 OIDC 接入流程的示例系统"),
		LogoURL:          ptr("https://cdn.example.com/logo/demo.svg"),
		ClientSecretHash: crypto.SHA256("demo-secret"),
		IsPublic:         false,
		RedirectURIs:     []string{"http://localhost:9090/callback"},
		PostLogoutURIs:   []string{"http://localhost:9090/"},
		GrantTypes:       []string{"authorization_code", "refresh_token"},
		Scopes:           []string{"openid", "profile", "email", "offline_access"},
		AccessTokenTTL:   900,
		RefreshTokenTTL:  2592000,
		TenantID:         "default",
		Status:           1,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	for _, c := range []*model.Client{portal, demo} {
		if err := st.CreateClient(ctx, c); err != nil {
			return fmt.Errorf("seed client %s: %w", c.ClientID, err)
		}
	}
	logger.Info("seed: clients created", "public", "portal", "confidential", "demo-app", "secret", "demo-secret")
	return nil
}

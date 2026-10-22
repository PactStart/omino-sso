package service_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
	"github.com/redis/go-redis/v9"

	"github.com/omino/omino-sso/internal/config"
	"github.com/omino/omino-sso/internal/pkg/crypto"
	"github.com/omino/omino-sso/internal/pkg/id"
	"github.com/omino/omino-sso/internal/service"
	"github.com/omino/omino-sso/internal/service/notification"
	"github.com/omino/omino-sso/internal/store"
	"github.com/omino/omino-sso/internal/store/model"
)

// 本文件覆盖 005 新增的业务规则（联系方式绑定 / 验证码登录 / 验证码重置密码 / MFA 绑定解绑）。
// 这些方法直接依赖 *store.Store 与 Redis，与 org_integration_test.go 同一套门控：
// 需同时设置 TEST_MYSQL_DSN 与 TEST_REDIS_ADDR，未设置时整组跳过，`make test` 不依赖外部基础设施。

const testRedisEnv = "TEST_REDIS_ADDR"

// testCryptoKey 是 32 字节测试密钥（MFA secret 加密用），不是真实凭据。
var testCryptoKey = func() [32]byte {
	var k [32]byte
	copy(k[:], []byte("0123456789abcdef0123456789abcdef"))
	return k
}()

const (
	testOldPass   = "Old!pass2026"
	testNewPass   = "New!pass2026"
	testNewPass2  = "Newer!pass2026"
	testMetaIP    = "127.0.0.1"
	testMetaAgent = "go-test"
)

type verifyEnv struct {
	st       *store.Store
	users    *service.UserService
	mfa      *service.MFAService
	sessions *service.SessionService
	notif    *notification.Service
}

func newVerifyEnv(t *testing.T) *verifyEnv {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv(testDSNEnv))
	redisAddr := strings.TrimSpace(os.Getenv(testRedisEnv))
	if dsn == "" || redisAddr == "" {
		t.Skip("未设置 " + testDSNEnv + " / " + testRedisEnv + "，跳过 005 业务集成测试")
	}

	db := openTestDB(t, dsn)
	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		t.Fatalf("连接 Redis 失败: %v", err)
	}
	t.Cleanup(func() { _ = rdb.Close() })

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	st := store.New(db, rdb)
	cfg := &config.Config{
		CryptoKey:          testCryptoKey,
		SessionTTL:         time.Hour,
		SessionTTLRemember: 24 * time.Hour,
		LoginMaxFailures:   5,
		LoginLockDuration:  15 * time.Minute,
	}
	cfg.SMS.Provider = "mock"
	cfg.Mail.Provider = "mock"
	cfg.Verify = config.VerifyConfig{
		CodeTTL:             5 * time.Minute,
		ResendInterval:      60 * time.Second,
		MaxAttempts:         5,
		RecordRetentionDays: 180,
	}

	audit := service.NewAuditor(st, 32, logger)
	notif, err := notification.New(cfg, st, audit, logger)
	if err != nil {
		t.Fatalf("构造验证码服务失败: %v", err)
	}
	users := service.NewUserService(st, cfg, audit, logger)
	users.SetNotification(notif)
	return &verifyEnv{
		st:       st,
		users:    users,
		mfa:      service.NewMFAService(st, cfg, notif, audit, logger),
		sessions: service.NewSessionService(st, cfg),
		notif:    notif,
	}
}

// ---- 测试数据构造 ----

// uniquePhone 生成每次调用唯一的 11 位手机号，避免命中 uk_users_phone。
func uniquePhone(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("1%010d", time.Now().UnixNano()%10_000_000_000)
}

func uniqueEmail(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("u%d@example.com", time.Now().UnixNano())
}

func uniqueName(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("u%d", time.Now().UnixNano())
}

// seedUser 直接落库构造账号：绕过 CreateUser 的组织锁校验，密码哈希与生产一致（argon2id）。
func (e *verifyEnv) seedUser(t *testing.T, phone, email *string, password string) *model.User {
	t.Helper()
	hash, err := crypto.HashPassword(password, crypto.DefaultArgon2Params())
	if err != nil {
		t.Fatalf("哈希密码失败: %v", err)
	}
	now := time.Now().UnixMilli()
	u := &model.User{
		ID:                id.New(),
		Username:          uniqueName(t),
		DisplayName:       "测试账号",
		Phone:             phone,
		Email:             email,
		PasswordHash:      hash,
		TenantID:          "default",
		Roles:             []string{"user"},
		Status:            model.UserStatusActive,
		PasswordUpdatedAt: now,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := e.st.CreateUser(context.Background(), u); err != nil {
		t.Fatalf("插入测试账号失败: %v", err)
	}
	return u
}

// issue 签发验证码，先清掉重发间隔键：同一用例内需要多次向同一目标发码。
func (e *verifyEnv) issue(t *testing.T, scene, channel, target string, userID string) {
	t.Helper()
	ctx := context.Background()
	if err := e.st.Redis().Del(ctx, "verify:resend:"+scene+":"+channel+":"+target).Err(); err != nil {
		t.Fatalf("清理重发间隔键失败: %v", err)
	}
	if _, err := e.notif.Issue(ctx, notification.IssueInput{
		Scene: scene, Channel: channel, Target: target, UserID: userID,
	}); err != nil {
		t.Fatalf("签发验证码失败: %v", err)
	}
}

func testMeta() service.Meta {
	return service.Meta{IP: testMetaIP, UserAgent: testMetaAgent}
}

// ---- US1：绑定 / 修改联系方式 ----

func TestBindContactIntegration(t *testing.T) {
	env := newVerifyEnv(t)
	ctx := context.Background()
	u := env.seedUser(t, nil, nil, testOldPass)
	phone := uniquePhone(t)

	env.issue(t, notification.SceneBindPhone, notification.ChannelSMS, phone, u.ID)
	masked, err := env.users.BindPhone(ctx, u.ID, phone, notification.MockCode, testMeta())
	if err != nil {
		t.Fatalf("绑定手机号失败: %v", err)
	}
	if want := notification.MaskPhone(phone); masked != want {
		t.Fatalf("绑定响应 = %q，期望脱敏值 %q", masked, want)
	}
	stored, err := env.st.GetUserByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("读取账号失败: %v", err)
	}
	if stored.Phone == nil || *stored.Phone != phone {
		t.Fatalf("库内手机号 = %v，期望 %q", stored.Phone, phone)
	}

	// 唯一性：另一账号重复绑定同一手机号必须被拒绝（预检查路径）。
	other := env.seedUser(t, nil, nil, testOldPass)
	if _, err := env.users.BindPhone(ctx, other.ID, phone, notification.MockCode, testMeta()); !errors.Is(err, service.ErrTargetInUse) {
		t.Fatalf("重复手机号应返回 ErrTargetInUse，实际 %v", err)
	}

	// 验证码错误：不得写入联系方式。
	phone2 := uniquePhone(t)
	env.issue(t, notification.SceneBindPhone, notification.ChannelSMS, phone2, u.ID)
	if _, err := env.users.BindPhone(ctx, u.ID, phone2, "000000", testMeta()); !errors.Is(err, notification.ErrCodeInvalid) {
		t.Fatalf("错误验证码应返回 ErrCodeInvalid，实际 %v", err)
	}
	stored, err = env.st.GetUserByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("读取账号失败: %v", err)
	}
	if stored.Phone == nil || *stored.Phone != phone {
		t.Fatalf("验证码错误后手机号不应变化，实际 %v", stored.Phone)
	}

	// 邮箱：绑定成功后服务端脱敏返回。
	email := uniqueEmail(t)
	env.issue(t, notification.SceneBindEmail, notification.ChannelEmail, email, u.ID)
	maskedEmail, err := env.users.BindEmail(ctx, u.ID, email, notification.MockCode, testMeta())
	if err != nil {
		t.Fatalf("绑定邮箱失败: %v", err)
	}
	if want := notification.MaskEmail(email); maskedEmail != want {
		t.Fatalf("邮箱绑定响应 = %q，期望 %q", maskedEmail, want)
	}
}

// ---- US2：验证码登录 ----

func TestAuthenticateByCodeIntegration(t *testing.T) {
	env := newVerifyEnv(t)
	ctx := context.Background()
	phone := uniquePhone(t)
	u := env.seedUser(t, &phone, nil, testOldPass)

	env.issue(t, notification.SceneLogin, notification.ChannelSMS, phone, "")
	got, err := env.users.AuthenticateByCode(ctx, notification.ChannelSMS, phone, notification.MockCode, testMeta())
	if err != nil {
		t.Fatalf("验证码登录失败: %v", err)
	}
	if got.ID != u.ID {
		t.Fatalf("验证码登录返回 %q，期望 %q", got.ID, u.ID)
	}

	// 连续 5 次错误后该验证码立即失效（FR-018），随后输入正确码也不通过。
	phone2 := uniquePhone(t)
	env.seedUser(t, &phone2, nil, testOldPass)
	env.issue(t, notification.SceneLogin, notification.ChannelSMS, phone2, "")
	for i := 1; i <= 5; i++ {
		if _, err := env.users.AuthenticateByCode(ctx, notification.ChannelSMS, phone2, "000000", testMeta()); !errors.Is(err, notification.ErrCodeInvalid) {
			t.Fatalf("第 %d 次错误应返回 ErrCodeInvalid，实际 %v", i, err)
		}
	}
	if _, err := env.users.AuthenticateByCode(ctx, notification.ChannelSMS, phone2, notification.MockCode, testMeta()); !errors.Is(err, notification.ErrCodeInvalid) {
		t.Fatalf("超限后正确验证码也应失效，实际 %v", err)
	}

	// 未绑定任何账号的号码：统一返回凭证错误，不泄露账号存在性。
	orphan := uniquePhone(t)
	env.issue(t, notification.SceneLogin, notification.ChannelSMS, orphan, "")
	if _, err := env.users.AuthenticateByCode(ctx, notification.ChannelSMS, orphan, notification.MockCode, testMeta()); !errors.Is(err, service.ErrInvalidCredentials) {
		t.Fatalf("未绑定号码应返回 ErrInvalidCredentials，实际 %v", err)
	}
}

// ---- US3：找回与重置密码 ----

func TestResetPasswordByCodeIntegration(t *testing.T) {
	env := newVerifyEnv(t)
	ctx := context.Background()
	phone := uniquePhone(t)
	u := env.seedUser(t, &phone, nil, testOldPass)

	// 弱口令在消费验证码之前就被拒绝。
	env.issue(t, notification.SceneFindPassword, notification.ChannelSMS, phone, "")
	if err := env.users.ResetPasswordByCode(ctx, notification.ChannelSMS, phone, notification.MockCode, "short", testMeta()); !errors.Is(err, service.ErrWeakPassword) {
		t.Fatalf("弱口令应返回 ErrWeakPassword，实际 %v", err)
	}

	// 重置成功后旧密码失效、新密码可用；且不自动登录（不产生会话）。
	env.issue(t, notification.SceneFindPassword, notification.ChannelSMS, phone, "")
	if err := env.users.ResetPasswordByCode(ctx, notification.ChannelSMS, phone, notification.MockCode, testNewPass, testMeta()); err != nil {
		t.Fatalf("重置密码失败: %v", err)
	}
	if _, err := env.users.Authenticate(ctx, u.Username, testNewPass, testMeta()); err != nil {
		t.Fatalf("新密码应可登录: %v", err)
	}
	sessions, err := env.sessions.List(ctx, u.ID)
	if err != nil {
		t.Fatalf("列出会话失败: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("重置密码不应自动登录，实际会话数 %d", len(sessions))
	}

	// 未绑定目标：与验证码错误同一错误，不泄露账号存在性。
	orphan := uniquePhone(t)
	env.issue(t, notification.SceneFindPassword, notification.ChannelSMS, orphan, "")
	if err := env.users.ResetPasswordByCode(ctx, notification.ChannelSMS, orphan, notification.MockCode, testNewPass, testMeta()); !errors.Is(err, notification.ErrCodeInvalid) {
		t.Fatalf("未绑定目标应返回验证码错误，实际 %v", err)
	}
}

func TestResetPasswordWithCodeIntegration(t *testing.T) {
	env := newVerifyEnv(t)
	ctx := context.Background()
	phone := uniquePhone(t)
	u := env.seedUser(t, &phone, nil, testOldPass)

	current, err := env.sessions.Create(ctx, u, []string{"pwd"}, false, testMetaIP, testMetaAgent)
	if err != nil {
		t.Fatalf("创建当前会话失败: %v", err)
	}
	if _, err := env.sessions.Create(ctx, u, []string{"pwd"}, false, "10.0.0.2", "other-device"); err != nil {
		t.Fatalf("创建其他设备会话失败: %v", err)
	}

	// 与原密码相同：拒绝且不改密。
	env.issue(t, notification.SceneFindPassword, notification.ChannelSMS, phone, u.ID)
	if err := env.users.ResetPasswordWithCode(ctx, u, notification.ChannelSMS, notification.MockCode, testOldPass, current.SID, testMeta()); !errors.Is(err, service.ErrSamePassword) {
		t.Fatalf("同密码应返回 ErrSamePassword，实际 %v", err)
	}

	// 成功路径：保留当前会话、吊销其他设备会话。
	env.issue(t, notification.SceneFindPassword, notification.ChannelSMS, phone, u.ID)
	if err := env.users.ResetPasswordWithCode(ctx, u, notification.ChannelSMS, notification.MockCode, testNewPass2, current.SID, testMeta()); err != nil {
		t.Fatalf("已登录验证码改密失败: %v", err)
	}
	sessions, err := env.sessions.List(ctx, u.ID)
	if err != nil {
		t.Fatalf("列出会话失败: %v", err)
	}
	if len(sessions) != 1 || sessions[0].SID != current.SID {
		t.Fatalf("应只保留当前会话，实际 %+v", sessions)
	}

	// 未绑定渠道：返回 ErrTargetNotBound。
	if err := env.users.ResetPasswordWithCode(ctx, u, notification.ChannelEmail, notification.MockCode, testNewPass2, current.SID, testMeta()); !errors.Is(err, service.ErrTargetNotBound) {
		t.Fatalf("未绑定邮箱应返回 ErrTargetNotBound，实际 %v", err)
	}
}

// ---- US4：MFA 绑定与解绑 ----

// otpauthSecret 从 otpauth:// URL 中取出 Base32 密钥，用于生成动态码。
func otpauthSecret(t *testing.T, raw string) string {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("解析 otpauth URL 失败: %v", err)
	}
	secret := parsed.Query().Get("secret")
	if secret == "" {
		t.Fatalf("otpauth URL 缺少 secret: %s", raw)
	}
	return secret
}

// wrongTOTP 返回一个必然与当前有效码不同的 6 位码。
func wrongTOTP(t *testing.T, secret string) string {
	t.Helper()
	valid, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("生成动态码失败: %v", err)
	}
	wrong := "000000"
	if wrong == valid {
		wrong = "111111"
	}
	return wrong
}

func TestMFABindAndUnbindIntegration(t *testing.T) {
	env := newVerifyEnv(t)
	ctx := context.Background()
	phone := uniquePhone(t)
	u := env.seedUser(t, &phone, nil, testOldPass)

	start, err := env.mfa.StartBind(ctx, u)
	if err != nil {
		t.Fatalf("启动绑定失败: %v", err)
	}
	if start.SetupTicket == "" || start.OtpauthURL == "" {
		t.Fatalf("启动绑定返回不完整: %+v", start)
	}
	secret := otpauthSecret(t, start.OtpauthURL)

	// 动态码错误：返回 ErrMFAInvalidCode，且不改变绑定状态。
	if err := env.mfa.ConfirmBind(ctx, u, start.SetupTicket, wrongTOTP(t, secret), testMeta()); !errors.Is(err, service.ErrMFAInvalidCode) {
		t.Fatalf("错误动态码应返回 ErrMFAInvalidCode，实际 %v", err)
	}
	stored, err := env.st.GetUserByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("读取账号失败: %v", err)
	}
	if stored.MFAEnabled || len(stored.MFASecret) != 0 {
		t.Fatal("动态码错误后不应改变绑定状态")
	}

	// setupTicket 一次性：已消费的票据再次使用按过期处理。
	if err := env.mfa.ConfirmBind(ctx, u, start.SetupTicket, notification.MockCode, testMeta()); !errors.Is(err, service.ErrMFASetupTicketInvalid) {
		t.Fatalf("已消费票据应返回 ErrMFASetupTicketInvalid，实际 %v", err)
	}

	// 重新绑定并用有效动态码确认。
	start2, err := env.mfa.StartBind(ctx, u)
	if err != nil {
		t.Fatalf("二次启动绑定失败: %v", err)
	}
	secret2 := otpauthSecret(t, start2.OtpauthURL)
	code, err := totp.GenerateCode(secret2, time.Now())
	if err != nil {
		t.Fatalf("生成动态码失败: %v", err)
	}
	if err := env.mfa.ConfirmBind(ctx, u, start2.SetupTicket, code, testMeta()); err != nil {
		t.Fatalf("确认绑定失败: %v", err)
	}
	stored, err = env.st.GetUserByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("读取账号失败: %v", err)
	}
	if !stored.MFAEnabled || len(stored.MFASecret) == 0 {
		t.Fatal("确认绑定后应置 mfa_enabled=true 并保存加密密钥")
	}
	if _, err := env.mfa.StartBind(ctx, stored); !errors.Is(err, service.ErrMFAAlreadyBound) {
		t.Fatalf("已绑定再次启动应返回 ErrMFAAlreadyBound，实际 %v", err)
	}

	// 解绑：未绑定渠道先拒绝，再用短信验证码解绑成功。
	if err := env.mfa.Unbind(ctx, stored, notification.ChannelEmail, notification.MockCode, testMeta()); !errors.Is(err, service.ErrTargetNotBound) {
		t.Fatalf("未绑定邮箱应返回 ErrTargetNotBound，实际 %v", err)
	}
	env.issue(t, notification.SceneMFAUnbind, notification.ChannelSMS, phone, u.ID)
	if err := env.mfa.Unbind(ctx, stored, notification.ChannelSMS, notification.MockCode, testMeta()); err != nil {
		t.Fatalf("解绑失败: %v", err)
	}
	stored, err = env.st.GetUserByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("读取账号失败: %v", err)
	}
	if stored.MFAEnabled || len(stored.MFASecret) != 0 {
		t.Fatal("解绑后应置 mfa_enabled=false 并清空密钥")
	}
	if err := env.mfa.Unbind(ctx, stored, notification.ChannelSMS, notification.MockCode, testMeta()); !errors.Is(err, service.ErrMFANotBound) {
		t.Fatalf("未绑定状态解绑应返回 ErrMFANotBound，实际 %v", err)
	}
}

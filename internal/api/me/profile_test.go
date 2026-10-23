package me

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/omino/omino-sso/internal/api/middleware"
	"github.com/omino/omino-sso/internal/config"
	"github.com/omino/omino-sso/internal/pkg/crypto"
	"github.com/omino/omino-sso/internal/pkg/id"
	"github.com/omino/omino-sso/internal/service"
	"github.com/omino/omino-sso/internal/service/notification"
	"github.com/omino/omino-sso/internal/store"
	"github.com/omino/omino-sso/internal/store/model"
)

// 007 FR-023 / plan D10 的会话失效三条路径，这里是**handler 层**的那一半：
//
//	改密（POST /me/password）      → 服务端全部会话失效 + 清掉带 Domain 的 sid cookie
//	重置（POST /me/password/reset）→ 保留当前会话、只吊销其他设备，**不清 cookie**
//
// 服务端会话的吊销语义由 internal/service/verification_integration_test.go 覆盖，
// cookie 属性的单元级回归由 internal/pkg/cookie/cookie_test.go 覆盖；
// 但"handler 到底有没有接上 clearSessionCookie"只能在这里钉住——历史上正是漏了这一行
// （手写 http.Cookie 丢了 Domain），在新拓扑下表现为"改完密码四个域都还登着"。
//
// 需要真实 MySQL + Redis：未设置 TEST_MYSQL_DSN / TEST_REDIS_ADDR 时整组跳过，
// `make test` 不依赖外部基础设施（`make test-integration` 会带上本包）。
const (
	profileTestDSNEnv   = "TEST_MYSQL_DSN"
	profileTestRedisEnv = "TEST_REDIS_ADDR"

	profileOldPass = "Old!pass2026"
	profileNewPass = "New!pass2026"
)

type profileEnv struct {
	st       *store.Store
	users    *service.UserService
	sessions *service.SessionService
	notif    *notification.Service
	cfg      *config.Config
	h        *Handler
}

func newProfileEnv(t *testing.T) *profileEnv {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv(profileTestDSNEnv))
	redisAddr := strings.TrimSpace(os.Getenv(profileTestRedisEnv))
	if dsn == "" || redisAddr == "" {
		t.Skip("未设置 " + profileTestDSNEnv + " / " + profileTestRedisEnv + "，跳过会话失效 handler 层用例")
	}

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: gormlogger.Discard})
	if err != nil {
		t.Fatalf("连接 MySQL 失败: %v", err)
	}
	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
	t.Cleanup(func() {
		_ = rdb.Close()
		if sqlDB, dbErr := db.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		t.Fatalf("连接 Redis 失败: %v", err)
	}

	cfg := &config.Config{
		CryptoKey:          func() [32]byte { var k [32]byte; copy(k[:], "0123456789abcdef0123456789abcdef"); return k }(),
		SessionTTL:         time.Hour,
		SessionTTLRemember: 24 * time.Hour,
		// 与部署模板一致：主域 cookie，写/清必须同一个值
		CookieDomain:   ".example.com",
		CookieSecure:   true,
		CookieSameSite: "Lax",
		Verify: config.VerifyConfig{
			CodeTTL:             5 * time.Minute,
			ResendInterval:      60 * time.Second,
			MaxAttempts:         5,
			RecordRetentionDays: 180,
		},
	}
	cfg.SMS.Provider = "mock"
	cfg.Mail.Provider = "mock"

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	st := store.New(db, rdb)
	audit := service.NewAuditor(st, 1, logger)
	notif, err := notification.New(cfg, st, audit, logger)
	if err != nil {
		t.Fatalf("构造验证码服务失败: %v", err)
	}
	users := service.NewUserService(st, cfg, audit, logger)
	users.SetNotification(notif)

	return &profileEnv{
		st:       st,
		users:    users,
		sessions: service.NewSessionService(st, cfg),
		notif:    notif,
		cfg:      cfg,
		// 只用得到 cfg / users / sessions / audit / log，其余依赖与本组端点无关
		h: &Handler{cfg: cfg, users: users, sessions: service.NewSessionService(st, cfg), audit: audit, log: logger},
	}
}

// seedUser 直接落库：密码哈希与生产一致（argon2id），并可选绑定手机号。
func (e *profileEnv) seedUser(t *testing.T, phone *string) *model.User {
	t.Helper()
	hash, err := crypto.HashPassword(profileOldPass, crypto.DefaultArgon2Params())
	if err != nil {
		t.Fatalf("哈希密码失败: %v", err)
	}
	now := time.Now().UnixMilli()
	u := &model.User{
		ID:                id.New(),
		Username:          fmt.Sprintf("it%d", time.Now().UnixNano()),
		DisplayName:       "会话失效用例",
		Phone:             phone,
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

func (e *profileEnv) newSession(t *testing.T, u *model.User) string {
	t.Helper()
	sess, err := e.sessions.Create(context.Background(), u, []string{"pwd"}, false, "127.0.0.1", "go-test")
	if err != nil {
		t.Fatalf("创建会话失败: %v", err)
	}
	return sess.SID
}

// issueCode 签发验证码，先清掉重发间隔键，保证同一用例内可重复签发。
func (e *profileEnv) issueCode(t *testing.T, scene, channel, target, userID string) {
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

// post 直接驱动 handler：用 middleware.CtxUser 模拟 Auth 中间件注入的当前用户，
// 避免为了一个 cookie 断言去组装整条 Redis 会话链路。
func (e *profileEnv) post(t *testing.T, fn gin.HandlerFunc, path, body string, u *model.User, sid string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	if sid != "" {
		c.Request.AddCookie(&http.Cookie{Name: middleware.SessionCookie, Value: sid})
	}
	if u != nil {
		c.Set(middleware.CtxUser, u)
		c.Request = c.Request.WithContext(service.WithActor(c.Request.Context(), u.ID, u.Username))
	}
	fn(c)
	return rec
}

func TestChangePasswordClearsSharedSessionCookie(t *testing.T) {
	env := newProfileEnv(t)
	ctx := context.Background()
	u := env.seedUser(t, nil)
	sid := env.newSession(t, u)

	// 原密码错误：不得清 cookie（否则等于"输错密码就把自己踢下线"）
	rec := env.post(t, env.h.ChangePassword, "/api/v1/me/password",
		`{"oldPassword":"Wrong!pass2026","newPassword":"`+profileNewPass+`"}`, u, sid)
	// AUTH_INVALID_CREDENTIALS 映射 401（见 ChangePassword 的 @Failure 注解）
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("原密码错误状态码 = %d，期望 401", rec.Code)
	}
	if got := rec.Header().Get("Set-Cookie"); got != "" {
		t.Fatalf("原密码错误时不应下发 Set-Cookie：%q", got)
	}
	if _, err := env.sessions.Get(ctx, sid); err != nil {
		t.Fatalf("原密码错误时当前会话不应失效: %v", err)
	}

	// 正常改密：200 + 清掉共享域 cookie + 该 sid 立即失效（同一 sid 再请求 /me 会 401）
	rec = env.post(t, env.h.ChangePassword, "/api/v1/me/password",
		`{"oldPassword":"`+profileOldPass+`","newPassword":"`+profileNewPass+`"}`, u, sid)
	if rec.Code != http.StatusOK {
		t.Fatalf("改密状态码 = %d，期望 200（body=%s）", rec.Code, rec.Body.String())
	}
	assertClearingCookie(t, rec)
	if _, err := env.sessions.Get(ctx, sid); !errors.Is(err, store.ErrSessionNotFound) {
		t.Fatalf("改密后同一 sid 应失效（ErrSessionNotFound），实际 %v", err)
	}
}

func TestResetPasswordKeepsCurrentSessionAndCookie(t *testing.T) {
	env := newProfileEnv(t)
	ctx := context.Background()
	phone := fmt.Sprintf("1%010d", time.Now().UnixNano()%10_000_000_000)
	u := env.seedUser(t, &phone)
	current := env.newSession(t, u)
	other := env.newSession(t, u)

	env.issueCode(t, notification.SceneFindPassword, notification.ChannelSMS, phone, u.ID)

	rec := env.post(t, env.h.ResetPassword, "/api/v1/me/password/reset",
		`{"channel":"sms","code":"`+notification.MockCode+`","newPassword":"`+profileNewPass+`"}`, u, current)
	if rec.Code != http.StatusOK {
		t.Fatalf("验证码重置状态码 = %d，期望 200（body=%s）", rec.Code, rec.Body.String())
	}

	// 契约是"保留当前会话、只吊销其他设备"：清 cookie 会把用户当场踢下线
	if got := rec.Header().Get("Set-Cookie"); got != "" {
		t.Fatalf("验证码重置不应下发任何 Set-Cookie（会误杀当前会话）：%q", got)
	}
	if _, err := env.sessions.Get(ctx, current); err != nil {
		t.Fatalf("验证码重置后当前会话应仍有效: %v", err)
	}
	if _, err := env.sessions.Get(ctx, other); !errors.Is(err, store.ErrSessionNotFound) {
		t.Fatalf("验证码重置后其他设备会话应失效，实际 %v", err)
	}
}

// assertClearingCookie 断言下发的是"带共享域、立即过期"的清除 cookie。
// Go 序列化会省略 Domain 前导点（配置 .example.com → 输出 example.com）。
func assertClearingCookie(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	raw := rec.Header().Get("Set-Cookie")
	if raw == "" {
		t.Fatal("改密后未下发 Set-Cookie：浏览器上的 sid 不会被清除")
	}
	if !strings.Contains(raw, "Domain=example.com") {
		t.Fatalf("Set-Cookie 缺少共享域 Domain=example.com（host-only 清不掉主域 cookie）：%q", raw)
	}
	if !strings.Contains(raw, "Max-Age=0") {
		t.Fatalf("Set-Cookie 缺少 Max-Age=0：%q", raw)
	}
	for _, ck := range rec.Result().Cookies() {
		if ck.Name != middleware.SessionCookie {
			continue
		}
		if ck.Value != "" || ck.MaxAge != -1 {
			t.Fatalf("清除用 cookie 应无值且 MaxAge=-1，实际 value=%q maxAge=%d", ck.Value, ck.MaxAge)
		}
		if !ck.HttpOnly || !ck.Secure || ck.SameSite != http.SameSiteLaxMode {
			t.Fatalf("cookie 属性漂移：HttpOnly=%v Secure=%v SameSite=%v", ck.HttpOnly, ck.Secure, ck.SameSite)
		}
		return
	}
	t.Fatalf("响应里没有 sid cookie：%q", raw)
}

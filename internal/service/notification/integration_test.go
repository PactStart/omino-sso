package notification

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/omino/omino-sso/internal/config"
	"github.com/omino/omino-sso/internal/store"
	"github.com/omino/omino-sso/internal/store/model"
)

// 验证码全链路集成测试（T046），由 TEST_MYSQL_DSN + TEST_REDIS_ADDR 双重门控：
// 未同时设置时跳过，`make test` 不依赖外部基础设施。
const (
	itDSNEnv   = "TEST_MYSQL_DSN"
	itRedisEnv = "TEST_REDIS_ADDR"
)

func newIntegrationService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv(itDSNEnv))
	redisAddr := strings.TrimSpace(os.Getenv(itRedisEnv))
	if dsn == "" || redisAddr == "" {
		t.Skip("未设置 TEST_MYSQL_DSN / TEST_REDIS_ADDR，跳过验证码集成测试")
	}

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: gormlogger.Discard})
	if err != nil {
		t.Fatalf("连接 MySQL 失败: %v", err)
	}
	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
	t.Cleanup(func() {
		_ = rdb.Close()
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		t.Fatalf("连接 Redis 失败: %v", err)
	}
	ensureSendRecordsTable(t, db)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	st := store.New(db, rdb)
	cfg := &config.Config{}
	cfg.SMS.Provider = "mock"
	cfg.Mail.Provider = "mock"
	cfg.Verify = config.VerifyConfig{
		CodeTTL:             5 * time.Minute,
		ResendInterval:      60 * time.Second,
		MaxAttempts:         5,
		RecordRetentionDays: 180,
	}
	svc, err := New(cfg, st, nil, logger)
	if err != nil {
		t.Fatalf("构造验证码服务失败: %v", err)
	}
	return svc, db
}

// ensureSendRecordsTable 只执行迁移中的 CREATE TABLE 段（ALTER users 与用例无关，
// 且重复执行会失败）。
func ensureSendRecordsTable(t *testing.T, db *gorm.DB) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "0005_verification_auth.up.sql"))
	if err != nil {
		t.Fatalf("读取迁移文件失败: %v", err)
	}
	ddl := string(raw)
	// 迁移文件里 CREATE 之前有 SET NAMES 与注释，且后面还有 ALTER users（重复执行会失败），
	// 因此只截取单条 CREATE TABLE 语句（gorm 默认不开 multiStatements）。
	start := strings.Index(ddl, "CREATE TABLE")
	if start < 0 {
		t.Fatal("迁移文件中未找到 CREATE TABLE send_records")
	}
	rest := ddl[start:]
	if end := strings.Index(rest, ";"); end >= 0 {
		rest = rest[:end+1]
	}
	if err := db.Exec(rest).Error; err != nil {
		t.Fatalf("创建 send_records 失败: %v", err)
	}
}

// uniquePhone 生成每次运行唯一的测试手机号（11 位，1 开头）。
func uniquePhone(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("1%010d", time.Now().UnixNano()%10_000_000_000)
}

func TestVerifyLifecycle(t *testing.T) {
	svc, db := newIntegrationService(t)
	ctx := context.Background()
	phone := uniquePhone(t)

	// 1) 签发：mock 固定 888888，写发送记录（provider=mock、target 脱敏）。
	res, err := svc.Issue(ctx, IssueInput{Scene: SceneLogin, Channel: ChannelSMS, Target: phone})
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	if res.ExpiresIn != 300 || res.ResendAfter != 60 {
		t.Fatalf("签发结果 = %+v，期望 300/60", res)
	}
	var rec model.SendRecord
	if err := db.Where("scene = ?", SceneLogin).Order("created_at DESC").Take(&rec).Error; err != nil {
		t.Fatalf("查询发送记录失败: %v", err)
	}
	if rec.Provider != model.SendProviderMock || rec.Result != model.SendResultSuccess {
		t.Fatalf("发送记录 = %+v，期望 provider=mock result=success", rec)
	}
	if rec.Target != MaskPhone(phone) {
		t.Fatalf("发送记录接收方 = %q，期望脱敏值 %q", rec.Target, MaskPhone(phone))
	}

	// 2) 60 秒内重发被拒。
	if _, err := svc.Issue(ctx, IssueInput{Scene: SceneLogin, Channel: ChannelSMS, Target: phone}); !errors.Is(err, ErrTooFrequent) {
		t.Fatalf("重发应返回 ErrTooFrequent，实际 %v", err)
	}

	// 3) 连续错误 5 次后验证码立即失效（即使随后输入正确也不再通过）。
	for i := 1; i <= 5; i++ {
		if err := svc.Verify(ctx, ChannelSMS, SceneLogin, phone, "000000"); !errors.Is(err, ErrCodeInvalid) {
			t.Fatalf("第 %d 次错误校验应返回 ErrCodeInvalid，实际 %v", i, err)
		}
	}
	if err := svc.Verify(ctx, ChannelSMS, SceneLogin, phone, MockCode); !errors.Is(err, ErrCodeInvalid) {
		t.Fatalf("超限后正确验证码也应失效，实际 %v", err)
	}

	// 4) 一次性消费：另起目标，校验成功后不可重复使用。
	phone2 := uniquePhone(t) + "1"
	phone2 = phone2[:11]
	if _, err := svc.Issue(ctx, IssueInput{Scene: SceneLogin, Channel: ChannelSMS, Target: phone2}); err != nil {
		t.Fatalf("第二次签发失败: %v", err)
	}
	if err := svc.Verify(ctx, ChannelSMS, SceneLogin, phone2, MockCode); err != nil {
		t.Fatalf("首次校验应成功: %v", err)
	}
	if err := svc.Verify(ctx, ChannelSMS, SceneLogin, phone2, MockCode); !errors.Is(err, ErrCodeInvalid) {
		t.Fatalf("重复使用应返回 ErrCodeInvalid，实际 %v", err)
	}

	// 5) 过期：直接删除 Redis key 模拟 TTL 到期。
	phone3 := (uniquePhone(t) + "2")[:11]
	if _, err := svc.Issue(ctx, IssueInput{Scene: SceneLogin, Channel: ChannelSMS, Target: phone3}); err != nil {
		t.Fatalf("第三次签发失败: %v", err)
	}
	if err := db.Exec("DELETE FROM send_records WHERE target = ?", MaskPhone(phone3)).Error; err != nil {
		t.Fatalf("清理记录失败: %v", err)
	}
	_ = svc.st.Redis().Del(ctx, codeKeyPrefix+SceneLogin+":"+ChannelSMS+":"+phone3).Err()
	if err := svc.Verify(ctx, ChannelSMS, SceneLogin, phone3, MockCode); !errors.Is(err, ErrCodeInvalid) {
		t.Fatalf("过期验证码应返回 ErrCodeInvalid，实际 %v", err)
	}

	// 清理本次产生的记录，避免污染开发库。
	if err := db.Exec("DELETE FROM send_records WHERE target IN ?",
		[]string{MaskPhone(phone), MaskPhone(phone2), MaskPhone(phone3)}).Error; err != nil {
		t.Fatalf("清理发送记录失败: %v", err)
	}
}

// faultyProvider 恒定失败的供应商，用于验证连续失败熔断（research 决策 11 / T066）。
type faultyProvider struct {
	calls int32
}

func (f *faultyProvider) Name() string { return model.SendProviderMock }

func (f *faultyProvider) Send(ctx context.Context, channel, target, code string) error {
	atomic.AddInt32(&f.calls, 1)
	return errors.New("provider down")
}

// TestCircuitBreaker 连续失败达到阈值后开闸，冷却期内请求直接失败且不再调用供应商。
func TestCircuitBreaker(t *testing.T) {
	svc, _ := newIntegrationService(t)
	ctx := context.Background()
	rdb := svc.st.Redis()

	svc.cbThreshold = 2
	svc.cbCooldown = time.Minute
	t.Cleanup(func() { svc.cbThreshold = 0 })

	countKey := circuitKeyPrefix + ChannelSMS
	openKey := countKey + circuitOpenFlag
	_ = rdb.Del(ctx, countKey, openKey).Err()
	t.Cleanup(func() { _ = rdb.Del(ctx, countKey, openKey).Err() })

	faulty := &faultyProvider{}
	svc.sms = faulty

	for i := 0; i < 2; i++ {
		target := fmt.Sprintf("1380000000%d", i)
		if _, err := svc.Issue(ctx, IssueInput{Scene: SceneLogin, Channel: ChannelSMS, Target: target}); !errors.Is(err, ErrSendFailed) {
			t.Fatalf("第 %d 次签发应返回 ErrSendFailed，实际 %v", i+1, err)
		}
	}
	if got := atomic.LoadInt32(&faulty.calls); got != 2 {
		t.Fatalf("前两次应各调用一次供应商，实际 %d", got)
	}
	if n, err := rdb.Exists(ctx, openKey).Result(); err != nil || n != 1 {
		t.Fatalf("连续失败达阈值后应写入开闸标记，实际 n=%d err=%v", n, err)
	}

	// 第三次：熔断期内直接失败，供应商不被调用，也不产生验证码与发送记录。
	if _, err := svc.Issue(ctx, IssueInput{Scene: SceneLogin, Channel: ChannelSMS, Target: "13800000009"}); !errors.Is(err, ErrSendFailed) {
		t.Fatalf("熔断期内应返回 ErrSendFailed，实际 %v", err)
	}
	if got := atomic.LoadInt32(&faulty.calls); got != 2 {
		t.Fatalf("熔断期内不应调用供应商，实际调用 %d 次", got)
	}

	// 冷却结束后恢复：删掉开闸标记（等价于 TTL 到期）后应重新尝试供应商。
	_ = rdb.Del(ctx, openKey).Err()
	svc.cbThreshold = 0 // 关闭熔断，验证请求重新回到供应商
	if _, err := svc.Issue(ctx, IssueInput{Scene: SceneLogin, Channel: ChannelSMS, Target: "13800000008"}); !errors.Is(err, ErrSendFailed) {
		t.Fatalf("冷却后应再次尝试发送并失败，实际 %v", err)
	}
	if got := atomic.LoadInt32(&faulty.calls); got != 3 {
		t.Fatalf("冷却结束后应重新调用供应商，实际调用 %d 次", got)
	}
}

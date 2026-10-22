package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Config 汇聚全部运行时配置（AGENTS.md §9）。
// 缺失必填项必须导致启动失败，禁止静默降级。
type Config struct {
	AppEnv   string
	LogLevel string

	HTTPIdpAddr  string
	HTTPAPIAddr  string
	ReadTimeout  time.Duration
	WriteTimeout time.Duration

	Issuer          string
	SigningKeyPath  string
	SigningKeyID    string
	CryptoKey       [32]byte
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
	AuthCodeTTL     time.Duration

	SessionTTL         time.Duration
	SessionTTLRemember time.Duration

	CookieDomain   string
	CookieSecure   bool
	CookieSameSite string

	DB    DBConfig
	Redis RedisConfig

	Argon2 Argon2Config
	OSS    OSSConfig
	SMS    SmsConfig
	Mail   MailConfig
	Verify VerifyConfig

	LoginMaxFailures     int
	LoginLockDuration    time.Duration
	RateLimitLoginPerMin int
	RateLimitAPIPerMin   int
	AuditAsyncBuffer     int

	BootstrapUsername string
	BootstrapPassword string
}

type DBConfig struct {
	Host            string
	Port            int
	User            string
	Password        string
	Name            string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

// DSN 生成 go-sql-driver/mysql 使用的连接串。
func (d DBConfig) DSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=false&loc=UTC&multiStatements=false",
		d.User, d.Password, d.Host, d.Port, d.Name)
}

type RedisConfig struct {
	Addr     string
	Password string
	DB       int
}

type Argon2Config struct {
	MemoryKiB   uint32
	Iterations  uint32
	Parallelism uint8
}

// SmsConfig 短信通道配置（005-verification-code-auth）。
// Provider 取值：""（未启用）/ mock（模拟发送，验证码固定 888888）/ chuanglan。
type SmsConfig struct {
	Provider          string
	ChuanglanURL      string
	ChuanglanAccount  string
	ChuanglanPassword string
	// ChuanglanTemplateID 模板报备 ID（创蓝 v2/tpl/send 必填），与 ChuanglanTemplate 二选一：
	// 填了 ID 走模板模式（templateId + templateParamJson），不再发送整段文案。
	ChuanglanTemplateID string
	ChuanglanTemplate   string // 内容模板文案（无 ID 时使用），支持 {code} / {s} 占位
}

// MailConfig 邮件通道配置。
// Provider 取值：""（未启用）/ mock（模拟发送，验证码固定 888888）/ smtp。
type MailConfig struct {
	Provider     string
	SMTPHost     string
	SMTPPort     int
	SMTPUsername string
	SMTPPassword string
	From         string
	TemplatePath string // html/template 模板文件路径
}

// VerifyConfig 验证码策略与发送记录保留期。
type VerifyConfig struct {
	CodeTTL             time.Duration
	ResendInterval      time.Duration
	MaxAttempts         int
	RecordRetentionDays int
	// CircuitThreshold / CircuitCooldown 控制供应商熔断（research 决策 11）：
	// 同一渠道连续失败达到阈值即打开熔断，冷却期内请求直接失败、不再调用供应商。
	// CircuitThreshold <= 0 表示关闭熔断。
	CircuitThreshold int
	CircuitCooldown  time.Duration
}

// OSSConfig 对象存储配置（provider 驱动，004-object-storage-upload）。
// Provider 为空表示未启用；为 aliyun_oss 时走 STS AssumeRole（经 ramossrole 角色）签发临时凭证。
// AccessKey/SecretKey 为调用 AssumeRole 的 RAM 用户长期凭证，RoleArn 必填。
type OSSConfig struct {
	Provider           string
	Endpoint           string
	Bucket             string
	AccessKey          string        // RAM 用户 AK（用于调用 STS AssumeRole）
	SecretKey          string        // RAM 用户 SK
	BasePath           string        // 对象键前缀（如 "sso"），空则不加前缀
	CustomDomain       string        // 公开访问域名，空则用 https://<bucket>.<endpoint>
	RoleArn            string        // 必填：被扮演角色的 ARN（acs:ram::<accountId>:role/ramossrole）
	RoleSessionName    string        // AssumeRole 会话名（可选，默认 omino-sso）
	StsEndpoint        string        // STS 端点（可选，默认 sts.aliyuncs.com）
	StsDurationSeconds int           // AssumeRole DurationSeconds（可选，默认 3600）
	CredentialTTL      time.Duration // 直传凭证（PostObject policy）有效期
}

// Load 从环境变量读取配置并校验。
// 若存在 .env 文件，其中未在进程环境变量中定义的键会被注入。
func Load() (*Config, error) {
	loadDotEnv(".env")

	c := &Config{
		AppEnv:               env("APP_ENV", "dev"),
		LogLevel:             env("LOG_LEVEL", "info"),
		HTTPIdpAddr:          env("HTTP_IDP_ADDR", ":8080"),
		HTTPAPIAddr:          env("HTTP_API_ADDR", ":8081"),
		ReadTimeout:          envDuration("HTTP_READ_TIMEOUT", 15*time.Second),
		WriteTimeout:         envDuration("HTTP_WRITE_TIMEOUT", 15*time.Second),
		Issuer:               env("OIDC_ISSUER", ""),
		SigningKeyPath:       env("OIDC_SIGNING_KEY_PATH", ""),
		SigningKeyID:         env("OIDC_SIGNING_KEY_ID", "2026-10"),
		AccessTokenTTL:       time.Duration(envInt("OIDC_ACCESS_TOKEN_TTL", 900)) * time.Second,
		RefreshTokenTTL:      time.Duration(envInt("OIDC_REFRESH_TOKEN_TTL", 2592000)) * time.Second,
		AuthCodeTTL:          time.Duration(envInt("OIDC_AUTH_CODE_TTL", 60)) * time.Second,
		SessionTTL:           time.Duration(envInt("SESSION_TTL", 28800)) * time.Second,
		SessionTTLRemember:   time.Duration(envInt("SESSION_TTL_REMEMBER", 604800)) * time.Second,
		CookieDomain:         env("COOKIE_DOMAIN", ""),
		CookieSecure:         envBool("COOKIE_SECURE", true),
		CookieSameSite:       env("COOKIE_SAME_SITE", "Lax"),
		LoginMaxFailures:     envInt("LOGIN_MAX_FAILURES", 5),
		LoginLockDuration:    time.Duration(envInt("LOGIN_LOCK_DURATION", 900)) * time.Second,
		RateLimitLoginPerMin: envInt("RATE_LIMIT_LOGIN_PER_MIN", 10),
		RateLimitAPIPerMin:   envInt("RATE_LIMIT_API_PER_MIN", 300),
		AuditAsyncBuffer:     envInt("AUDIT_ASYNC_BUFFER", 1024),
		BootstrapUsername:    env("ADMIN_BOOTSTRAP_USERNAME", ""),
		BootstrapPassword:    env("ADMIN_BOOTSTRAP_PASSWORD", ""),
		DB: DBConfig{
			Host:            env("DB_HOST", "127.0.0.1"),
			Port:            envInt("DB_PORT", 3306),
			User:            env("DB_USER", ""),
			Password:        env("DB_PASSWORD", ""),
			Name:            env("DB_NAME", "omino_sso"),
			MaxOpenConns:    envInt("DB_MAX_OPEN_CONNS", 25),
			MaxIdleConns:    envInt("DB_MAX_IDLE_CONNS", 10),
			ConnMaxLifetime: envDuration("DB_CONN_MAX_LIFETIME", 30*time.Minute),
		},
		Redis: RedisConfig{
			Addr:     env("REDIS_ADDR", "127.0.0.1:6379"),
			Password: env("REDIS_PASSWORD", ""),
			DB:       envInt("REDIS_DB", 0),
		},
		Argon2: Argon2Config{
			MemoryKiB:   uint32(envInt("ARGON2_MEMORY_KIB", 65536)),
			Iterations:  uint32(envInt("ARGON2_ITERATIONS", 3)),
			Parallelism: uint8(envInt("ARGON2_PARALLELISM", 2)),
		},
		OSS: OSSConfig{
			Provider:           env("OSS_PROVIDER", ""),
			Endpoint:           env("OSS_ENDPOINT", ""),
			Bucket:             env("OSS_BUCKET", ""),
			AccessKey:          env("OSS_ACCESS_KEY", ""),
			SecretKey:          env("OSS_SECRET_KEY", ""),
			BasePath:           env("OSS_BASE_PATH", ""),
			CustomDomain:       env("OSS_CUSTOM_DOMAIN", ""),
			RoleArn:            env("OSS_ROLE_ARN", ""),
			RoleSessionName:    env("OSS_ROLE_SESSION_NAME", "omino-sso"),
			StsEndpoint:        env("OSS_STS_ENDPOINT", "sts.aliyuncs.com"),
			StsDurationSeconds: envInt("OSS_STS_DURATION_SECONDS", 3600),
			CredentialTTL:      time.Duration(envInt("OSS_CREDENTIAL_TTL", 300)) * time.Second,
		},
		SMS: SmsConfig{
			Provider:            env("SMS_PROVIDER", "mock"),
			ChuanglanURL:        env("SMS_CHUANGLAN_URL", ""),
			ChuanglanAccount:    env("SMS_CHUANGLAN_ACCOUNT", ""),
			ChuanglanPassword:   env("SMS_CHUANGLAN_PASSWORD", ""),
			ChuanglanTemplateID: env("SMS_CHUANGLAN_TEMPLATE_ID", ""),
			ChuanglanTemplate:   env("SMS_CHUANGLAN_TEMPLATE", ""),
		},
		Mail: MailConfig{
			Provider:     env("MAIL_PROVIDER", "mock"),
			SMTPHost:     env("MAIL_SMTP_HOST", ""),
			SMTPPort:     envInt("MAIL_SMTP_PORT", 587),
			SMTPUsername: env("MAIL_SMTP_USERNAME", ""),
			SMTPPassword: env("MAIL_SMTP_PASSWORD", ""),
			From:         env("MAIL_FROM", ""),
			TemplatePath: env("MAIL_TEMPLATE_PATH", "./configs/verify-mail.tmpl"),
		},
		Verify: VerifyConfig{
			CodeTTL:             time.Duration(envInt("VERIFY_CODE_TTL", 300)) * time.Second,
			ResendInterval:      time.Duration(envInt("VERIFY_RESEND_INTERVAL", 60)) * time.Second,
			MaxAttempts:         envInt("VERIFY_MAX_ATTEMPTS", 5),
			RecordRetentionDays: envInt("VERIFY_RECORD_RETENTION_DAYS", 180),
			CircuitThreshold:    envInt("VERIFY_CIRCUIT_THRESHOLD", 5),
			CircuitCooldown:     time.Duration(envInt("VERIFY_CIRCUIT_COOLDOWN", 60)) * time.Second,
		},
	}

	raw := env("OIDC_CRYPTO_KEY", "")
	if raw != "" {
		b, err := decodeKey(raw)
		if err != nil {
			return nil, fmt.Errorf("OIDC_CRYPTO_KEY: %w", err)
		}
		c.CryptoKey = b
	}

	if err := c.validate(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Config) validate() error {
	var missing []string
	for name, v := range map[string]string{
		"OIDC_ISSUER":           c.Issuer,
		"OIDC_SIGNING_KEY_PATH": c.SigningKeyPath,
		"DB_USER":               c.DB.User,
		"DB_PASSWORD":           c.DB.Password,
		"REDIS_ADDR":            c.Redis.Addr,
	} {
		if strings.TrimSpace(v) == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required env: %s", strings.Join(missing, ", "))
	}
	if c.CryptoKey == ([32]byte{}) {
		return errors.New("OIDC_CRYPTO_KEY must be a 32-byte key (raw 32 chars or base64)")
	}
	if c.AuthCodeTTL > 60*time.Second {
		return errors.New("OIDC_AUTH_CODE_TTL must not exceed 60s")
	}
	if c.OSS.Provider != "" {
		if c.OSS.Provider != "aliyun_oss" {
			return fmt.Errorf("unsupported OSS_PROVIDER: %s", c.OSS.Provider)
		}
		var ossMissing []string
		for name, v := range map[string]string{
			"OSS_ACCESS_KEY": c.OSS.AccessKey,
			"OSS_SECRET_KEY": c.OSS.SecretKey,
			"OSS_ROLE_ARN":   c.OSS.RoleArn,
			"OSS_BUCKET":     c.OSS.Bucket,
			"OSS_ENDPOINT":   c.OSS.Endpoint,
		} {
			if strings.TrimSpace(v) == "" {
				ossMissing = append(ossMissing, name)
			}
		}
		if len(ossMissing) > 0 {
			return fmt.Errorf("missing required env for OSS: %s", strings.Join(ossMissing, ", "))
		}
		if !strings.HasPrefix(c.OSS.RoleArn, "acs:ram::") || !strings.Contains(c.OSS.RoleArn, ":role/") {
			return fmt.Errorf("OSS_ROLE_ARN must be like acs:ram::<accountId>:role/<roleName>")
		}
		if c.OSS.StsDurationSeconds < 900 || c.OSS.StsDurationSeconds > 43200 {
			return fmt.Errorf("OSS_STS_DURATION_SECONDS must be in [900, 43200]")
		}
	}
	if err := c.validateNotification(); err != nil {
		return err
	}
	if c.AppEnv == "prod" {
		if !strings.HasPrefix(c.Issuer, "https://") {
			return errors.New("OIDC_ISSUER must use https in prod")
		}
		if !c.CookieSecure {
			return errors.New("COOKIE_SECURE must be true in prod")
		}
	}
	return nil
}

// validateNotification 按 provider 校验短信/邮件通道的必填项，并校验验证码策略。
// 未配置（provider 为空）表示该通道关闭，不触发校验（与 OSS 一致）。
func (c *Config) validateNotification() error {
	switch c.SMS.Provider {
	case "", "mock":
	case "chuanglan":
		var missing []string
		for name, v := range map[string]string{
			"SMS_CHUANGLAN_URL":      c.SMS.ChuanglanURL,
			"SMS_CHUANGLAN_ACCOUNT":  c.SMS.ChuanglanAccount,
			"SMS_CHUANGLAN_PASSWORD": c.SMS.ChuanglanPassword,
		} {
			if strings.TrimSpace(v) == "" {
				missing = append(missing, name)
			}
		}
		// 模板 ID（v2/tpl/send 模板报备模式）与整段文案二选一，都不配则无从发送。
		if strings.TrimSpace(c.SMS.ChuanglanTemplateID) == "" && strings.TrimSpace(c.SMS.ChuanglanTemplate) == "" {
			missing = append(missing, "SMS_CHUANGLAN_TEMPLATE_ID or SMS_CHUANGLAN_TEMPLATE")
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			return fmt.Errorf("missing required env for SMS: %s", strings.Join(missing, ", "))
		}
	default:
		return fmt.Errorf("unsupported SMS_PROVIDER: %s", c.SMS.Provider)
	}

	switch c.Mail.Provider {
	case "", "mock":
	case "smtp":
		var missing []string
		for name, v := range map[string]string{
			"MAIL_SMTP_HOST":     c.Mail.SMTPHost,
			"MAIL_FROM":          c.Mail.From,
			"MAIL_TEMPLATE_PATH": c.Mail.TemplatePath,
		} {
			if strings.TrimSpace(v) == "" {
				missing = append(missing, name)
			}
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			return fmt.Errorf("missing required env for MAIL: %s", strings.Join(missing, ", "))
		}
		if c.Mail.SMTPPort <= 0 || c.Mail.SMTPPort > 65535 {
			return errors.New("MAIL_SMTP_PORT must be in [1, 65535]")
		}
		if c.Mail.SMTPUsername != "" && c.Mail.SMTPPassword == "" {
			return errors.New("MAIL_SMTP_PASSWORD is required when MAIL_SMTP_USERNAME is set")
		}
	default:
		return fmt.Errorf("unsupported MAIL_PROVIDER: %s", c.Mail.Provider)
	}

	// 生产环境禁止模拟发送（spec Edge Cases：禁止在生成环境误开启模拟发送）。
	if c.AppEnv == "prod" {
		if c.SMS.Provider == "mock" {
			return errors.New("SMS_PROVIDER must not be mock in prod")
		}
		if c.Mail.Provider == "mock" {
			return errors.New("MAIL_PROVIDER must not be mock in prod")
		}
	}

	// 零值表示未设置（测试构造的 Config 常见），跳过策略校验；Load 总会填充默认值。
	if c.Verify.CodeTTL < 0 || c.Verify.CodeTTL > 30*time.Minute {
		return errors.New("VERIFY_CODE_TTL must be in (0, 30m]")
	}
	if c.Verify.ResendInterval < 0 {
		return errors.New("VERIFY_RESEND_INTERVAL must not be negative")
	}
	if c.Verify.CodeTTL > 0 && c.Verify.ResendInterval >= c.Verify.CodeTTL {
		return errors.New("VERIFY_RESEND_INTERVAL must be less than VERIFY_CODE_TTL")
	}
	if c.Verify.MaxAttempts < 0 || c.Verify.MaxAttempts > 10 {
		return errors.New("VERIFY_MAX_ATTEMPTS must be in [1, 10]")
	}
	if c.Verify.CircuitThreshold < 0 || c.Verify.CircuitThreshold > 100 {
		return errors.New("VERIFY_CIRCUIT_THRESHOLD must be in [0, 100] (0 disables the breaker)")
	}
	if c.Verify.CircuitCooldown < 0 || c.Verify.CircuitCooldown > 10*time.Minute {
		return errors.New("VERIFY_CIRCUIT_COOLDOWN must be in [0, 10m]")
	}
	// 开了熔断却设成 0 冷却，会导致开闸后永久短路，直接判定为配置错误。
	if c.Verify.CircuitThreshold > 0 && c.Verify.CircuitCooldown <= 0 {
		return errors.New("VERIFY_CIRCUIT_COOLDOWN must be > 0 when VERIFY_CIRCUIT_THRESHOLD > 0")
	}
	if c.Verify.RecordRetentionDays < 0 {
		return errors.New("VERIFY_RECORD_RETENTION_DAYS must not be negative")
	}
	return nil
}

func decodeKey(raw string) ([32]byte, error) {
	var out [32]byte
	if len(raw) == 32 {
		copy(out[:], raw)
		return out, nil
	}
	b, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return out, errors.New("must be 32 raw chars or base64-encoded 32 bytes")
	}
	if len(b) != 32 {
		return out, fmt.Errorf("decoded length = %d, want 32", len(b))
	}
	copy(out[:], b)
	return out, nil
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func envBool(key string, def bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func envDuration(key string, def time.Duration) time.Duration {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

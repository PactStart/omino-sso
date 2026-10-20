package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
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
	Addr      string
	Password  string
	DB        int
	KeyPrefix string
}

type Argon2Config struct {
	MemoryKiB   uint32
	Iterations  uint32
	Parallelism uint8
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
			Addr:      env("REDIS_ADDR", "127.0.0.1:6379"),
			Password:  env("REDIS_PASSWORD", ""),
			DB:        envInt("REDIS_DB", 0),
			KeyPrefix: env("REDIS_KEY_PREFIX", "sso:"),
		},
		Argon2: Argon2Config{
			MemoryKiB:   uint32(envInt("ARGON2_MEMORY_KIB", 65536)),
			Iterations:  uint32(envInt("ARGON2_ITERATIONS", 3)),
			Parallelism: uint8(envInt("ARGON2_PARALLELISM", 2)),
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

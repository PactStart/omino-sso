package config

import (
	"strings"
	"testing"
	"time"
)

// validBaseConfig 构造满足非 OSS 必填项的基础配置，供 OSS 校验用例复用。
func validBaseConfig() *Config {
	return &Config{
		AppEnv:         "dev",
		Issuer:         "http://localhost:8080",
		SigningKeyPath: "/tmp/dev-signing-key.pem",
		CryptoKey:      [32]byte{1, 2, 3, 4, 5},
		AuthCodeTTL:    60 * time.Second,
		DB:             DBConfig{User: "root", Password: "x"},
		Redis:          RedisConfig{Addr: "127.0.0.1:6379"},
	}
}

// validOSS 返回一份完整合法的 aliyun_oss 配置。
func validOSS() OSSConfig {
	return OSSConfig{
		Provider:           "aliyun_oss",
		Endpoint:           "oss-cn-shenzhen.aliyuncs.com",
		Bucket:             "test-bucket",
		AccessKey:          "ak",
		SecretKey:          "sk",
		RoleArn:            "acs:ram::123456:role/ramossrole",
		RoleSessionName:    "omino-sso",
		StsEndpoint:        "sts.aliyuncs.com",
		StsDurationSeconds: 3600,
		CredentialTTL:      300 * time.Second,
	}
}

func TestValidateOSS_Valid(t *testing.T) {
	c := validBaseConfig()
	c.OSS = validOSS()
	if err := c.validate(); err != nil {
		t.Fatalf("合法 OSS 配置应通过校验，实际 %v", err)
	}
}

func TestValidateOSS_ProviderEmptySkipsCheck(t *testing.T) {
	// Provider 为空 = 未启用，不应触发 OSS 校验。
	c := validBaseConfig()
	c.OSS = OSSConfig{}
	if err := c.validate(); err != nil {
		t.Fatalf("Provider 为空应跳过 OSS 校验，实际 %v", err)
	}
}

func TestValidateOSS_UnsupportedProvider(t *testing.T) {
	c := validBaseConfig()
	c.OSS = validOSS()
	c.OSS.Provider = "s3"
	err := c.validate()
	if err == nil || !strings.Contains(err.Error(), "unsupported OSS_PROVIDER") {
		t.Fatalf("应报 unsupported OSS_PROVIDER，实际 %v", err)
	}
}

func TestValidateOSS_MissingCredentials(t *testing.T) {
	c := validBaseConfig()
	c.OSS = validOSS()
	c.OSS.AccessKey = ""
	c.OSS.SecretKey = ""
	err := c.validate()
	if err == nil || !strings.Contains(err.Error(), "missing required env for OSS") {
		t.Fatalf("应报 missing required env for OSS，实际 %v", err)
	}
	if !strings.Contains(err.Error(), "OSS_ACCESS_KEY") || !strings.Contains(err.Error(), "OSS_SECRET_KEY") {
		t.Fatalf("错误信息应包含缺失变量名，实际 %v", err)
	}
}

func TestValidateOSS_BadRoleArn(t *testing.T) {
	c := validBaseConfig()
	c.OSS = validOSS()
	c.OSS.RoleArn = "not-an-arn"
	err := c.validate()
	if err == nil || !strings.Contains(err.Error(), "OSS_ROLE_ARN must be like") {
		t.Fatalf("应报 OSS_ROLE_ARN 格式错误，实际 %v", err)
	}
}

func TestValidateOSS_StsDurationOutOfRange(t *testing.T) {
	c := validBaseConfig()
	c.OSS = validOSS()
	c.OSS.StsDurationSeconds = 100
	err := c.validate()
	if err == nil || !strings.Contains(err.Error(), "OSS_STS_DURATION_SECONDS must be in [900, 43200]") {
		t.Fatalf("应报 OSS_STS_DURATION_SECONDS 越界，实际 %v", err)
	}
}

func TestValidateNotification_MockAllowedInDev(t *testing.T) {
	c := validBaseConfig()
	c.SMS.Provider = "mock"
	c.Mail.Provider = "mock"
	c.Verify = VerifyConfig{
		CodeTTL:             5 * time.Minute,
		ResendInterval:      60 * time.Second,
		MaxAttempts:         5,
		RecordRetentionDays: 180,
	}
	if err := c.validate(); err != nil {
		t.Fatalf("dev 下 mock 通道应通过校验，实际 %v", err)
	}
}

func TestValidateNotification_ProdRejectsMock(t *testing.T) {
	c := validBaseConfig()
	c.AppEnv = "prod"
	c.Issuer = "https://sso.example.com"
	c.SMS.Provider = "mock"
	err := c.validate()
	if err == nil || !strings.Contains(err.Error(), "SMS_PROVIDER must not be mock in prod") {
		t.Fatalf("prod 下 mock 短信应被拒绝，实际 %v", err)
	}
}

func TestValidateNotification_SMSMissingFields(t *testing.T) {
	c := validBaseConfig()
	c.SMS.Provider = "chuanglan"
	c.SMS.ChuanglanURL = "https://smssh1.253.com/msg/v1/send/json"
	err := c.validate()
	if err == nil || !strings.Contains(err.Error(), "missing required env for SMS") {
		t.Fatalf("chuanglan 缺参数应校验失败，实际 %v", err)
	}
}

// TestValidateNotification_CircuitBreaker 熔断参数：阈值 > 0 时冷却必须 > 0，
// 否则开闸后永久短路（T066 / research 决策 11）。
func TestValidateNotification_CircuitBreaker(t *testing.T) {
	c := validBaseConfig()
	c.Verify.CircuitThreshold = 5
	c.Verify.CircuitCooldown = 0
	if err := c.validate(); err == nil || !strings.Contains(err.Error(), "VERIFY_CIRCUIT_COOLDOWN") {
		t.Fatalf("阈值 > 0 且冷却 = 0 应校验失败，实际 %v", err)
	}

	c.Verify.CircuitCooldown = time.Minute
	if err := c.validate(); err != nil {
		t.Fatalf("合法熔断配置不应报错，实际 %v", err)
	}

	c.Verify.CircuitThreshold = 0 // 0 表示关闭熔断
	c.Verify.CircuitCooldown = 0
	if err := c.validate(); err != nil {
		t.Fatalf("关闭熔断（阈值 0）不应报错，实际 %v", err)
	}
}

func TestValidateNotification_UnsupportedProvider(t *testing.T) {
	c := validBaseConfig()
	c.Mail.Provider = "sendgrid"
	err := c.validate()
	if err == nil || !strings.Contains(err.Error(), "unsupported MAIL_PROVIDER") {
		t.Fatalf("未知邮件 provider 应校验失败，实际 %v", err)
	}
}

func TestValidateNotification_SMTPMissingFields(t *testing.T) {
	c := validBaseConfig()
	c.Mail.Provider = "smtp"
	c.Mail.SMTPPort = 587
	err := c.validate()
	if err == nil || !strings.Contains(err.Error(), "missing required env for MAIL") {
		t.Fatalf("smtp 缺参数应校验失败，实际 %v", err)
	}
}

func TestValidateNotification_ResendIntervalMustBeLessThanTTL(t *testing.T) {
	c := validBaseConfig()
	c.Verify = VerifyConfig{
		CodeTTL:        time.Minute,
		ResendInterval: 2 * time.Minute,
		MaxAttempts:    5,
	}
	err := c.validate()
	if err == nil || !strings.Contains(err.Error(), "VERIFY_RESEND_INTERVAL") {
		t.Fatalf("重发间隔不小于有效期应校验失败，实际 %v", err)
	}
}
// ---------------------------------------------------------------------------
// 007-deployment：可信代理与 prod cookie 域校验（tasks.md T006）
// ---------------------------------------------------------------------------

// validProdConfig 在通用基线上补齐 prod 的三条硬要求（https issuer / Secure cookie / 主域 cookie）。
func validProdConfig() *Config {
	c := validBaseConfig()
	c.AppEnv = "prod"
	c.Issuer = "https://sso.example.com"
	c.CookieSecure = true
	c.CookieDomain = ".example.com"
	return c
}

func TestValidateProd_Valid(t *testing.T) {
	if err := validProdConfig().validate(); err != nil {
		t.Fatalf("合法 prod 配置应通过校验，实际 %v", err)
	}
}

// host-only cookie 会让「门户登录成功、进后台又要登录」，必须在启动期就拒绝。
func TestValidateProd_CookieDomain(t *testing.T) {
	tests := []struct {
		name   string
		domain string
		want   string // 期望错误里的关键字；空表示应通过
	}{
		{name: "留空即 host-only", domain: "", want: "COOKIE_DOMAIN is required in prod"},
		{name: "裸主机名", domain: "sso.example.com", want: "must be a parent domain starting with '.'"},
		{name: "带 scheme", domain: ".https://example.com", want: "must be a bare domain"},
		{name: "带尾斜杠", domain: ".example.com/", want: "must be a bare domain"},
		{name: "合法主域", domain: ".example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := validProdConfig()
			c.CookieDomain = tt.domain
			err := c.validate()
			if tt.want == "" {
				if err != nil {
					t.Fatalf("COOKIE_DOMAIN=%q 应通过，实际 %v", tt.domain, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("COOKIE_DOMAIN=%q 错误 = %v, want 含 %q", tt.domain, err, tt.want)
			}
		})
	}
}

func TestValidateProd_IssuerAndSecure(t *testing.T) {
	c := validProdConfig()
	c.Issuer = "http://sso.example.com"
	if err := c.validate(); err == nil || !strings.Contains(err.Error(), "OIDC_ISSUER must use https") {
		t.Fatalf("prod 下 http issuer 应失败，实际 %v", err)
	}

	c = validProdConfig()
	c.CookieSecure = false
	if err := c.validate(); err == nil || !strings.Contains(err.Error(), "COOKIE_SECURE must be true") {
		t.Fatalf("prod 下 Secure=false 应失败，实际 %v", err)
	}
}

func TestParseTrustedProxies(t *testing.T) {
	t.Run("空值即不信任任何代理", func(t *testing.T) {
		got, err := parseTrustedProxies("  ")
		if err != nil {
			t.Fatalf("空值不应报错，实际 %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("空值应得到空列表，实际 %v", got)
		}
	})

	t.Run("CIDR 与单 IP 混合", func(t *testing.T) {
		got, err := parseTrustedProxies("10.0.0.0/8, 172.18.0.0/16 ,192.168.1.10")
		if err != nil {
			t.Fatalf("合法输入不应报错，实际 %v", err)
		}
		want := []string{"10.0.0.0/8", "172.18.0.0/16", "192.168.1.10/32"}
		if len(got) != len(want) {
			t.Fatalf("解析出 %d 项，want %d（%v）", len(got), len(want), got)
		}
		for i := range want {
			if got[i].String() != want[i] {
				t.Fatalf("第 %d 项 = %q, want %q（单 IP 必须规范化为 /32）", i, got[i].String(), want[i])
			}
		}
	})

	// 非法项必须让启动失败，并打印**具体非法值**——静默忽略会让审计 IP 悄悄变错。
	t.Run("非法项报出具体值", func(t *testing.T) {
		_, err := parseTrustedProxies("10.0.0.0/8,not-a-cidr")
		if err == nil || !strings.Contains(err.Error(), `"not-a-cidr"`) {
			t.Fatalf("错误 = %v, want 含具体非法值 \"not-a-cidr\"", err)
		}
	})
}

func TestTrustedProxyCIDRs(t *testing.T) {
	if got := (&Config{}).TrustedProxyCIDRs(); got != nil {
		t.Fatalf("未配置时应返回 nil（交给 gin 表示不信任任何代理），实际 %v", got)
	}
	proxies, err := parseTrustedProxies("10.0.0.0/8,192.168.1.10")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	got := (&Config{TrustedProxies: proxies}).TrustedProxyCIDRs()
	want := []string{"10.0.0.0/8", "192.168.1.10/32"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

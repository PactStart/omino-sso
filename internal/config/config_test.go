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

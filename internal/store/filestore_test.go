package store

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/omino/omino-sso/internal/config"
)

func TestContentType(t *testing.T) {
	cases := map[string]string{
		"jpg":  "image/jpeg",
		"png":  "image/png",
		"webp": "image/webp",
		"gif":  "image/gif", // 未列出的格式回退 image/<format>
	}
	for format, want := range cases {
		if got := contentType(format); got != want {
			t.Fatalf("contentType(%q) = %q, 期望 %q", format, got, want)
		}
	}
}

func TestFullKey(t *testing.T) {
	f := &OSSFileStore{cfg: &config.OSSConfig{}}
	if got := f.fullKey("a/b.png"); got != "a/b.png" {
		t.Fatalf("无前缀应原样，实际 %q", got)
	}
	f.cfg.BasePath = "sso/"
	if got := f.fullKey("a/b.png"); got != "sso/a/b.png" {
		t.Fatalf("应有前缀，实际 %q", got)
	}
	f.cfg.BasePath = "/sso/"
	if got := f.fullKey("a/b.png"); got != "sso/a/b.png" {
		t.Fatalf("应去掉前后斜杠，实际 %q", got)
	}
}

func TestResolvePublicURL(t *testing.T) {
	f := &OSSFileStore{cfg: &config.OSSConfig{Bucket: "b", Endpoint: "oss.example.com"}}
	if got := f.ResolvePublicURL("k.png"); got != "https://b.oss.example.com/k.png" {
		t.Fatalf("无 CustomDomain 应回退 bucket.endpoint，实际 %q", got)
	}
	f.cfg.CustomDomain = "https://cdn.example.com/"
	if got := f.ResolvePublicURL("k.png"); got != "https://cdn.example.com/k.png" {
		t.Fatalf("CustomDomain 应优先，实际 %q", got)
	}
}

func TestNoopFileStore(t *testing.T) {
	n := NewNoopFileStore()
	if _, err := n.CreateUploadCredential(context.Background(), "k", "png", 1); !errors.Is(err, ErrObjectStorageNotConfigured) {
		t.Fatalf("应返回 ErrObjectStorageNotConfigured，实际 %v", err)
	}
	if err := n.DeleteObject(context.Background(), "k"); !errors.Is(err, ErrObjectStorageNotConfigured) {
		t.Fatalf("应返回 ErrObjectStorageNotConfigured，实际 %v", err)
	}
	if got := n.ResolvePublicURL("k"); got != "" {
		t.Fatalf("应返回空串，实际 %q", got)
	}
}

func TestRPCPercenEncode(t *testing.T) {
	if got := rpcPercentEncode("a b"); got != "a%20b" {
		t.Fatalf("空格应编码为 %%20，实际 %q", got)
	}
	if got := rpcPercentEncode("a/b"); got != "a%2Fb" {
		t.Fatalf("斜杠应编码，实际 %q", got)
	}
	if got := rpcPercentEncode("Abc-._~09"); got != "Abc-._~09" {
		t.Fatalf("非保留字符应原样，实际 %q", got)
	}
}

// TestCreateUploadCredential_PolicyAndSignature 用本地 httptest 模拟 STS AssumeRole，
// 以固定密钥断言直传凭证的 policy 约束与 HMAC-SHA1 签名正确性。
func TestCreateUploadCredential_PolicyAndSignature(t *testing.T) {
	const (
		mockAK  = "mock-access-key"
		mockSK  = "mock-secret-key"
		mockTok = "mock-security-token"
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Code":"","Credentials":{"AccessKeyId":"` + mockAK +
			`","AccessKeySecret":"` + mockSK + `","SecurityToken":"` + mockTok +
			`","Expiration":"2030-01-01T00:00:00Z"}}`))
	}))
	defer server.Close()

	cfg := &config.OSSConfig{
		Provider:           "aliyun_oss",
		Endpoint:           "oss-cn-shenzhen.aliyuncs.com",
		Bucket:             "test-bucket",
		AccessKey:          "long-term-ak",
		SecretKey:          "long-term-sk",
		BasePath:           "sso",
		RoleArn:            "acs:ram::123456:role/ramossrole",
		RoleSessionName:    "omino-sso",
		StsEndpoint:        server.URL,
		StsDurationSeconds: 3600,
		CredentialTTL:      300 * time.Second,
	}
	f, err := NewOSSFileStore(cfg)
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}

	cred, err := f.CreateUploadCredential(context.Background(), "app_logo/01J9.png", "png", 1024)
	if err != nil {
		t.Fatalf("签发凭证失败: %v", err)
	}

	if cred.Key != "sso/app_logo/01J9.png" {
		t.Fatalf("Key 应加 BasePath 前缀，实际 %q", cred.Key)
	}
	if cred.UploadURL != "https://test-bucket.oss-cn-shenzhen.aliyuncs.com" {
		t.Fatalf("UploadURL 错误，实际 %q", cred.UploadURL)
	}
	if cred.AccessKeyID != mockAK || cred.SecurityToken != mockTok {
		t.Fatalf("临时凭证应来自 STS，实际 ak=%q token=%q", cred.AccessKeyID, cred.SecurityToken)
	}

	// 解码 policy，校验 key / Content-Type / 大小 / 过期约束。
	raw, err := base64.StdEncoding.DecodeString(cred.Policy)
	if err != nil {
		t.Fatalf("policy 不是合法 base64: %v", err)
	}
	var policy postPolicy
	if err := json.Unmarshal(raw, &policy); err != nil {
		t.Fatalf("policy 不是合法 JSON: %v", err)
	}
	if policy.Expiration == "" {
		t.Fatal("policy 缺 expiration")
	}
	if len(policy.Conditions) != 3 {
		t.Fatalf("conditions 应有 3 项，实际 %d", len(policy.Conditions))
	}
	if len(policy.Conditions[0]) != 3 || policy.Conditions[0][0] != "eq" ||
		policy.Conditions[0][1] != "$key" || policy.Conditions[0][2] != "sso/app_logo/01J9.png" {
		t.Fatalf("key 约束错误: %v", policy.Conditions[0])
	}
	if len(policy.Conditions[1]) != 3 || policy.Conditions[1][0] != "eq" ||
		policy.Conditions[1][1] != "$Content-Type" || policy.Conditions[1][2] != "image/png" {
		t.Fatalf("Content-Type 约束错误: %v", policy.Conditions[1])
	}
	if len(policy.Conditions[2]) != 3 || policy.Conditions[2][0] != "content-length-range" {
		t.Fatalf("大小约束错误: %v", policy.Conditions[2])
	}

	// signature = base64(HMAC-SHA1(policyB64, secret))，用固定 secret 复核。
	mac := hmac.New(sha1.New, []byte(mockSK))
	mac.Write([]byte(cred.Policy))
	wantSig := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	if cred.Signature != wantSig {
		t.Fatalf("signature 应等于 HMAC-SHA1(policy, secret)，实际 %q 期望 %q", cred.Signature, wantSig)
	}
}

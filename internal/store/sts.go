package store

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/omino/omino-sso/internal/config"
)

// stsRefreshBuffer 临时凭证到期前提前多久触发续期（须小于最短 DurationSeconds=900s）。
const stsRefreshBuffer = 5 * time.Minute

// stsCredentials 阿里云 STS 临时安全凭证（AssumeRole 返回）。
type stsCredentials struct {
	AccessKeyID     string
	AccessKeySecret string
	SecurityToken   string
	Expiration      time.Time
}

// stsProvider 按需调用 AssumeRole 并缓存临时凭证，到期前自动续期，线程安全。
type stsProvider struct {
	endpoint        string
	accessKey       string
	secretKey       string
	roleArn         string
	roleSessionName string
	durationSeconds int

	mu     sync.Mutex
	cached *stsCredentials
}

// newSTSProvider 构造；endpoint 形如 sts.aliyuncs.com，无协议前缀时补 https://。
func newSTSProvider(cfg *config.OSSConfig) *stsProvider {
	endpoint := strings.TrimRight(cfg.StsEndpoint, "/")
	if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
		endpoint = "https://" + endpoint
	}
	return &stsProvider{
		endpoint:        endpoint,
		accessKey:       cfg.AccessKey,
		secretKey:       cfg.SecretKey,
		roleArn:         cfg.RoleArn,
		roleSessionName: cfg.RoleSessionName,
		durationSeconds: cfg.StsDurationSeconds,
	}
}

// Get 返回当前可用的临时凭证；剩余有效期不足时重新 AssumeRole。
func (p *stsProvider) Get(ctx context.Context) (*stsCredentials, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cached != nil && time.Until(p.cached.Expiration) > stsRefreshBuffer {
		return p.cached, nil
	}
	cred, err := assumeRole(ctx, p.endpoint, p.accessKey, p.secretKey, p.roleArn, p.roleSessionName, p.durationSeconds)
	if err != nil {
		return nil, err
	}
	p.cached = cred
	return cred, nil
}

// assumeRole 调用 STS AssumeRole（RPC 签名 V1 + GET）。
func assumeRole(ctx context.Context, endpoint, ak, sk, roleArn, sessionName string, durationSeconds int) (*stsCredentials, error) {
	params := map[string]string{
		"Action":           "AssumeRole",
		"Version":          "2015-04-01",
		"Format":           "JSON",
		"AccessKeyId":      ak,
		"SignatureMethod":  "HMAC-SHA1",
		"SignatureVersion": "1.0",
		"SignatureNonce":   newNonce(),
		"Timestamp":        time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		"RoleArn":          roleArn,
		"RoleSessionName":  sessionName,
	}
	if durationSeconds > 0 {
		params["DurationSeconds"] = strconv.Itoa(durationSeconds)
	}

	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, rpcPercentEncode(k)+"="+rpcPercentEncode(params[k]))
	}
	canonical := strings.Join(parts, "&")
	stringToSign := "GET&" + rpcPercentEncode("/") + "&" + rpcPercentEncode(canonical)

	mac := hmac.New(sha1.New, []byte(sk+"&"))
	mac.Write([]byte(stringToSign))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	reqURL := endpoint + "/?" + canonical + "&Signature=" + rpcPercentEncode(signature)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("sts assume_role: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sts assume_role: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("sts assume_role: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sts assume_role: http %d: %s", resp.StatusCode, body)
	}

	var out struct {
		Code        string `json:"Code"`
		Message     string `json:"Message"`
		Credentials struct {
			AccessKeyID     string `json:"AccessKeyId"`
			AccessKeySecret string `json:"AccessKeySecret"`
			SecurityToken   string `json:"SecurityToken"`
			Expiration      string `json:"Expiration"`
		} `json:"Credentials"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("sts assume_role: decode response: %w", err)
	}
	if out.Code != "" {
		return nil, fmt.Errorf("sts assume_role: %s: %s", out.Code, out.Message)
	}
	exp, err := time.Parse(time.RFC3339, out.Credentials.Expiration)
	if err != nil {
		return nil, fmt.Errorf("sts assume_role: parse expiration: %w", err)
	}
	return &stsCredentials{
		AccessKeyID:     out.Credentials.AccessKeyID,
		AccessKeySecret: out.Credentials.AccessKeySecret,
		SecurityToken:   out.Credentials.SecurityToken,
		Expiration:      exp,
	}, nil
}

// rpcPercentEncode 阿里云 RPC 签名 V1 的 percent-encode（RFC 3986，空格编码为 %20）。
func rpcPercentEncode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '.' || c == '_' || c == '~' {
			b.WriteByte(c)
		} else {
			b.WriteString(fmt.Sprintf("%%%02X", c))
		}
	}
	return b.String()
}

// newNonce 生成 32 位随机十六进制串作为 SignatureNonce。
func newNonce() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b[:])
}

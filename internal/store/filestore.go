package store

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"

	"github.com/omino/omino-sso/internal/config"
)

// ErrObjectStorageNotConfigured 对象存储未配置。
var ErrObjectStorageNotConfigured = errors.New("object storage not configured")

// UploadCredential 预签名直传凭证。
type UploadCredential struct {
	Key           string
	UploadURL     string
	AccessKeyID   string
	SecurityToken string // STS 临时凭证的 SecurityToken，客户端表单字段 x-oss-security-token
	Policy        string
	Signature     string
	ExpiresAt     int64 // Unix 毫秒
}

// FileStore 对象存储抽象（provider 驱动，本期实现阿里云 OSS）。
type FileStore interface {
	CreateUploadCredential(ctx context.Context, key, format string, size int64) (*UploadCredential, error)
	DeleteObject(ctx context.Context, objectKey string) error
	ResolvePublicURL(objectKey string) string
}

// OSSFileStore 阿里云 OSS 实现（PostObject 预签名表单直传，经 STS AssumeRole 临时凭证）。
type OSSFileStore struct {
	cfg *config.OSSConfig
	sts *stsProvider
}

// NewOSSFileStore 构造。
func NewOSSFileStore(cfg *config.OSSConfig) (*OSSFileStore, error) {
	return &OSSFileStore{cfg: cfg, sts: newSTSProvider(cfg)}, nil
}

// newBucket 用临时凭证构造 bucket 客户端（携带 SecurityToken）。
func (f *OSSFileStore) newBucket(cred *stsCredentials) (*oss.Bucket, error) {
	client, err := oss.New(f.cfg.Endpoint, cred.AccessKeyID, cred.AccessKeySecret, oss.SecurityToken(cred.SecurityToken))
	if err != nil {
		return nil, err
	}
	return client.Bucket(f.cfg.Bucket)
}

// fullKey 给相对对象键加 BasePath 前缀（如 sso/）。
func (f *OSSFileStore) fullKey(relative string) string {
	p := strings.Trim(f.cfg.BasePath, "/")
	if p == "" {
		return relative
	}
	return p + "/" + relative
}

// postPolicy 是 OSS PostObject 的 policy 载荷。
type postPolicy struct {
	Expiration string  `json:"expiration"`
	Conditions [][]any `json:"conditions"`
}

// CreateUploadCredential 签发 PostObject 直传凭证，policy 约束 key、Content-Type 与大小。
// 凭证由 STS 临时密钥签名，并随响应返回 SecurityToken（客户端以 x-oss-security-token 提交）。
func (f *OSSFileStore) CreateUploadCredential(ctx context.Context, key, format string, size int64) (*UploadCredential, error) {
	cred, err := f.sts.Get(ctx)
	if err != nil {
		return nil, err
	}
	key = f.fullKey(key)
	expires := time.Now().Add(f.cfg.CredentialTTL)
	policy := postPolicy{
		Expiration: expires.UTC().Format("2006-01-02T15:04:05Z"),
		Conditions: [][]any{
			{"eq", "$key", key},
			{"eq", "$Content-Type", contentType(format)},
			{"content-length-range", 0, size},
		},
	}
	raw, err := json.Marshal(policy)
	if err != nil {
		return nil, err
	}
	policyB64 := base64.StdEncoding.EncodeToString(raw)
	mac := hmac.New(sha1.New, []byte(cred.AccessKeySecret))
	mac.Write([]byte(policyB64))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	return &UploadCredential{
		Key:           key,
		UploadURL:     "https://" + f.cfg.Bucket + "." + f.cfg.Endpoint,
		AccessKeyID:   cred.AccessKeyID,
		SecurityToken: cred.SecurityToken,
		Policy:        policyB64,
		Signature:     signature,
		ExpiresAt:     expires.UnixMilli(),
	}, nil
}

// DeleteObject 删除对象。
func (f *OSSFileStore) DeleteObject(ctx context.Context, objectKey string) error {
	cred, err := f.sts.Get(ctx)
	if err != nil {
		return err
	}
	bucket, err := f.newBucket(cred)
	if err != nil {
		return err
	}
	return bucket.DeleteObject(objectKey)
}

// ResolvePublicURL 生成公开访问地址（优先 CustomDomain，否则 bucket.endpoint）。
func (f *OSSFileStore) ResolvePublicURL(objectKey string) string {
	base := f.cfg.CustomDomain
	if base == "" {
		base = "https://" + f.cfg.Bucket + "." + f.cfg.Endpoint
	}
	return strings.TrimRight(base, "/") + "/" + objectKey
}

func contentType(format string) string {
	switch format {
	case "jpg":
		return "image/jpeg"
	case "webp":
		return "image/webp"
	default:
		return "image/" + format
	}
}

// NoopFileStore 未配置对象存储时的占位实现，写操作返回未配置错误。
type NoopFileStore struct{}

// NewNoopFileStore 构造。
func NewNoopFileStore() *NoopFileStore { return &NoopFileStore{} }

// CreateUploadCredential 未配置时拒绝签发。
func (NoopFileStore) CreateUploadCredential(context.Context, string, string, int64) (*UploadCredential, error) {
	return nil, ErrObjectStorageNotConfigured
}

// DeleteObject 未配置时返回错误。
func (NoopFileStore) DeleteObject(context.Context, string) error {
	return ErrObjectStorageNotConfigured
}

// ResolvePublicURL 未配置时返回空串。
func (NoopFileStore) ResolvePublicURL(string) string { return "" }

package crypto

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// ErrMismatchedHash 表示密码不匹配。
var ErrMismatchedHash = errors.New("password does not match")

// Argon2Params argon2id 参数。
type Argon2Params struct {
	Memory      uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

// DefaultArgon2Params 规范下限：memory >= 64MB, iterations >= 3。
func DefaultArgon2Params() Argon2Params {
	return Argon2Params{
		Memory:      64 * 1024,
		Iterations:  3,
		Parallelism: 2,
		SaltLength:  16,
		KeyLength:   32,
	}
}

// normalize 补齐零值，避免用零参数派生密钥（会 panic）。
func (p Argon2Params) normalize() Argon2Params {
	d := DefaultArgon2Params()
	if p.Memory < 8*1024 {
		p.Memory = d.Memory
	}
	if p.Iterations == 0 {
		p.Iterations = d.Iterations
	}
	if p.Parallelism == 0 {
		p.Parallelism = d.Parallelism
	}
	if p.SaltLength == 0 {
		p.SaltLength = d.SaltLength
	}
	if p.KeyLength == 0 {
		p.KeyLength = d.KeyLength
	}
	return p
}

// HashPassword 返回 PHC 格式的 argon2id 编码串。
func HashPassword(password string, p Argon2Params) (string, error) {
	p = p.normalize()
	salt := make([]byte, p.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, p.Iterations, p.Memory, p.Parallelism, p.KeyLength)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.Memory, p.Iterations, p.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword 以常量时间比较密码与编码串。
func VerifyPassword(password, encoded string) error {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return ErrMismatchedHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return ErrMismatchedHash
	}
	var memory, iterations uint32
	var parallelism uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil {
		return ErrMismatchedHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return ErrMismatchedHash
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return ErrMismatchedHash
	}
	got := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, uint32(len(want)))
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return ErrMismatchedHash
	}
	return nil
}

// ConstantTimeEqual 常量时间字符串比较，用于 client_secret 等。
func ConstantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// SHA256 返回 32 字节摘要，用于授权码 / refresh token / client_secret 落库。
func SHA256(s string) []byte {
	sum := sha256.Sum256([]byte(s))
	return sum[:]
}

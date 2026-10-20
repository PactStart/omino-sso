package crypto

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
)

// RandomToken 生成 n 字节熵的 base64url 字符串（无填充）。
// 用于授权码、refresh token、sid、MFA ticket。
func RandomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// RandomHex 生成 n 字节熵的十六进制字符串。
func RandomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(b)
}

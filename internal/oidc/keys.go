package oidc

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"

	jose "github.com/go-jose/go-jose/v4"
)

// signingKey 实现 op.SigningKey（持有私钥）。
type signingKey struct {
	id  string
	key *rsa.PrivateKey
}

func (s *signingKey) SignatureAlgorithm() jose.SignatureAlgorithm { return jose.RS256 }
func (s *signingKey) Key() any                                    { return s.key }
func (s *signingKey) ID() string                                  { return s.id }

// publicKey 实现 op.Key（JWKS 输出）。
type publicKey struct {
	id  string
	key *rsa.PublicKey
}

func (p *publicKey) ID() string                         { return p.id }
func (p *publicKey) Algorithm() jose.SignatureAlgorithm { return jose.RS256 }
func (p *publicKey) Use() string                        { return "sig" }
func (p *publicKey) Key() any                           { return p.key }

// KeyManager 持有单个签名密钥（规范：双 key 轮换为后续阶段，MVP 单 key）。
type KeyManager struct {
	sign *signingKey
	pub  *publicKey
}

// LoadKeyManager 从 PEM 文件加载 RSA 私钥。
func LoadKeyManager(path, kid string) (*KeyManager, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read signing key: %w", err)
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, errors.New("signing key is not valid PEM")
	}
	var priv *rsa.PrivateKey
	switch block.Type {
	case "RSA PRIVATE KEY":
		priv, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	case "PRIVATE KEY":
		var k any
		k, err = x509.ParsePKCS8PrivateKey(block.Bytes)
		if rk, ok := k.(*rsa.PrivateKey); ok {
			priv = rk
		} else if err == nil {
			err = errors.New("signing key must be RSA")
		}
	default:
		return nil, fmt.Errorf("unsupported PEM type %q", block.Type)
	}
	if err != nil {
		return nil, fmt.Errorf("parse signing key: %w", err)
	}
	return &KeyManager{
		sign: &signingKey{id: kid, key: priv},
		pub:  &publicKey{id: kid, key: &priv.PublicKey},
	}, nil
}

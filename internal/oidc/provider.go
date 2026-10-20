package oidc

import (
	"fmt"
	"log/slog"

	"github.com/zitadel/oidc/v3/pkg/op"

	"github.com/omino/omino-sso/internal/config"
)

// NewProvider 构造 OIDC Provider。
// 注意：op.NewProvider 含变长参数，fx 无法直接注入，必须由此显式构造函数包装。
func NewProvider(cfg *config.Config, st *Storage, logger *slog.Logger) (*op.Provider, error) {
	opts := []op.Option{
		op.WithCustomAuthEndpoint(op.NewEndpoint("oauth2/authorize")),
		op.WithCustomTokenEndpoint(op.NewEndpoint("oauth2/token")),
		op.WithCustomIntrospectionEndpoint(op.NewEndpoint("oauth2/introspect")),
		op.WithCustomUserinfoEndpoint(op.NewEndpoint("oauth2/userinfo")),
		op.WithCustomRevocationEndpoint(op.NewEndpoint("oauth2/revoke")),
		op.WithCustomEndSessionEndpoint(op.NewEndpoint("oauth2/end_session")),
		op.WithCustomKeysEndpoint(op.NewEndpoint("oauth2/jwks")),
		op.WithLogger(logger),
	}
	if cfg.AppEnv != "prod" {
		opts = append(opts, op.WithAllowInsecure())
	}

	provider, err := op.NewProvider(
		&op.Config{
			CryptoKey:                cfg.CryptoKey,
			DefaultLogoutRedirectURI: "/logged-out",
			// 只允许 S256（规范 §9 第 2 条）。
			CodeMethodS256:         true,
			AuthMethodPost:         true,
			GrantTypeRefreshToken:  true,
			RequestObjectSupported: false,
		},
		st,
		op.StaticIssuer(cfg.Issuer),
		opts...,
	)
	if err != nil {
		return nil, fmt.Errorf("create oidc provider: %w", err)
	}
	return provider, nil
}

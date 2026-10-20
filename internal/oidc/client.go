package oidc

import (
	"net/url"
	"time"

	"github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/oidc/v3/pkg/op"

	"github.com/omino/omino-sso/internal/store/model"
)

// clientAdapter 把 clients 表映射为 op.Client。
type clientAdapter struct {
	m        *model.Client
	loginURL func(string) string
}

func (c *clientAdapter) GetID() string { return c.m.ClientID }

func (c *clientAdapter) RedirectURIs() []string { return c.m.RedirectURIs }

func (c *clientAdapter) PostLogoutRedirectURIs() []string { return c.m.PostLogoutURIs }

func (c *clientAdapter) ApplicationType() op.ApplicationType {
	if c.m.IsPublic {
		return op.ApplicationTypeNative
	}
	return op.ApplicationTypeWeb
}

func (c *clientAdapter) AuthMethod() oidc.AuthMethod {
	if c.m.IsPublic {
		return oidc.AuthMethodNone
	}
	return oidc.AuthMethodBasic
}

func (c *clientAdapter) ResponseTypes() []oidc.ResponseType {
	return []oidc.ResponseType{oidc.ResponseTypeCode}
}

func (c *clientAdapter) GrantTypes() []oidc.GrantType {
	out := make([]oidc.GrantType, 0, len(c.m.GrantTypes))
	for _, g := range c.m.GrantTypes {
		out = append(out, oidc.GrantType(g))
	}
	return out
}

func (c *clientAdapter) LoginURL(id string) string {
	if c.loginURL != nil {
		return c.loginURL(id)
	}
	return "/login?id=" + url.QueryEscape(id)
}

func (c *clientAdapter) AccessTokenType() op.AccessTokenType { return op.AccessTokenTypeJWT }

func (c *clientAdapter) IDTokenLifetime() time.Duration { return 15 * time.Minute }

func (c *clientAdapter) DevMode() bool { return false }

func (c *clientAdapter) RestrictAdditionalIdTokenScopes() func([]string) []string {
	return func(s []string) []string { return s }
}

func (c *clientAdapter) RestrictAdditionalAccessTokenScopes() func([]string) []string {
	return func(s []string) []string { return s }
}

func (c *clientAdapter) IsScopeAllowed(scope string) bool {
	for _, s := range c.m.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

func (c *clientAdapter) IDTokenUserinfoClaimsAssertion() bool { return false }

func (c *clientAdapter) ClockSkew() time.Duration { return 0 }

var _ op.Client = (*clientAdapter)(nil)

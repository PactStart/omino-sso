package oidc

import (
	"time"

	"github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/oidc/v3/pkg/op"
)

// authRequest 实现 op.AuthRequest。
type authRequest struct {
	ID           string
	ClientID     string
	RedirectURI  string
	Scopes       []string
	ResponseType oidc.ResponseType
	ResponseMode oidc.ResponseMode
	Nonce        string
	State        string
	ACR          string
	AMR          []string
	Audience     []string
	CodeChall    *oidc.CodeChallenge
	Subject      string
	AuthTime     time.Time
	done         bool
}

func (a *authRequest) GetID() string                         { return a.ID }
func (a *authRequest) GetACR() string                        { return a.ACR }
func (a *authRequest) GetAMR() []string                      { return a.AMR }
func (a *authRequest) GetAudience() []string                 { return a.Audience }
func (a *authRequest) GetAuthTime() time.Time                { return a.AuthTime }
func (a *authRequest) GetClientID() string                   { return a.ClientID }
func (a *authRequest) GetCodeChallenge() *oidc.CodeChallenge { return a.CodeChall }
func (a *authRequest) GetNonce() string                      { return a.Nonce }
func (a *authRequest) GetRedirectURI() string                { return a.RedirectURI }
func (a *authRequest) GetResponseType() oidc.ResponseType    { return a.ResponseType }
func (a *authRequest) GetResponseMode() oidc.ResponseMode    { return a.ResponseMode }
func (a *authRequest) GetScopes() []string                   { return a.Scopes }
func (a *authRequest) GetState() string                      { return a.State }
func (a *authRequest) GetSubject() string                    { return a.Subject }
func (a *authRequest) Done() bool                            { return a.done }

// refreshTokenRequest 实现 op.RefreshTokenRequest。
type refreshTokenRequest struct {
	TokenID   string
	FamilyID  string
	ClientID  string
	Subject   string
	Scopes    []string
	AMR       []string
	Audience  []string
	AuthTime  time.Time
	ExpiresAt time.Time
}

func (r *refreshTokenRequest) GetAMR() []string            { return r.AMR }
func (r *refreshTokenRequest) GetAudience() []string       { return r.Audience }
func (r *refreshTokenRequest) GetAuthTime() time.Time      { return r.AuthTime }
func (r *refreshTokenRequest) GetClientID() string         { return r.ClientID }
func (r *refreshTokenRequest) GetScopes() []string         { return r.Scopes }
func (r *refreshTokenRequest) GetSubject() string          { return r.Subject }
func (r *refreshTokenRequest) SetCurrentScopes(s []string) { r.Scopes = s }

// 以下助手用于从 op.TokenRequest 提取客户端/认证信息（接口本身只暴露 subject/audience/scopes）。

func requestClientID(r op.TokenRequest) string {
	switch v := r.(type) {
	case *authRequest:
		return v.ClientID
	case *refreshTokenRequest:
		return v.ClientID
	default:
		return ""
	}
}

func requestAMR(r op.TokenRequest) []string {
	switch v := r.(type) {
	case *authRequest:
		return v.AMR
	case *refreshTokenRequest:
		return v.AMR
	default:
		return nil
	}
}

func requestAuthTime(r op.TokenRequest) time.Time {
	switch v := r.(type) {
	case *authRequest:
		return v.AuthTime
	case *refreshTokenRequest:
		return v.AuthTime
	default:
		return time.Time{}
	}
}

var (
	_ op.AuthRequest         = (*authRequest)(nil)
	_ op.RefreshTokenRequest = (*refreshTokenRequest)(nil)
)

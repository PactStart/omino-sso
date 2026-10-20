package oidc

import (
	"context"

	"github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/oidc/v3/pkg/op"
	"gorm.io/gorm"

	"github.com/omino/omino-sso/internal/store/model"
)

// SetUserinfoFromScopes 已废弃，保留空实现（规范：以 SetUserinfoFromRequest 为准）。
func (s *Storage) SetUserinfoFromScopes(ctx context.Context, userinfo *oidc.UserInfo, userID, clientID string, scopes []string) error {
	return nil
}

// SetUserinfoFromRequest 填充 ID Token 的声明。
func (s *Storage) SetUserinfoFromRequest(ctx context.Context, userinfo *oidc.UserInfo, request op.IDTokenRequest, scopes []string) error {
	return s.fillUserinfo(ctx, userinfo, request.GetSubject())
}

// SetUserinfoFromToken 填充 /oauth2/userinfo 的响应。
func (s *Storage) SetUserinfoFromToken(ctx context.Context, userinfo *oidc.UserInfo, tokenID, subject, origin string) error {
	return s.fillUserinfo(ctx, userinfo, subject)
}

// SetIntrospectionFromToken 填充 /oauth2/introspect 的响应。
func (s *Storage) SetIntrospectionFromToken(ctx context.Context, resp *oidc.IntrospectionResponse, tokenID, subject, clientID string) error {
	u, err := s.st.GetUserByID(ctx, subject)
	if err != nil {
		return err
	}
	resp.Subject = u.ID
	resp.Username = u.Username
	resp.Active = true
	resp.PreferredUsername = u.Username
	resp.Name = u.DisplayName
	return nil
}

// GetPrivateClaimsFromScopes 返回自定义声明。
// 命名一旦上线不得更改（规范：claim 冻结）。
func (s *Storage) GetPrivateClaimsFromScopes(ctx context.Context, userID, clientID string, scopes []string) (map[string]any, error) {
	u, err := s.st.GetUserByID(ctx, userID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return map[string]any{
		"tenant":  u.TenantID,
		"dept_id": u.DeptID,
		"roles":   []string(u.Roles),
	}, nil
}

func (s *Storage) fillUserinfo(ctx context.Context, userinfo *oidc.UserInfo, subject string) error {
	u, err := s.st.GetUserByID(ctx, subject)
	if err != nil {
		return err
	}
	userinfo.Subject = u.ID
	userinfo.PreferredUsername = u.Username
	userinfo.Name = u.DisplayName
	if u.Email != nil {
		userinfo.Email = *u.Email
		userinfo.EmailVerified = true
	}
	if u.AvatarURL != nil {
		userinfo.Picture = *u.AvatarURL
	}
	if len(u.Roles) > 0 {
		userinfo.Claims = map[string]any{
			"tenant":  u.TenantID,
			"dept_id": u.DeptID,
			"roles":   []string(u.Roles),
		}
	}
	return nil
}

var _ = model.User{}

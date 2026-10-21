package oidc

import (
	"context"
	"errors"
	"log/slog"

	"github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/oidc/v3/pkg/op"
	"gorm.io/gorm"

	"github.com/omino/omino-sso/internal/service"
	"github.com/omino/omino-sso/internal/store"
)

// SetUserinfoFromScopes 已废弃，保留空实现（规范：以 SetUserinfoFromRequest 为准）。
func (s *Storage) SetUserinfoFromScopes(ctx context.Context, userinfo *oidc.UserInfo, userID, clientID string, scopes []string) error {
	return nil
}

// SetUserinfoFromRequest 填充 ID Token 的声明。
// 应用内角色与权限**不进 id_token**（research R6），只在 userinfo 出现。
func (s *Storage) SetUserinfoFromRequest(ctx context.Context, userinfo *oidc.UserInfo, request op.IDTokenRequest, scopes []string) error {
	return s.fillUserinfo(ctx, userinfo, request.GetSubject())
}

// SetUserinfoFromToken 填充 /oauth2/userinfo 的响应。
// 先填基础声明，再按令牌所属应用追加 app_roles / permissions（FR-027~FR-030）。
func (s *Storage) SetUserinfoFromToken(ctx context.Context, userinfo *oidc.UserInfo, tokenID, subject, origin string) error {
	if err := s.fillUserinfo(ctx, userinfo, subject); err != nil {
		return err
	}
	s.fillAppContext(ctx, userinfo, tokenID, subject)
	return nil
}

// appContextSource 取"令牌 → 应用上下文"，抽成接口以便单测替换（claims_test.go）。
type appContextSource interface {
	// AppContext 返回令牌所属应用、用户是否仍为成员，以及该应用内的角色与权限。
	// tokenID 无映射时返回 gorm.ErrRecordNotFound。
	AppContext(ctx context.Context, tokenID, subject string) (clientID string, member bool, roles, perms []string, err error)
}

// storeAppContext 基于 store + AppAuthService 的默认实现。
type storeAppContext struct {
	st  *store.Store
	svc *service.AppAuthService
}

func (a storeAppContext) AppContext(ctx context.Context, tokenID, subject string) (string, bool, []string, []string, error) {
	clientID, err := a.st.AccessTokenClientID(ctx, tokenID)
	if err != nil {
		return "", false, nil, nil, err
	}
	member, err := a.st.IsClientMember(ctx, clientID, subject)
	if err != nil {
		return clientID, false, nil, nil, err
	}
	if !member {
		return clientID, false, nil, nil, nil
	}
	roles, perms, err := a.svc.Snapshot(ctx, clientID, subject)
	if err != nil {
		return clientID, true, nil, nil, err
	}
	return clientID, true, roles, perms, nil
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
	if userinfo.Claims == nil {
		userinfo.Claims = map[string]any{}
	}
	userinfo.Claims["tenant"] = u.TenantID
	userinfo.Claims["dept_id"] = u.DeptID
	userinfo.Claims["roles"] = []string(u.Roles)
	return nil
}

// fillAppContext 按令牌所属应用填充 app_roles / permissions。
// 失败语义（contracts/userinfo-claims.md）：映射缺失或用户已非成员时返回空数组、不报错，
// 但必须各打一条 Warn——这是唯一的失效信号；"成员但无任何绑定"属正常状态，不打日志。
func (s *Storage) fillAppContext(ctx context.Context, userinfo *oidc.UserInfo, tokenID, subject string) {
	empty := func() {
		userinfo.Claims["app_roles"] = []string{}
		userinfo.Claims["permissions"] = []string{}
	}
	if s.appCtx == nil {
		empty()
		return
	}
	clientID, member, roles, perms, err := s.appCtx.AppContext(ctx, tokenID, subject)
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		slog.Warn("userinfo app context miss", "reason", "mapping_absent", "token_id", tokenID, "sub", subject)
		empty()
		return
	case err != nil:
		slog.Error("userinfo app context lookup failed", "err", err, "token_id", tokenID, "sub", subject)
		empty()
		return
	case !member:
		slog.Warn("userinfo app context miss", "reason", "not_member", "token_id", tokenID, "sub", subject, "client_id", clientID)
		empty()
		return
	}
	if roles == nil {
		roles = []string{}
	}
	if perms == nil {
		perms = []string{}
	}
	userinfo.Claims["app_roles"] = roles
	userinfo.Claims["permissions"] = perms
}

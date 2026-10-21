package oidc

import (
	"context"
	"reflect"
	"testing"

	"github.com/zitadel/oidc/v3/pkg/oidc"
	"gorm.io/gorm"
)

// fakeAppContext 覆盖 userinfo 应用上下文的三种失效态（T060 / E7）。
type fakeAppContext struct {
	clientID string
	member   bool
	roles    []string
	perms    []string
	err      error
}

func (f fakeAppContext) AppContext(ctx context.Context, tokenID, subject string) (string, bool, []string, []string, error) {
	return f.clientID, f.member, f.roles, f.perms, f.err
}

func TestFillAppContext(t *testing.T) {
	cases := []struct {
		name      string
		src       appContextSource
		wantRoles []string
		wantPerms []string
	}{
		{
			name:      "命中：返回该应用内的角色与权限",
			src:       fakeAppContext{clientID: "demo-app", member: true, roles: []string{"opsAdmin"}, perms: []string{"order:read"}},
			wantRoles: []string{"opsAdmin"},
			wantPerms: []string{"order:read"},
		},
		{
			name:      "映射缺失：返回空数组且不报错",
			src:       fakeAppContext{err: gorm.ErrRecordNotFound},
			wantRoles: []string{},
			wantPerms: []string{},
		},
		{
			name:      "已非成员：返回空数组且不报错",
			src:       fakeAppContext{clientID: "demo-app", member: false},
			wantRoles: []string{},
			wantPerms: []string{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &Storage{appCtx: tc.src}
			ui := &oidc.UserInfo{Claims: map[string]any{}}
			s.fillAppContext(context.Background(), ui, "token-1", "user-1")

			roles, _ := ui.Claims["app_roles"].([]string)
			perms, _ := ui.Claims["permissions"].([]string)
			if !reflect.DeepEqual(roles, tc.wantRoles) {
				t.Fatalf("app_roles = %v, 期望 %v", roles, tc.wantRoles)
			}
			if !reflect.DeepEqual(perms, tc.wantPerms) {
				t.Fatalf("permissions = %v, 期望 %v", perms, tc.wantPerms)
			}
		})
	}
}

// 无 appCtx（例如未装配）时必须给出空数组而不是缺失键，避免接入方解析出 null。
func TestFillAppContextWithoutSource(t *testing.T) {
	s := &Storage{}
	ui := &oidc.UserInfo{Claims: map[string]any{}}
	s.fillAppContext(context.Background(), ui, "token-1", "user-1")
	if roles, ok := ui.Claims["app_roles"].([]string); !ok || len(roles) != 0 {
		t.Fatalf("app_roles 应为空数组，实际 %#v", ui.Claims["app_roles"])
	}
	if perms, ok := ui.Claims["permissions"].([]string); !ok || len(perms) != 0 {
		t.Fatalf("permissions 应为空数组，实际 %#v", ui.Claims["permissions"])
	}
}

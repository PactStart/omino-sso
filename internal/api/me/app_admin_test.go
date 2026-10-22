package me

import (
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// 契约固化（contracts/portal-app-admin-api.md §3）：门户侧应用管理共 20 条端点，
// 构成是 1 条摘要 + 4 条应用信息（详情 / 更新 / Logo 设置 / Logo 清除）+ 15 条成员/角色/权限/授权
// （其中 15 条转交 admin.Handler 的同名实现，应用信息为门户自有实现且只开放四个可编辑字段）。
//
// 这里做编译期 + 数量断言：包装方法被删除或改名会立即失败。
// 行为一致性由 internal/api/router_test.go（路由集 / 未注册路径 / 401）与
// internal/service 的 AuthorizeAppAdmin 集成测试（拒绝原因、隔离、即时生效）覆盖。
func TestAppAdminWrappersPresent(t *testing.T) {
	h := &Handler{}
	handlers := []struct {
		name string
		fn   gin.HandlerFunc
	}{
		{"AppManageContext", h.AppManageContext},
		{"AppClientDetail", h.AppClientDetail},
		{"AppUpdateClient", h.AppUpdateClient},
		{"AppSetClientLogo", h.AppSetClientLogo},
		{"AppClearClientLogo", h.AppClearClientLogo},
		{"AppListMembers", h.AppListMembers},
		{"AppListCandidates", h.AppListCandidates},
		{"AppAddMembers", h.AppAddMembers},
		{"AppRemoveMembers", h.AppRemoveMembers},
		{"AppListRoles", h.AppListRoles},
		{"AppCreateRole", h.AppCreateRole},
		{"AppGetRole", h.AppGetRole},
		{"AppUpdateRole", h.AppUpdateRole},
		{"AppDeleteRole", h.AppDeleteRole},
		{"AppPutRolePermissions", h.AppPutRolePermissions},
		{"AppPutRoleMembers", h.AppPutRoleMembers},
		{"AppListPermissions", h.AppListPermissions},
		{"AppCreatePermission", h.AppCreatePermission},
		{"AppUpdatePermission", h.AppUpdatePermission},
		{"AppDeletePermission", h.AppDeletePermission},
	}
	if len(handlers) != 20 {
		t.Fatalf("门户应用管理端点数为 %d, 期望 20（摘要 + 应用信息 3 + 成员/角色/权限/授权 16）", len(handlers))
	}
	for _, item := range handlers {
		if item.fn == nil {
			t.Fatalf("%s 为空实现", item.name)
		}
	}
}

// TestAppUpdateClientRequestIsWhitelisted 固化「门户只改名称/描述/Logo/回调地址」：
// 请求结构体一旦出现 status / postLogoutUris / accessTokenTtl / refreshTokenTtl 等字段，
// 本测试立即失败，避免日后顺手把 admin 的 8 字段口径复制回来。
func TestAppUpdateClientRequestIsWhitelisted(t *testing.T) {
	want := map[string]bool{
		"clientName":   true,
		"description":  true,
		"logoUrl":      true,
		"redirectUris": true,
		"updatedAt":    true,
	}
	typ := reflect.TypeOf(AppUpdateClientRequest{})
	got := make(map[string]bool, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		tag := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
		if tag == "" || tag == "-" {
			t.Fatalf("字段 %s 缺少 json 标签", typ.Field(i).Name)
		}
		got[tag] = true
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("门户可编辑字段 = %v, 期望 %v", got, want)
	}
}

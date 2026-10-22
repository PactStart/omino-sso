package service_test

import (
	"context"
	"testing"

	"github.com/omino/omino-sso/internal/service"
	"github.com/omino/omino-sso/internal/store/model"
)

// TestUpdateBasicInfoIgnoresReadOnlyFields 固化门户「可编辑字段白名单」的行为，
// 而不只是请求结构体的形状（app_admin_test.go 已做反射断言）：
// 即使调用方把 postLogoutUris / accessTokenTtl / refreshTokenTtl / status 一并传入，
// UpdateBasicInfo 也只落库名称 / 描述 / Logo / 回调地址，其余列保持原值（FR-005 口径调整）。
func TestUpdateBasicInfoIgnoresReadOnlyFields(t *testing.T) {
	env, _, clients, clientID := newAppAuthEnv(t)
	ctx := context.Background()

	// 先落一组「只读字段」的原值，避免断言碰巧命中零值。
	var original model.Client
	if err := env.db.Where("client_id = ?", clientID).Take(&original).Error; err != nil {
		t.Fatalf("读取应用失败: %v", err)
	}
	original.PostLogoutURIs = model.StringList{"https://demo.example.com/signed-out"}
	original.AccessTokenTTL = 1800
	original.RefreshTokenTTL = 86400
	original.Status = 1
	if err := env.db.Save(&original).Error; err != nil {
		t.Fatalf("准备只读字段原值失败: %v", err)
	}

	name := "门户改名"
	desc := "门户改描述"
	logout := []string{"https://evil.example.com/out"}
	shortTTL := 60
	disabled := int8(0)
	updated, err := clients.UpdateBasicInfo(ctx, clientID, service.UpdateClientInput{
		ClientName:      &name,
		Description:     &desc,
		PostLogoutURIs:  &logout,
		AccessTokenTTL:  &shortTTL,
		RefreshTokenTTL: &shortTTL,
		Status:          &disabled,
	})
	if err != nil {
		t.Fatalf("UpdateBasicInfo 失败: %v", err)
	}
	if updated.ClientName != name {
		t.Fatalf("返回值名称 = %q, 期望 %q", updated.ClientName, name)
	}
	if updated.PostLogoutURIs[0] != "https://demo.example.com/signed-out" {
		t.Fatalf("返回值登出地址被改写: %v", updated.PostLogoutURIs)
	}

	var got model.Client
	if err := env.db.Where("client_id = ?", clientID).Take(&got).Error; err != nil {
		t.Fatalf("回读应用失败: %v", err)
	}
	if got.ClientName != name || got.Description == nil || *got.Description != desc {
		t.Fatalf("白名单字段未落库: name=%q desc=%v", got.ClientName, got.Description)
	}
	if len(got.PostLogoutURIs) != 1 || got.PostLogoutURIs[0] != "https://demo.example.com/signed-out" {
		t.Fatalf("登出地址被门户改写: %v", got.PostLogoutURIs)
	}
	if got.AccessTokenTTL != 1800 || got.RefreshTokenTTL != 86400 {
		t.Fatalf("TTL 被门户改写: access=%d refresh=%d", got.AccessTokenTTL, got.RefreshTokenTTL)
	}
	if got.Status != 1 {
		t.Fatalf("启用状态被门户改写: %d", got.Status)
	}
}

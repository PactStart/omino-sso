package service_test

import (
	"context"
	"testing"

	"github.com/omino/omino-sso/internal/service"
)

// 应用内成员 / 角色 / 权限 / 授权的 DB 行为集成测试（TEST_MYSQL_DSN 门控）。

func newAppAuthEnv(t *testing.T) (*orgEnv, *service.AppAuthService, *service.ClientService, string) {
	t.Helper()
	env := newOrgEnv(t)
	env.reset(t)
	for _, table := range []string{
		"client_members", "client_roles", "client_permissions",
		"client_role_permissions", "client_role_members", "access_tokens", "clients",
	} {
		if err := env.db.Exec("TRUNCATE TABLE " + table).Error; err != nil {
			t.Fatalf("truncate %s: %v", table, err)
		}
	}
	appAuth := service.NewAppAuthService(env.st)
	clients := service.NewClientService(env.st)
	cl, _, err := clients.Create(context.Background(), service.CreateClientInput{
		ClientName:   "验收应用",
		RedirectURIs: []string{"https://demo.example.com/cb"},
	})
	if err != nil {
		t.Fatalf("创建应用失败: %v", err)
	}
	return env, appAuth, clients, cl.ClientID
}

func TestAppAuthMembersIdempotent(t *testing.T) {
	env, appAuth, _, clientID := newAppAuthEnv(t)
	dept := env.createOrg(t, nil, "技术中心", nil)
	u1 := env.insertUser(t, dept.ID, "member1")
	u2 := env.insertUser(t, dept.ID, "member2")

	added, skipped, err := appAuth.AddMembers(context.Background(), clientID, []string{u1, u2, u1}, "")
	if err != nil {
		t.Fatalf("首次添加失败: %v", err)
	}
	if added != 2 || skipped != 0 {
		t.Fatalf("首次添加 added=%d skipped=%d, 期望 2/0", added, skipped)
	}
	added, skipped, err = appAuth.AddMembers(context.Background(), clientID, []string{u1, u2}, "")
	if err != nil {
		t.Fatalf("重复添加失败: %v", err)
	}
	if added != 0 || skipped != 2 {
		t.Fatalf("重复添加 added=%d skipped=%d, 期望 0/2", added, skipped)
	}

	_, memberCount, err := appAuth.ClientDetail(context.Background(), clientID)
	if err != nil {
		t.Fatalf("读取详情失败: %v", err)
	}
	if memberCount != 2 {
		t.Fatalf("memberCount = %d, 期望 2", memberCount)
	}
}

func TestAppAuthRemoveCascadesRoleBindings(t *testing.T) {
	env, appAuth, _, clientID := newAppAuthEnv(t)
	dept := env.createOrg(t, nil, "技术中心", nil)
	uid := env.insertUser(t, dept.ID, "roleuser")

	if _, _, err := appAuth.AddMembers(context.Background(), clientID, []string{uid}, ""); err != nil {
		t.Fatalf("添加成员失败: %v", err)
	}
	role, err := appAuth.CreateRole(context.Background(), clientID, "opsAdmin", nil)
	if err != nil {
		t.Fatalf("创建角色失败: %v", err)
	}
	if _, _, _, _, err := appAuth.PutRoleMembers(context.Background(), clientID, role.ID, []string{uid}, nil); err != nil {
		t.Fatalf("授权成员失败: %v", err)
	}

	removed, bindings, notMember, err := appAuth.RemoveMembers(context.Background(), clientID, []string{uid})
	if err != nil {
		t.Fatalf("移除成员失败: %v", err)
	}
	if removed != 1 || bindings != 1 || notMember != 0 {
		t.Fatalf("removed=%d bindings=%d notMember=%d, 期望 1/1/0", removed, bindings, notMember)
	}
	roles, perms, err := appAuth.Snapshot(context.Background(), clientID, uid)
	if err != nil {
		t.Fatalf("读取快照失败: %v", err)
	}
	if len(roles) != 0 || len(perms) != 0 {
		t.Fatalf("移除成员后快照应为空，实际 roles=%v perms=%v", roles, perms)
	}
}

func TestAppAuthDeptJoinCounts(t *testing.T) {
	env, appAuth, _, clientID := newAppAuthEnv(t)
	dept := env.createOrg(t, nil, "技术中心", nil)
	sub := env.createOrg(t, &dept.ID, "平台组", nil)
	_ = env.insertUser(t, dept.ID, "dept-a")
	_ = env.insertUser(t, sub.ID, "dept-b")
	existing := env.insertUser(t, dept.ID, "dept-existing")
	if _, _, err := appAuth.AddMembers(context.Background(), clientID, []string{existing}, ""); err != nil {
		t.Fatalf("预置成员失败: %v", err)
	}
	// 停用账号：应计入 disabled 且不写入成员。
	if err := env.db.Exec(`INSERT INTO users (id, username, display_name, password_hash, dept_id, tenant_id, roles, status,
	 password_updated_at, created_at, updated_at)
	 VALUES ('01J9ZDISABLED0000000000001', 'dept-disabled', '停用', 'x', ?, 'default', JSON_ARRAY('user'), 0, 0, 0, 0)`, dept.ID).Error; err != nil {
		t.Fatalf("插入停用用户失败: %v", err)
	}

	added, existingCount, disabled, err := appAuth.AddMembersByDept(context.Background(), clientID, dept.ID, true)
	if err != nil {
		t.Fatalf("按部门加入失败: %v", err)
	}
	if added != 2 || existingCount != 1 || disabled != 1 {
		t.Fatalf("added=%d existing=%d disabled=%d, 期望 2/1/1", added, existingCount, disabled)
	}
}

func TestAppAuthPortalVisibilityFollowsMembership(t *testing.T) {
	env, appAuth, clients, clientID := newAppAuthEnv(t)
	dept := env.createOrg(t, nil, "技术中心", nil)
	uid := env.insertUser(t, dept.ID, "portaluser")

	apps, err := clients.ListForUser(context.Background(), uid)
	if err != nil {
		t.Fatalf("读取我的应用失败: %v", err)
	}
	if len(apps) != 0 {
		t.Fatalf("非成员不应看到应用，实际 %d 个", len(apps))
	}
	if _, _, err := appAuth.AddMembers(context.Background(), clientID, []string{uid}, ""); err != nil {
		t.Fatalf("添加成员失败: %v", err)
	}
	apps, err = clients.ListForUser(context.Background(), uid)
	if err != nil {
		t.Fatalf("读取我的应用失败: %v", err)
	}
	if len(apps) != 1 || apps[0].ClientID != clientID {
		t.Fatalf("成员应恰好看到 1 个应用，实际 %v", apps)
	}
}

func TestAppAuthGrantAutoJoinsAndVersionConflict(t *testing.T) {
	env, appAuth, _, clientID := newAppAuthEnv(t)
	dept := env.createOrg(t, nil, "技术中心", nil)
	uid := env.insertUser(t, dept.ID, "grantuser")

	role, err := appAuth.CreateRole(context.Background(), clientID, "opsAdmin", nil)
	if err != nil {
		t.Fatalf("创建角色失败: %v", err)
	}
	final, added, removed, autoJoined, err := appAuth.PutRoleMembers(context.Background(), clientID, role.ID, []string{uid}, nil)
	if err != nil {
		t.Fatalf("授权失败: %v", err)
	}
	if len(final) != 1 || added != 1 || removed != 0 || autoJoined != 1 {
		t.Fatalf("final=%v added=%d removed=%d auto=%d, 期望 1/1/0/1", final, added, removed, autoJoined)
	}
	member, err := env.st.IsClientMember(context.Background(), clientID, uid)
	if err != nil || !member {
		t.Fatalf("授权后应自动成为成员: member=%v err=%v", member, err)
	}

	// 覆盖式写入幂等：重复提交相同集合不应产生变更。
	_, added, removed, autoJoined, err = appAuth.PutRoleMembers(context.Background(), clientID, role.ID, []string{uid}, nil)
	if err != nil {
		t.Fatalf("重复授权失败: %v", err)
	}
	if added != 0 || removed != 0 || autoJoined != 0 {
		t.Fatalf("重复授权应无变更，实际 added=%d removed=%d auto=%d", added, removed, autoJoined)
	}

	// 版本校验：提交过期版本 → ErrAppResourceStale。
	role2, _, _, err := appAuth.GetRole(context.Background(), clientID, role.ID)
	if err != nil {
		t.Fatalf("读取角色失败: %v", err)
	}
	stale := role2.UpdatedAt - 1
	if _, _, _, _, err := appAuth.PutRoleMembers(context.Background(), clientID, role.ID, []string{uid}, &stale); err == nil {
		t.Fatal("过期版本应返回错误")
	} else if err != service.ErrAppResourceStale {
		t.Fatalf("过期版本错误 = %v, 期望 ErrAppResourceStale", err)
	}
}

func TestAppAuthSnapshotUnion(t *testing.T) {
	env, appAuth, _, clientID := newAppAuthEnv(t)
	dept := env.createOrg(t, nil, "技术中心", nil)
	uid := env.insertUser(t, dept.ID, "snapuser")
	ctx := context.Background()

	p1, err := appAuth.CreatePermission(ctx, clientID, service.PermissionInput{Code: "order:read", Name: "订单读取"})
	if err != nil {
		t.Fatalf("创建权限失败: %v", err)
	}
	p2, err := appAuth.CreatePermission(ctx, clientID, service.PermissionInput{Code: "order:write", Name: "订单写入"})
	if err != nil {
		t.Fatalf("创建权限失败: %v", err)
	}
	r1, err := appAuth.CreateRole(ctx, clientID, "roleA", nil)
	if err != nil {
		t.Fatalf("创建角色失败: %v", err)
	}
	r2, err := appAuth.CreateRole(ctx, clientID, "roleB", nil)
	if err != nil {
		t.Fatalf("创建角色失败: %v", err)
	}
	if _, _, _, err := appAuth.PutRolePermissions(ctx, clientID, r1.ID, []string{p1.ID}, nil); err != nil {
		t.Fatalf("授权权限失败: %v", err)
	}
	if _, _, _, err := appAuth.PutRolePermissions(ctx, clientID, r2.ID, []string{p1.ID, p2.ID}, nil); err != nil {
		t.Fatalf("授权权限失败: %v", err)
	}
	if _, _, _, _, err := appAuth.PutRoleMembers(ctx, clientID, r1.ID, []string{uid}, nil); err != nil {
		t.Fatalf("授权成员失败: %v", err)
	}
	if _, _, _, _, err := appAuth.PutRoleMembers(ctx, clientID, r2.ID, []string{uid}, nil); err != nil {
		t.Fatalf("授权成员失败: %v", err)
	}

	roles, perms, err := appAuth.Snapshot(ctx, clientID, uid)
	if err != nil {
		t.Fatalf("读取快照失败: %v", err)
	}
	if len(roles) != 2 || roles[0] != "roleA" || roles[1] != "roleB" {
		t.Fatalf("roles = %v, 期望 [roleA roleB]", roles)
	}
	if len(perms) != 2 || perms[0] != "order:read" || perms[1] != "order:write" {
		t.Fatalf("permissions = %v, 期望去重并集 [order:read order:write]", perms)
	}
}

func TestAppAuthPermissionDeleteProtection(t *testing.T) {
	env, appAuth, _, clientID := newAppAuthEnv(t)
	_ = env
	ctx := context.Background()

	parent, err := appAuth.CreatePermission(ctx, clientID, service.PermissionInput{Code: "order", Name: "订单"})
	if err != nil {
		t.Fatalf("创建父权限失败: %v", err)
	}
	child, err := appAuth.CreatePermission(ctx, clientID, service.PermissionInput{Code: "order:read", Name: "订单读取", ParentID: &parent.ID})
	if err != nil {
		t.Fatalf("创建子权限失败: %v", err)
	}
	if _, _, err := appAuth.DeletePermission(ctx, clientID, parent.ID); err == nil {
		t.Fatal("有子权限时应拒绝删除")
	}
	if _, _, err := appAuth.DeletePermission(ctx, clientID, child.ID); err != nil {
		t.Fatalf("删除子权限失败: %v", err)
	}
	if _, _, err := appAuth.DeletePermission(ctx, clientID, parent.ID); err != nil {
		t.Fatalf("删除父权限失败: %v", err)
	}
}

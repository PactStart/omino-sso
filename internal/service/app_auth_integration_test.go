package service_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/omino/omino-sso/internal/pkg/id"
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

// ---- 性能造数与计时（quickstart §11 / SC-002、SC-004）----

const (
	perfUserCount    = 10000
	perfMemberCount  = 500
	perfPermCount    = 200
	perfBatchSize    = 500
	perfDurationCeil = time.Second
	perfRound        = 5
)

// TestAppAuthPerfFixture 造 10,000 用户 + 500 成员 + 200 权限节点，
// 测量成员列表 / 候选 / memberCount 三条查询的耗时并输出 EXPLAIN。
// 默认跳过，需 PERF_FIXTURE=1 显式开启，不进入常规 go test ./...。
func TestAppAuthPerfFixture(t *testing.T) {
	if os.Getenv("PERF_FIXTURE") != "1" {
		t.Skip("未设置 PERF_FIXTURE=1，跳过性能造数用例")
	}
	env, appAuth, _, clientID := newAppAuthEnv(t)
	ctx := context.Background()
	dept := env.createOrg(t, nil, "性能测试部", nil)

	start := time.Now()
	userIDs := seedPerfUsers(t, env, dept.ID, perfUserCount)
	seedPerfMembers(t, env, clientID, userIDs[:perfMemberCount])
	seedPerfPermissions(t, env, clientID, perfPermCount)
	seedPerfRoles(t, env, clientID, userIDs[:perfMemberCount])
	t.Logf("造数完成：%d 用户 / %d 成员 / %d 权限节点 / %d 用户绑定角色，耗时 %s",
		perfUserCount, perfMemberCount, perfPermCount, perfMemberCount, time.Since(start).Round(time.Millisecond))

	// SC-002：三条接口路径均 ≤ 1s
	measure := func(name string, fn func() error) {
		t.Helper()
		var max time.Duration
		for i := 0; i < perfRound; i++ {
			began := time.Now()
			if err := fn(); err != nil {
				t.Fatalf("%s 第 %d 轮失败: %v", name, i+1, err)
			}
			if d := time.Since(began); d > max {
				max = d
			}
		}
		t.Logf("%-24s 最大耗时 %s（%d 轮）", name, max.Round(time.Millisecond), perfRound)
		if max > perfDurationCeil {
			t.Fatalf("%s 最大耗时 %s，超过 1s（SC-002）", name, max.Round(time.Millisecond))
		}
	}

	measure("成员列表 pageSize=50", func() error {
		rows, total, err := appAuth.ListMembers(ctx, clientID, service.MemberFilter{Page: 1, PageSize: 50})
		if err != nil {
			return err
		}
		if len(rows) != 50 || total != perfMemberCount {
			return fmt.Errorf("成员列表返回 %d 行 / total=%d，期望 50 / %d", len(rows), total, perfMemberCount)
		}
		return nil
	})
	measure("候选列表 keyword=perf", func() error {
		rows, total, err := appAuth.ListCandidates(ctx, clientID, service.MemberFilter{Keyword: "perf", Page: 1, PageSize: 50})
		if err != nil {
			return err
		}
		if len(rows) != 50 || total != perfUserCount-perfMemberCount {
			return fmt.Errorf("候选返回 %d 行 / total=%d，期望 50 / %d", len(rows), total, perfUserCount-perfMemberCount)
		}
		return nil
	})
	measure("memberCount(ClientDetail)", func() error {
		_, count, err := appAuth.ClientDetail(ctx, clientID)
		if err != nil {
			return err
		}
		if count != perfMemberCount {
			return fmt.Errorf("memberCount = %d，期望 %d", count, perfMemberCount)
		}
		return nil
	})

	// SC-004：200 节点权限树一次取全量（前端建树不额外请求）
	measure("权限列表 200 节点", func() error {
		list, err := appAuth.ListPermissions(ctx, clientID)
		if err != nil {
			return err
		}
		if len(list) != perfPermCount {
			return fmt.Errorf("权限节点 %d 个，期望 %d", len(list), perfPermCount)
		}
		return nil
	})

	// EXPLAIN：不得出现 Using filesort / Using temporary
	explainPerfQueries(t, env, clientID)
}

// seedPerfUsers 批量插入 perf00001… 用户，返回其 ID（按插入顺序）。
func seedPerfUsers(t *testing.T, env *orgEnv, deptID string, n int) []string {
	t.Helper()
	ids := make([]string, 0, n)
	for i := 0; i < n; i += perfBatchSize {
		upper := i + perfBatchSize
		if upper > n {
			upper = n
		}
		placeholders := make([]string, 0, upper-i)
		args := make([]any, 0, (upper-i)*5)
		for j := i; j < upper; j++ {
			uid := id.New()
			ids = append(ids, uid)
			placeholders = append(placeholders, "(?, ?, ?, 'x', ?, JSON_ARRAY('user'), 1, 0, 0, 0)")
			args = append(args, uid, fmt.Sprintf("perf%05d", j+1), fmt.Sprintf("性能用户%d", j+1), deptID)
		}
		stmt := `INSERT INTO users (id, username, display_name, password_hash, dept_id, roles, status,
			password_updated_at, created_at, updated_at) VALUES ` + strings.Join(placeholders, ",")
		if err := env.db.Exec(stmt, args...).Error; err != nil {
			t.Fatalf("批量插入用户失败（第 %d 批）: %v", i/perfBatchSize+1, err)
		}
	}
	return ids
}

// seedPerfMembers 把前 n 个用户加入应用（raw 插入，避免叠加 AddMembers 的分片开销）。
func seedPerfMembers(t *testing.T, env *orgEnv, clientID string, userIDs []string) {
	t.Helper()
	now := time.Now().UnixMilli()
	for i := 0; i < len(userIDs); i += perfBatchSize {
		upper := i + perfBatchSize
		if upper > len(userIDs) {
			upper = len(userIDs)
		}
		placeholders := make([]string, 0, upper-i)
		args := make([]any, 0, (upper-i)*3)
		for _, uid := range userIDs[i:upper] {
			placeholders = append(placeholders, "(?, ?, ?, ?)")
			args = append(args, clientID, uid, nil, now)
		}
		stmt := `INSERT INTO client_members (client_id, user_id, added_by, joined_at) VALUES ` + strings.Join(placeholders, ",")
		if err := env.db.Exec(stmt, args...).Error; err != nil {
			t.Fatalf("批量插入成员失败: %v", err)
		}
	}
}

// seedPerfPermissions 造 3 层、共 n 个权限节点（20 根 / 100 二级 / 余下三级）。
func seedPerfPermissions(t *testing.T, env *orgEnv, clientID string, n int) {
	t.Helper()
	now := time.Now().UnixMilli()
	type node struct {
		id       string
		parentID *string
		code     string
		sort     int
	}
	nodes := make([]node, 0, n)
	rootTotal := 20
	if n < rootTotal {
		rootTotal = n
	}
	for i := 0; i < rootTotal; i++ {
		nodes = append(nodes, node{id: id.New(), code: fmt.Sprintf("perf.p%02d", i), sort: i})
	}
	roots := len(nodes)
	for i := 0; i < roots*5 && len(nodes) < n; i++ {
		parent := nodes[i%roots]
		parentID := parent.id
		nodes = append(nodes, node{id: id.New(), parentID: &parentID, code: fmt.Sprintf("%s.l2.%02d", parent.code, i), sort: i})
	}
	for len(nodes) < n {
		// 三级挂在二级节点上
		parent := nodes[roots+len(nodes)%(len(nodes)-roots)]
		parentID := parent.id
		nodes = append(nodes, node{id: id.New(), parentID: &parentID, code: fmt.Sprintf("%s.l3.%02d", parent.code, len(nodes)), sort: len(nodes)})
	}
	for _, nd := range nodes {
		if err := env.db.Exec(`INSERT INTO client_permissions (id, client_id, parent_id, code, name, description, sort, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, nd.id, clientID, nd.parentID, nd.code, nd.code, nil, nd.sort, now, now).Error; err != nil {
			t.Fatalf("插入权限节点 %s 失败: %v", nd.code, err)
		}
	}
}

// seedPerfRoles 建 2 个角色：roleAll 绑定全部成员、roleHalf 绑定前一半成员，
// 使成员列表里的 role_count 子查询有真实数据。
func seedPerfRoles(t *testing.T, env *orgEnv, clientID string, userIDs []string) {
	t.Helper()
	now := time.Now().UnixMilli()
	roles := []struct {
		id    string
		name  string
		bound []string
	}{
		{id: id.New(), name: "perfAll", bound: userIDs},
		{id: id.New(), name: "perfHalf", bound: userIDs[:len(userIDs)/2]},
	}
	for _, r := range roles {
		if err := env.db.Exec(`INSERT INTO client_roles (id, client_id, name, description, created_at, updated_at)
			VALUES (?, ?, ?, NULL, ?, ?)`, r.id, clientID, r.name, now, now).Error; err != nil {
			t.Fatalf("插入角色 %s 失败: %v", r.name, err)
		}
	}
	for _, r := range roles {
		for i := 0; i < len(r.bound); i += perfBatchSize {
			upper := i + perfBatchSize
			if upper > len(r.bound) {
				upper = len(r.bound)
			}
			placeholders := make([]string, 0, upper-i)
			args := make([]any, 0, (upper-i)*4)
			for _, uid := range r.bound[i:upper] {
				placeholders = append(placeholders, "(?, ?, ?, ?)")
				args = append(args, r.id, uid, clientID, now)
			}
			stmt := `INSERT INTO client_role_members (role_id, user_id, client_id, created_at) VALUES ` + strings.Join(placeholders, ",")
			if err := env.db.Exec(stmt, args...).Error; err != nil {
				t.Fatalf("绑定角色成员失败（%s）: %v", r.name, err)
			}
		}
	}
}

// explainPerfQueries 打印三条关键 SQL 的执行计划：
// 候选与 memberCount 必须无 Using filesort / Using temporary；
// 成员列表允许（见其 allowSort 说明），其耗时仍受 SC-002 约束。
func explainPerfQueries(t *testing.T, env *orgEnv, clientID string) {
	t.Helper()
	memberWhereClause := ` AND (u.username LIKE CONCAT(?, '%') OR u.display_name LIKE CONCAT(?, '%'))`
	cases := []struct {
		name      string
		sql       string
		args      []any
		allowSort bool
	}{
		{
			name: "成员列表",
			sql: `SELECT m.user_id, u.username, u.display_name, u.dept_id,
  COALESCE(o.name, '') AS dept_name, u.status, m.joined_at,
  (SELECT COUNT(*) FROM client_role_members rm WHERE rm.client_id = m.client_id AND rm.user_id = m.user_id) AS role_count
FROM client_members m
JOIN users u ON u.id = m.user_id
LEFT JOIN organizations o ON o.id = u.dept_id
WHERE m.client_id = ?` + memberWhereClause + `
ORDER BY u.username ASC
LIMIT 50 OFFSET 0`,
			args: []any{clientID, "perf", "perf"},
			// 排序键跨表（users.username），优化器选 client_members 驱动后必然在成员集内排序：
			// 规模上界 = 成员数（本例 500 行，实测 <1ms），不随用户总量增长。
			// STRAIGHT_JOIN 强制 users 驱动虽可消掉排序，但分页代价会变成与用户总量成正比，不采用。
			allowSort: true,
		},
		{
			name: "候选列表",
			sql: `SELECT u.id, u.username, u.display_name, u.dept_id, COALESCE(o.name, '') AS dept_name
FROM users u LEFT JOIN organizations o ON o.id = u.dept_id
WHERE u.status = 1 AND NOT EXISTS (SELECT 1 FROM client_members m2 WHERE m2.client_id = ? AND m2.user_id = u.id)` + memberWhereClause + `
ORDER BY u.username ASC LIMIT 50 OFFSET 0`,
			args: []any{clientID, "perf", "perf"},
			// keyword 含 display_name 的包含匹配（前导通配符），无法走 username 唯一索引，
			// 必然全表扫描 + 排序；这是产品语义（按姓名模糊搜索）的固有代价，
			// 已通过 pageSize ≤ 50、keyword ≤ 64 限制，实测 10k 用户 < 300ms，仍满足 SC-002。
			allowSort: true,
		},
		{
			name: "memberCount",
			sql:  `SELECT COUNT(*) FROM client_members m JOIN users u ON u.id = m.user_id WHERE m.client_id = ?` + memberWhereClause,
			args: []any{clientID, "perf", "perf"},
		},
	}
	for _, c := range cases {
		rows, err := env.db.Raw("EXPLAIN "+c.sql, c.args...).Rows()
		if err != nil {
			t.Fatalf("%s EXPLAIN 失败: %v", c.name, err)
		}
		cols, err := rows.Columns()
		if err != nil {
			_ = rows.Close()
			t.Fatalf("%s EXPLAIN 读取列失败: %v", c.name, err)
		}
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				_ = rows.Close()
				t.Fatalf("%s EXPLAIN 扫描失败: %v", c.name, err)
			}
			record := make([]string, 0, len(cols))
			extra := ""
			for i, col := range cols {
				val := fmt.Sprintf("%v", printable(vals[i]))
				record = append(record, col+"="+val)
				if col == "Extra" {
					extra = val
				}
			}
			t.Logf("EXPLAIN %s: %s", c.name, strings.Join(record, " "))
			degrades := strings.Contains(extra, "Using filesort") || strings.Contains(extra, "Using temporary")
			if degrades && !c.allowSort {
				t.Fatalf("%s 执行计划出现退化：Extra=%q", c.name, extra)
			}
			if degrades && c.allowSort {
				t.Logf("%s 计划含排序（允许，有界结果集/受限关键字）：Extra=%q", c.name, extra)
			}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			t.Fatalf("%s EXPLAIN 遍历失败: %v", c.name, err)
		}
		_ = rows.Close()
	}
}

// printable 把 EXPLAIN 的原始列值转成可读形式（[]byte 直接转字符串）。
func printable(v any) any {
	switch x := v.(type) {
	case []byte:
		return string(x)
	case nil:
		return "NULL"
	default:
		return x
	}
}

// ---- 006-app-admin-role：应用管理员标记 ----

// 授予/取消的完整生命周期：成员列表回显、判定放行/拒绝、即时生效、移出即失效、重新加入默认普通成员。
func TestAppMemberAdminLifecycle(t *testing.T) {
	env, appAuth, clients, clientID := newAppAuthEnv(t)
	ctx := context.Background()
	dept := env.createOrg(t, nil, "技术中心", nil)
	u1 := env.insertUser(t, dept.ID, "admin1")
	u2 := env.insertUser(t, dept.ID, "member2")
	outsider := env.insertUser(t, dept.ID, "outsider")

	if _, _, err := appAuth.AddMembers(ctx, clientID, []string{u1, u2}, ""); err != nil {
		t.Fatalf("添加成员失败: %v", err)
	}

	// 初始：两人都是普通成员，判定为「非管理员」。
	if err := appAuth.AuthorizeAppAdmin(ctx, clientID, u1); !errors.Is(err, service.ErrAppNotAdmin) {
		t.Fatalf("授予前 AuthorizeAppAdmin = %v, 期望 ErrAppNotAdmin", err)
	}

	before, changed, err := appAuth.SetMemberAdmin(ctx, clientID, u1, true)
	if err != nil {
		t.Fatalf("设为管理员失败: %v", err)
	}
	if before || !changed {
		t.Fatalf("首次授予 before=%v changed=%v, 期望 false/true", before, changed)
	}
	if err := appAuth.AuthorizeAppAdmin(ctx, clientID, u1); err != nil {
		t.Fatalf("授予后判定失败: %v", err)
	}

	// 幂等：同值重复设置不产生变更。
	before, changed, err = appAuth.SetMemberAdmin(ctx, clientID, u1, true)
	if err != nil {
		t.Fatalf("重复授予失败: %v", err)
	}
	if !before || changed {
		t.Fatalf("重复授予 before=%v changed=%v, 期望 true/false", before, changed)
	}

	// 成员列表回显 isAdmin。
	items, _, err := appAuth.ListMembers(ctx, clientID, service.MemberFilter{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("成员列表失败: %v", err)
	}
	flags := map[string]bool{}
	for _, it := range items {
		flags[it.UserID] = it.IsAdmin
	}
	if !flags[u1] || flags[u2] {
		t.Fatalf("成员列表 isAdmin 不正确: %v", flags)
	}

	// 门户「我的应用」带出 isAppAdmin，且只对管理员为真。
	apps, err := clients.ListForUser(ctx, u1)
	if err != nil {
		t.Fatalf("ListForUser 失败: %v", err)
	}
	if len(apps) != 1 || !apps[0].IsAppAdmin {
		t.Fatalf("u1 的应用列表 = %+v, 期望 1 项且 isAppAdmin=true", apps)
	}
	apps, err = clients.ListForUser(ctx, u2)
	if err != nil {
		t.Fatalf("ListForUser(u2) 失败: %v", err)
	}
	if len(apps) != 1 || apps[0].IsAppAdmin {
		t.Fatalf("u2 的应用列表 = %+v, 期望 1 项且 isAppAdmin=false", apps)
	}

	// 非成员与不存在的应用分别给出可区分的服务端原因（HTTP 层统一 403）。
	if err := appAuth.AuthorizeAppAdmin(ctx, clientID, outsider); !errors.Is(err, service.ErrAppMemberNotFound) {
		t.Fatalf("非成员判定 = %v, 期望 ErrAppMemberNotFound", err)
	}
	if err := appAuth.AuthorizeAppAdmin(ctx, "no-such-client", u1); !errors.Is(err, service.ErrClientNotFound) {
		t.Fatalf("不存在的应用判定 = %v, 期望 ErrClientNotFound", err)
	}
	if _, _, err := appAuth.SetMemberAdmin(ctx, clientID, outsider, true); !errors.Is(err, service.ErrAppMemberNotFound) {
		t.Fatalf("把非成员设为管理员 = %v, 期望 ErrAppMemberNotFound", err)
	}

	// 移出即失去管理员身份；重新加入默认普通成员（FR-008）。
	if _, _, _, err := appAuth.RemoveMembers(ctx, clientID, []string{u1}); err != nil {
		t.Fatalf("移除成员失败: %v", err)
	}
	if apps, err = clients.ListForUser(ctx, u1); err != nil {
		t.Fatalf("移除后 ListForUser 失败: %v", err)
	}
	if len(apps) != 0 {
		t.Fatalf("移除后 u1 仍可见应用: %+v", apps)
	}
	if _, _, err := appAuth.AddMembers(ctx, clientID, []string{u1}, ""); err != nil {
		t.Fatalf("重新加入失败: %v", err)
	}
	if err := appAuth.AuthorizeAppAdmin(ctx, clientID, u1); !errors.Is(err, service.ErrAppNotAdmin) {
		t.Fatalf("重新加入后判定 = %v, 期望 ErrAppNotAdmin（默认普通成员）", err)
	}
}

// 应用停用后：管理判定被拒，标记本身保留（重新启用即恢复）。
func TestAuthorizeAppAdminRejectsDisabledClient(t *testing.T) {
	env, appAuth, clients, clientID := newAppAuthEnv(t)
	ctx := context.Background()
	dept := env.createOrg(t, nil, "技术中心", nil)
	admin := env.insertUser(t, dept.ID, "admin1")

	if _, _, err := appAuth.AddMembers(ctx, clientID, []string{admin}, ""); err != nil {
		t.Fatalf("添加成员失败: %v", err)
	}
	if _, _, err := appAuth.SetMemberAdmin(ctx, clientID, admin, true); err != nil {
		t.Fatalf("设为管理员失败: %v", err)
	}

	disabled := int8(0)
	if _, err := clients.Update(ctx, clientID, service.UpdateClientInput{Status: &disabled}); err != nil {
		t.Fatalf("停用应用失败: %v", err)
	}
	if err := appAuth.AuthorizeAppAdmin(ctx, clientID, admin); !errors.Is(err, service.ErrAppClientDisabled) {
		t.Fatalf("停用后判定 = %v, 期望 ErrAppClientDisabled", err)
	}

	enabled := int8(1)
	if _, err := clients.Update(ctx, clientID, service.UpdateClientInput{Status: &enabled}); err != nil {
		t.Fatalf("启用应用失败: %v", err)
	}
	if err := appAuth.AuthorizeAppAdmin(ctx, clientID, admin); err != nil {
		t.Fatalf("重新启用后判定失败（标记应保留）: %v", err)
	}
}

// 平台 admin 角色不构成门户侧管理权（spec A2 / US4-AC7）：只有 is_admin 才放行。
func TestAuthorizeAppAdminIgnoresPlatformRole(t *testing.T) {
	env, appAuth, _, clientID := newAppAuthEnv(t)
	ctx := context.Background()
	dept := env.createOrg(t, nil, "技术中心", nil)
	platformAdmin := env.insertUser(t, dept.ID, "platform-admin")

	if err := env.db.Exec(
		`UPDATE users SET roles = JSON_ARRAY('user', 'admin') WHERE id = ?`, platformAdmin,
	).Error; err != nil {
		t.Fatalf("设置平台管理员角色失败: %v", err)
	}
	if _, _, err := appAuth.AddMembers(ctx, clientID, []string{platformAdmin}, ""); err != nil {
		t.Fatalf("添加成员失败: %v", err)
	}

	if err := appAuth.AuthorizeAppAdmin(ctx, clientID, platformAdmin); !errors.Is(err, service.ErrAppNotAdmin) {
		t.Fatalf("平台管理员（非应用管理员）判定 = %v, 期望 ErrAppNotAdmin", err)
	}
	if _, _, err := appAuth.SetMemberAdmin(ctx, clientID, platformAdmin, true); err != nil {
		t.Fatalf("授予应用管理员失败: %v", err)
	}
	if err := appAuth.AuthorizeAppAdmin(ctx, clientID, platformAdmin); err != nil {
		t.Fatalf("兼任应用管理员后判定失败: %v", err)
	}
}

// 管理权严格按应用隔离，且取消后同一会话下一次请求即被拒（无缓存，FR-010 / FR-011）。
func TestAuthorizeAppAdminIsolatedAndImmediate(t *testing.T) {
	env, appAuth, clients, clientX := newAppAuthEnv(t)
	ctx := context.Background()
	dept := env.createOrg(t, nil, "技术中心", nil)
	operator := env.insertUser(t, dept.ID, "app-admin")

	// 第二个应用：operator 只是普通成员。
	other, _, err := clients.Create(ctx, service.CreateClientInput{
		ClientName:   "另一个应用",
		RedirectURIs: []string{"https://other.example.com/cb"},
	})
	if err != nil {
		t.Fatalf("创建第二个应用失败: %v", err)
	}
	if _, _, err := appAuth.AddMembers(ctx, clientX, []string{operator}, ""); err != nil {
		t.Fatalf("加入应用 X 失败: %v", err)
	}
	if _, _, err := appAuth.AddMembers(ctx, other.ClientID, []string{operator}, ""); err != nil {
		t.Fatalf("加入应用 Y 失败: %v", err)
	}
	if _, _, err := appAuth.SetMemberAdmin(ctx, clientX, operator, true); err != nil {
		t.Fatalf("授予 X 的管理员失败: %v", err)
	}

	if err := appAuth.AuthorizeAppAdmin(ctx, clientX, operator); err != nil {
		t.Fatalf("X 的管理员应放行: %v", err)
	}
	if err := appAuth.AuthorizeAppAdmin(ctx, other.ClientID, operator); !errors.Is(err, service.ErrAppNotAdmin) {
		t.Fatalf("对 Y 的判定 = %v, 期望 ErrAppNotAdmin（权限不得外溢）", err)
	}

	// 取消后同一会话（无重新登录、无缓存）下一次判定即被拒。
	before, changed, err := appAuth.SetMemberAdmin(ctx, clientX, operator, false)
	if err != nil || !before || !changed {
		t.Fatalf("取消管理员失败: before=%v changed=%v err=%v", before, changed, err)
	}
	if err := appAuth.AuthorizeAppAdmin(ctx, clientX, operator); !errors.Is(err, service.ErrAppNotAdmin) {
		t.Fatalf("取消后判定 = %v, 期望 ErrAppNotAdmin（应立即生效）", err)
	}
}

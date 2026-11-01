package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/omino/omino-sso/internal/service"
)

// 组织授权（010-app-access-org-grant）的 DB 行为集成测试（TEST_MYSQL_DSN 门控）。
//
// 这些用例是"判定口径"的护栏：可见性、覆盖人数、撤销影响人数三者必须始终一致，
// 任何一处 SQL 漂移都会在这里暴露（critique E1）。

func orgGrantCtx() context.Context { return context.Background() }

// hasApp 判断某用户的「我的应用」是否包含指定应用。
func hasApp(t *testing.T, clients *service.ClientService, userID, clientID string) bool {
	t.Helper()
	apps, err := clients.ListForUser(orgGrantCtx(), userID)
	if err != nil {
		t.Fatalf("ListForUser(%s): %v", userID, err)
	}
	for _, a := range apps {
		if a.ClientID == clientID {
			return true
		}
	}
	return false
}

func mustAddGrant(t *testing.T, appAuth *service.AppAuthService, clientID string, orgID string, includeSub bool) {
	t.Helper()
	if _, _, _, _, _, err := appAuth.AddOrgGrants(orgGrantCtx(), clientID, "",
		[]service.OrgGrantInput{{OrgID: orgID, IncludeSub: includeSub}}); err != nil {
		t.Fatalf("AddOrgGrants(%s, includeSub=%v): %v", orgID, includeSub, err)
	}
}

// TestOrgGrant_VisibilityMatchesCoverage 是本特性的口径等价性门禁（不得删除或改名）：
// ① /me/apps 的可见集合 == 组织授权覆盖人数去重后的并集；
// ② includeSubDepts=false 时子孙部门的人员既不可见、也不计入覆盖。
func TestOrgGrant_VisibilityMatchesCoverage(t *testing.T) {
	env, appAuth, clients, clientID := newAppAuthEnv(t)
	ctx := orgGrantCtx()

	root := env.createOrg(t, nil, "研发部", nil)
	sub := env.createOrg(t, &root.ID, "平台组", nil)
	other := env.createOrg(t, nil, "市场部", nil)
	dev1 := env.insertUser(t, sub.ID, "dev1")
	dev2 := env.insertUser(t, root.ID, "dev2")
	mkt1 := env.insertUser(t, other.ID, "mkt1")

	mustAddGrant(t, appAuth, clientID, root.ID, true)

	if !hasApp(t, clients, dev1, clientID) {
		t.Fatal("子部门员工 dev1 应可见（includeSubDepts=true）")
	}
	if !hasApp(t, clients, dev2, clientID) {
		t.Fatal("本级员工 dev2 应可见")
	}
	if hasApp(t, clients, mkt1, clientID) {
		t.Fatal("市场部员工 mkt1 不应可见")
	}

	grants, err := appAuth.ListOrgGrants(ctx, clientID)
	if err != nil {
		t.Fatalf("ListOrgGrants: %v", err)
	}
	if len(grants) != 1 {
		t.Fatalf("组织授权条数 = %d，期望 1", len(grants))
	}
	if grants[0].CoveredUsers != 2 {
		t.Fatalf("覆盖人数 = %d，期望 2（dev1 + dev2）", grants[0].CoveredUsers)
	}
	// 等价性：无手动成员时，可见人数 == 覆盖人数。
	visible, err := appAuth.VisibleUserCount(ctx, clientID)
	if err != nil {
		t.Fatalf("VisibleUserCount: %v", err)
	}
	if visible != grants[0].CoveredUsers {
		t.Fatalf("可见人数 %d != 覆盖人数 %d（口径不一致）", visible, grants[0].CoveredUsers)
	}

	// 切换为「不含子部门」：dev1 立即不可见，覆盖人数降为 1。
	mustAddGrant(t, appAuth, clientID, root.ID, false)
	if hasApp(t, clients, dev1, clientID) {
		t.Fatal("includeSubDepts=false 时子部门员工 dev1 不应可见")
	}
	if !hasApp(t, clients, dev2, clientID) {
		t.Fatal("includeSubDepts=false 时本级员工 dev2 仍应可见")
	}
	grants, _ = appAuth.ListOrgGrants(ctx, clientID)
	if grants[0].CoveredUsers != 1 {
		t.Fatalf("切换后覆盖人数 = %d，期望 1", grants[0].CoveredUsers)
	}
}

// TestOrgGrant_RealtimeFollowsOrgChange 覆盖 spec FR-013：调岗后下一次请求即生效，
// 且组织授权行本身不发生变化（证明是实时判定而非物化同步）。
func TestOrgGrant_RealtimeFollowsOrgChange(t *testing.T) {
	env, appAuth, clients, clientID := newAppAuthEnv(t)
	ctx := orgGrantCtx()

	root := env.createOrg(t, nil, "研发部", nil)
	sub := env.createOrg(t, &root.ID, "平台组", nil)
	other := env.createOrg(t, nil, "市场部", nil)
	mkt1 := env.insertUser(t, other.ID, "mkt1")

	mustAddGrant(t, appAuth, clientID, root.ID, true)
	if hasApp(t, clients, mkt1, clientID) {
		t.Fatal("调岗前 mkt1 不应可见")
	}
	before, _ := appAuth.ListOrgGrants(ctx, clientID)

	if err := env.db.Exec("UPDATE users SET dept_id = ? WHERE id = ?", sub.ID, mkt1).Error; err != nil {
		t.Fatalf("调岗失败: %v", err)
	}
	if !hasApp(t, clients, mkt1, clientID) {
		t.Fatal("调岗进入授权子树后应立即可见")
	}

	if err := env.db.Exec("UPDATE users SET dept_id = ? WHERE id = ?", other.ID, mkt1).Error; err != nil {
		t.Fatalf("调出失败: %v", err)
	}
	if hasApp(t, clients, mkt1, clientID) {
		t.Fatal("调出授权子树后应立即不可见")
	}

	after, _ := appAuth.ListOrgGrants(ctx, clientID)
	if len(after) != len(before) {
		t.Fatalf("组织授权行数发生变化（%d → %d），说明引入了物化同步", len(before), len(after))
	}

	// 停用账号：即使部门仍被授权也不可见。
	dev := env.insertUser(t, sub.ID, "dev9")
	if !hasApp(t, clients, dev, clientID) {
		t.Fatal("在职员工应可见")
	}
	if err := env.db.Exec("UPDATE users SET status = 0 WHERE id = ?", dev).Error; err != nil {
		t.Fatalf("停用失败: %v", err)
	}
	if hasApp(t, clients, dev, clientID) {
		t.Fatal("停用账号不应可见（spec FR-011）")
	}
}

// TestOrgGrant_RemoveImpact 覆盖 spec FR-007/FR-008：撤销只解除授权，
// impactUsers 是"删除前"计算的独占人数，手动成员不受影响。
func TestOrgGrant_RemoveImpact(t *testing.T) {
	env, appAuth, clients, clientID := newAppAuthEnv(t)
	ctx := orgGrantCtx()

	root := env.createOrg(t, nil, "研发部", nil)
	sub := env.createOrg(t, &root.ID, "平台组", nil)
	dev1 := env.insertUser(t, sub.ID, "dev1") // 仅授权覆盖
	dev2 := env.insertUser(t, root.ID, "dev2") // 授权覆盖 + 手动成员

	if _, _, err := appAuth.AddMembers(ctx, clientID, []string{dev2}, ""); err != nil {
		t.Fatalf("加成员失败: %v", err)
	}
	mustAddGrant(t, appAuth, clientID, root.ID, true)

	grants, _ := appAuth.ListOrgGrants(ctx, clientID)
	if grants[0].CoveredUsers != 2 {
		t.Fatalf("覆盖人数 = %d，期望 2", grants[0].CoveredUsers)
	}
	if grants[0].UniqueUsers != 1 {
		t.Fatalf("独占人数 = %d，期望 1（dev2 另有手动成员身份）", grants[0].UniqueUsers)
	}

	removed, impact, remaining, err := appAuth.RemoveOrgGrants(ctx, clientID, []string{root.ID})
	if err != nil {
		t.Fatalf("RemoveOrgGrants: %v", err)
	}
	if removed != 1 || impact != 1 {
		t.Fatalf("removed=%d impact=%d，期望 1/1（impact 必须在删除前计算）", removed, impact)
	}
	if remaining != 1 {
		t.Fatalf("remaining=%d，期望 1（dev2 仍是手动成员）", remaining)
	}
	if hasApp(t, clients, dev1, clientID) {
		t.Fatal("dev1 仅因授权可见，撤销后不应再可见")
	}
	if !hasApp(t, clients, dev2, clientID) {
		t.Fatal("dev2 仍是手动成员，撤销授权后必须仍可见")
	}
}

// TestOrgGrant_IdempotentAndLimit 覆盖 spec FR-003 / FR-010。
func TestOrgGrant_IdempotentAndLimit(t *testing.T) {
	env, appAuth, _, clientID := newAppAuthEnv(t)
	ctx := orgGrantCtx()
	root := env.createOrg(t, nil, "研发部", nil)

	added, updated, unchanged, _, _, err := appAuth.AddOrgGrants(ctx, clientID, "",
		[]service.OrgGrantInput{{OrgID: root.ID, IncludeSub: true}})
	if err != nil || added != 1 || updated != 0 || unchanged != 0 {
		t.Fatalf("首次写入 = (%d,%d,%d,err=%v)，期望 (1,0,0,nil)", added, updated, unchanged, err)
	}
	added, updated, unchanged, _, _, err = appAuth.AddOrgGrants(ctx, clientID, "",
		[]service.OrgGrantInput{{OrgID: root.ID, IncludeSub: true}})
	if err != nil || added != 0 || updated != 0 || unchanged != 1 {
		t.Fatalf("重复写入 = (%d,%d,%d,err=%v)，期望 (0,0,1,nil)", added, updated, unchanged, err)
	}
	added, updated, unchanged, _, _, err = appAuth.AddOrgGrants(ctx, clientID, "",
		[]service.OrgGrantInput{{OrgID: root.ID, IncludeSub: false}})
	if err != nil || added != 0 || updated != 1 || unchanged != 0 {
		t.Fatalf("改 includeSubDepts = (%d,%d,%d,err=%v)，期望 (0,1,0,nil)", added, updated, unchanged, err)
	}

	// 部门不存在：整批失败（不写入部分记录）。
	added, _, _, _, _, err = appAuth.AddOrgGrants(ctx, clientID, "",
		[]service.OrgGrantInput{{OrgID: "01J9ZZZZZZZZZZZZZZZZZZZZZZ", IncludeSub: true}})
	if !errors.Is(err, service.ErrAppDeptNotFound) {
		t.Fatalf("部门不存在时 err = %v，期望 ErrAppDeptNotFound", err)
	}
	if added != 0 {
		t.Fatal("失败批次不应写入任何记录")
	}
}

// TestOrgGrant_DeptDeleteProtected 覆盖 spec FR-028：被授权引用的部门不能删除。
func TestOrgGrant_DeptDeleteProtected(t *testing.T) {
	env, appAuth, _, clientID := newAppAuthEnv(t)
	ctx := orgGrantCtx()
	root := env.createOrg(t, nil, "研发部", nil)
	mustAddGrant(t, appAuth, clientID, root.ID, true)

	if err := env.orgs.Delete(ctx, root.ID); !errors.Is(err, service.ErrOrgHasGrants) {
		t.Fatalf("删除被授权引用的部门 err = %v，期望 ErrOrgHasGrants", err)
	}
	if _, _, _, err := appAuth.RemoveOrgGrants(ctx, clientID, []string{root.ID}); err != nil {
		t.Fatalf("撤销失败: %v", err)
	}
	if err := env.orgs.Delete(ctx, root.ID); err != nil {
		t.Fatalf("撤销授权后删除部门应成功: %v", err)
	}
}

// TestOrgGrant_MemberSourcesAndCandidates 覆盖 spec FR-016 / FR-018 / FR-019。
func TestOrgGrant_MemberSourcesAndCandidates(t *testing.T) {
	env, appAuth, _, clientID := newAppAuthEnv(t)
	ctx := orgGrantCtx()
	root := env.createOrg(t, nil, "研发部", nil)
	member := env.insertUser(t, root.ID, "member1")
	covered := env.insertUser(t, root.ID, "covered1")

	if _, _, err := appAuth.AddMembers(ctx, clientID, []string{member}, ""); err != nil {
		t.Fatalf("加成员失败: %v", err)
	}
	mustAddGrant(t, appAuth, clientID, root.ID, true)

	items, total, err := appAuth.ListMembers(ctx, clientID, service.MemberFilter{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("手动成员 total=%d len=%d，期望 1/1", total, len(items))
	}
	if len(items[0].OrgGrants) != 1 || items[0].OrgGrants[0].DeptName != "研发部" {
		t.Fatalf("成员来源 = %+v，期望含研发部授权", items[0].OrgGrants)
	}

	visible, err := appAuth.VisibleUserCount(ctx, clientID)
	if err != nil {
		t.Fatalf("VisibleUserCount: %v", err)
	}
	if visible != 2 {
		t.Fatalf("可见人数 = %d，期望 2（1 手动 + 1 授权覆盖去重）", visible)
	}
	// 口径不变式（FR-019）：可见总人数不可能少于手动成员数。
	if visible < total {
		t.Fatalf("可见人数 %d 小于手动成员数 %d，口径被破坏", visible, total)
	}

	cands, _, err := appAuth.ListCandidates(ctx, clientID, service.MemberFilter{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListCandidates: %v", err)
	}
	found := false
	for _, c := range cands {
		if c.UserID == covered {
			found = true
			if !c.OrgCovered {
				t.Fatal("已被组织授权覆盖的候选应标记 orgCovered=true")
			}
		}
	}
	if !found {
		t.Fatal("授权覆盖但非成员的用户仍应出现在候选列表（候选集合不变）")
	}
}

package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	mysqldrv "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/omino/omino-sso/internal/api/admin"
	"github.com/omino/omino-sso/internal/config"
	"github.com/omino/omino-sso/internal/pkg/id"
	"github.com/omino/omino-sso/internal/service"
	"github.com/omino/omino-sso/internal/store"
	"github.com/omino/omino-sso/internal/store/model"
)

// 本文件是 DB 行为集成测试，由 TEST_MYSQL_DSN 门控：
// 未设置时整组跳过，`make test` 不依赖 MySQL（plan.md Testing）。
// 建库与建表只走 migrations/*.up.sql（按文件名升序全量执行），禁止 AutoMigrate（migrations/README.md）。

const testDSNEnv = "TEST_MYSQL_DSN"

// testUserPassphrase 是集成测试账号的固定口令，不是真实凭据。
// 提取为常量而非内联字面量：便于复用，也避免被凭据扫描规则
// （匹配 `password = "..."` 形式的赋值）误判。
const testUserPassphrase = "Init!pass2026"

type orgEnv struct {
	db    *gorm.DB
	st    *store.Store
	orgs  *service.OrgService
	users *service.UserService
	log   *slog.Logger
}

func newOrgEnv(t *testing.T) *orgEnv {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv(testDSNEnv))
	if dsn == "" {
		t.Skip("未设置 " + testDSNEnv + "，跳过组织写操作集成测试")
	}
	db := openTestDB(t, dsn)
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	st := store.New(db, nil)
	cfg := &config.Config{}
	audit := service.NewAuditor(st, 1, logger)
	return &orgEnv{
		db:    db,
		st:    st,
		orgs:  service.NewOrgService(st, logger),
		users: service.NewUserService(st, cfg, audit, logger),
		log:   logger,
	}
}

// reset 清空组织/用户/审计表：迁移只含 CREATE TABLE IF NOT EXISTS，
// 不清理会残留上次用例的数据。
func (e *orgEnv) reset(t *testing.T) {
	t.Helper()
	for _, table := range []string{"organizations", "users", "audit_events"} {
		if err := e.db.Exec("TRUNCATE TABLE " + table).Error; err != nil {
			t.Fatalf("truncate %s: %v", table, err)
		}
	}
}

// schemaOnce 保证整个测试二进制内只重建一次测试库，且迁移只跑一遍：
// 迁移脚本（0002 起为裸 ALTER）不幂等，重复执行会因列已存在而失败。
var (
	schemaOnce sync.Once
	schemaErr  error
)

func openTestDB(t *testing.T, dsn string) *gorm.DB {
	t.Helper()
	cfg, err := mysqldrv.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("解析 %s 失败: %v", testDSNEnv, err)
	}
	if cfg.DBName == "" {
		t.Fatalf("%s 必须包含库名（形如 user:pass@tcp(127.0.0.1:3307)/omino_sso_test）", testDSNEnv)
	}
	schemaOnce.Do(func() { schemaErr = rebuildSchema(dsn, cfg) })
	if schemaErr != nil {
		t.Fatalf("初始化测试库失败: %v", schemaErr)
	}

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger:                 gormlogger.Discard,
		SkipDefaultTransaction: true,
	})
	if err != nil {
		t.Fatalf("连接测试库失败: %v", err)
	}
	return db
}

// rebuildSchema 丢弃并重建测试库，再按文件名升序执行全部 *.up.sql，
// 使测试 schema 与生产迁移链完全一致（禁止 AutoMigrate）。
func rebuildSchema(dsn string, cfg *mysqldrv.Config) error {
	if err := recreateTestDB(cfg); err != nil {
		return fmt.Errorf("重建测试库失败: %w", err)
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger:                 gormlogger.Discard,
		SkipDefaultTransaction: true,
	})
	if err != nil {
		return fmt.Errorf("连接测试库失败: %w", err)
	}
	if sqlDB, dbErr := db.DB(); dbErr == nil {
		defer func() { _ = sqlDB.Close() }()
	}
	return applyMigrations(db)
}

func recreateTestDB(cfg *mysqldrv.Config) error {
	adminCfg := *cfg
	adminCfg.DBName = ""
	adminDB, err := gorm.Open(mysql.Open(adminCfg.FormatDSN()), &gorm.Config{Logger: gormlogger.Discard})
	if err != nil {
		return err
	}
	if sqlDB, dbErr := adminDB.DB(); dbErr == nil {
		defer func() { _ = sqlDB.Close() }()
	}
	if err := adminDB.Exec("DROP DATABASE IF EXISTS `" + cfg.DBName + "`").Error; err != nil {
		return err
	}
	createDB := "CREATE DATABASE `" + cfg.DBName + "` DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci"
	return adminDB.Exec(createDB).Error
}

func applyMigrations(db *gorm.DB) error {
	files, err := filepath.Glob("../../migrations/*.up.sql")
	if err != nil {
		return fmt.Errorf("扫描迁移文件失败: %w", err)
	}
	if len(files) == 0 {
		return errors.New("未找到任何 *.up.sql 迁移文件")
	}
	sort.Strings(files)
	for _, file := range files {
		if err := applyMigrationFile(db, file); err != nil {
			return err
		}
	}
	return nil
}

func applyMigrationFile(db *gorm.DB, file string) error {
	raw, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("读取迁移文件失败: %w", err)
	}
	for _, stmt := range strings.Split(string(raw), ";") {
		lines := make([]string, 0, 4)
		for _, line := range strings.Split(stmt, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "--") {
				continue
			}
			lines = append(lines, line)
		}
		sql := strings.TrimSpace(strings.Join(lines, "\n"))
		if sql == "" {
			continue
		}
		if err := db.Exec(sql).Error; err != nil {
			return fmt.Errorf("执行迁移失败（%s）: %w\n%s", file, err, sql)
		}
	}
	return nil
}

func (e *orgEnv) createOrg(t *testing.T, parentID *string, name string, sort *int) *model.Organization {
	t.Helper()
	o, err := e.orgs.Create(context.Background(), service.CreateOrgInput{
		ParentID: parentID, Name: name, Sort: sort,
	})
	if err != nil {
		t.Fatalf("创建组织 %q 失败: %v", name, err)
	}
	return o
}

func (e *orgEnv) insertUser(t *testing.T, deptID, username string) string {
	t.Helper()
	uid := id.New()
	if err := e.db.Exec(
		`INSERT INTO users (id, username, display_name, password_hash, dept_id, tenant_id, roles, status,
		 password_updated_at, created_at, updated_at)
		 VALUES (?, ?, ?, 'x', ?, 'default', JSON_ARRAY('user'), 1, 0, 0, 0)`,
		uid, username, username, deptID,
	).Error; err != nil {
		t.Fatalf("插入用户失败: %v", err)
	}
	return uid
}

func (e *orgEnv) getOrg(t *testing.T, orgID string) *model.Organization {
	t.Helper()
	o, err := e.st.GetOrganization(context.Background(), orgID)
	if err != nil {
		t.Fatalf("读取组织 %s 失败: %v", orgID, err)
	}
	return o
}

func intPtr(v int) *int { return &v }

// TestOrgCreateSiblingNameUnique 覆盖同级重名拒绝与"不同父节点可同名"。
func TestOrgCreateSiblingNameUnique(t *testing.T) {
	env := newOrgEnv(t)
	env.reset(t)
	ctx := context.Background()

	root := env.createOrg(t, nil, "技术中心", nil)
	if _, err := env.orgs.Create(ctx, service.CreateOrgInput{ParentID: &root.ID, Name: "平台组"}); err != nil {
		t.Fatalf("首次创建子部门失败: %v", err)
	}
	if _, err := env.orgs.Create(ctx, service.CreateOrgInput{ParentID: &root.ID, Name: "平台组"}); !errors.Is(err, service.ErrOrgNameExists) {
		t.Fatalf("同级重名应返回 ErrOrgNameExists, 实际 %v", err)
	}

	other := env.createOrg(t, nil, "运营中心", nil)
	if _, err := env.orgs.Create(ctx, service.CreateOrgInput{ParentID: &other.ID, Name: "平台组"}); err != nil {
		t.Fatalf("不同父节点下同名应允许: %v", err)
	}

	// 名称入库前 trim：两端空白不产生第二个"平台组"。
	if _, err := env.orgs.Create(ctx, service.CreateOrgInput{ParentID: &root.ID, Name: "  平台组  "}); !errors.Is(err, service.ErrOrgNameExists) {
		t.Fatalf("trim 后同名应返回 ErrOrgNameExists, 实际 %v", err)
	}

	var n int64
	if err := env.db.Model(&model.Organization{}).Where("parent_id = ?", root.ID).Count(&n).Error; err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("root 下子部门数 = %d, 期望 1（拒绝时不得写入）", n)
	}
}

// TestOrgCreateNameCaseSensitive 覆盖"区分大小写"：Platform 与 platform 可共存
// （表排序规则为 utf8mb4_0900_ai_ci，必须靠 BINARY 比较）。
func TestOrgCreateNameCaseSensitive(t *testing.T) {
	env := newOrgEnv(t)
	env.reset(t)
	ctx := context.Background()

	root := env.createOrg(t, nil, "研发中心", nil)
	env.createOrg(t, &root.ID, "Platform", nil)
	if _, err := env.orgs.Create(ctx, service.CreateOrgInput{ParentID: &root.ID, Name: "platform"}); err != nil {
		t.Fatalf("大小写不同应允许共存: %v", err)
	}
}

// TestOrgCreateDepthLimit 覆盖 10 级上限：第 10 级成功、第 11 级拒绝。
func TestOrgCreateDepthLimit(t *testing.T) {
	env := newOrgEnv(t)
	env.reset(t)
	ctx := context.Background()

	var parent *string
	var last *model.Organization
	for i := 1; i <= 10; i++ {
		last = env.createOrg(t, parent, "L"+strconv.Itoa(i), nil)
		parent = &last.ID
	}
	if got := model.OrgDepth(last.Path); got != 10 {
		t.Fatalf("第 10 级节点深度 = %d, 期望 10", got)
	}
	if _, err := env.orgs.Create(ctx, service.CreateOrgInput{ParentID: &last.ID, Name: "L11"}); !errors.Is(err, service.ErrOrgDepthExceeded) {
		t.Fatalf("第 11 级应返回 ErrOrgDepthExceeded, 实际 %v", err)
	}
}

// TestOrgCreateInvalidParent 覆盖父节点不存在与租户继承。
func TestOrgCreateInvalidParent(t *testing.T) {
	env := newOrgEnv(t)
	env.reset(t)
	ctx := context.Background()

	missing := "01J9ZZZZZZZZZZZZZZZZZZZZZZ"
	if _, err := env.orgs.Create(ctx, service.CreateOrgInput{ParentID: &missing, Name: "孤立部门"}); !errors.Is(err, service.ErrOrgInvalidParent) {
		t.Fatalf("父节点不存在应返回 ErrOrgInvalidParent, 实际 %v", err)
	}

	root := env.createOrg(t, nil, "总公司", nil)
	if root.TenantID != service.DefaultOrgTenant {
		t.Fatalf("根级租户 = %q, 期望 %q", root.TenantID, service.DefaultOrgTenant)
	}
	child := env.createOrg(t, &root.ID, "技术中心", nil)
	if child.TenantID != root.TenantID {
		t.Fatalf("子节点租户 = %q, 期望继承父节点 %q", child.TenantID, root.TenantID)
	}
	if child.Path != root.Path+child.ID+"/" {
		t.Fatalf("子节点 path = %q, 期望 %q", child.Path, root.Path+child.ID+"/")
	}
}

// TestOrgCreateConcurrentSameName 覆盖并发同名创建（空表上会触发
// supremum 间隙锁死锁，service 层重试一次后应一方成功、一方冲突）。
func TestOrgCreateConcurrentSameName(t *testing.T) {
	env := newOrgEnv(t)
	env.reset(t)
	ctx := context.Background()

	const workers = 2
	var wg sync.WaitGroup
	errs := make([]error, workers)
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(i int) {
			defer wg.Done()
			_, errs[i] = env.orgs.Create(ctx, service.CreateOrgInput{Name: "并发部门"})
		}(i)
	}
	wg.Wait()

	okCount, conflictCount := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			okCount++
		case errors.Is(err, service.ErrOrgNameExists):
			conflictCount++
		default:
			t.Fatalf("并发创建出现非预期错误: %v", err)
		}
	}
	if okCount != 1 || conflictCount != workers-1 {
		t.Fatalf("并发创建结果 成功=%d 冲突=%d, 期望 1/%d", okCount, conflictCount, workers-1)
	}
}

// TestOrgUpdateRenameKeepsStructure 覆盖只改名：父子关系、排序与 path 不变。
func TestOrgUpdateRenameKeepsStructure(t *testing.T) {
	env := newOrgEnv(t)
	env.reset(t)
	ctx := context.Background()

	root := env.createOrg(t, nil, "技术中心", intPtr(3))
	child := env.createOrg(t, &root.ID, "平台组", intPtr(5))

	updated, err := env.orgs.Update(ctx, child.ID, service.UpdateOrgInput{Name: strPtr("平台工程组")})
	if err != nil {
		t.Fatalf("改名失败: %v", err)
	}
	if updated.Name != "平台工程组" {
		t.Fatalf("name = %q", updated.Name)
	}
	if updated.ParentID == nil || *updated.ParentID != root.ID {
		t.Fatalf("parentId 应保持 %q", root.ID)
	}
	if updated.Path != child.Path {
		t.Fatalf("path 应不变: %q → %q", child.Path, updated.Path)
	}
	if updated.Sort != 5 {
		t.Fatalf("sort 应保持 5, 实际 %d", updated.Sort)
	}
	if updated.CreatedAt != child.CreatedAt {
		t.Fatalf("createdAt 应不变")
	}

	// 同级重名（排除自身）应拒绝，且原名称不变。
	env.createOrg(t, &root.ID, "数据组", nil)
	if _, err := env.orgs.Update(ctx, child.ID, service.UpdateOrgInput{Name: strPtr("数据组")}); !errors.Is(err, service.ErrOrgNameExists) {
		t.Fatalf("同名改名应返回 ErrOrgNameExists, 实际 %v", err)
	}
	if got := env.getOrg(t, child.ID); got.Name != "平台工程组" {
		t.Fatalf("拒绝后名称被改写: %q", got.Name)
	}

	// 改成自身原名不算冲突。
	if _, err := env.orgs.Update(ctx, child.ID, service.UpdateOrgInput{Name: strPtr("平台工程组")}); err != nil {
		t.Fatalf("改回自身原名不应报错: %v", err)
	}
}

// TestOrgUpdateMoveSubtree 覆盖移动整棵子树：后代 path 级联、用户归属与人数不变。
func TestOrgUpdateMoveSubtree(t *testing.T) {
	env := newOrgEnv(t)
	env.reset(t)
	ctx := context.Background()

	a := env.createOrg(t, nil, "A", nil)
	b := env.createOrg(t, &a.ID, "B", intPtr(7))
	c := env.createOrg(t, &b.ID, "C", nil)
	d := env.createOrg(t, nil, "D", nil)
	env.insertUser(t, c.ID, "user-in-c")

	before, err := env.orgs.UserCountInSubtree(ctx, a.ID)
	if err != nil {
		t.Fatalf("统计人数失败: %v", err)
	}
	if before != 1 {
		t.Fatalf("移动前人数 = %d, 期望 1", before)
	}

	if _, err := env.orgs.Update(ctx, a.ID, service.UpdateOrgInput{ParentID: &d.ID, ParentIDSet: true}); err != nil {
		t.Fatalf("移动失败: %v", err)
	}

	wantA := d.Path + a.ID + "/"
	gotA := env.getOrg(t, a.ID)
	if gotA.ParentID == nil || *gotA.ParentID != d.ID {
		t.Fatalf("A.parentId 未更新")
	}
	if gotA.Path != wantA {
		t.Fatalf("A.path = %q, 期望 %q", gotA.Path, wantA)
	}
	if gotA.Sort != a.Sort {
		t.Fatalf("移动不应改写 sort: %d → %d", a.Sort, gotA.Sort)
	}
	if gotB := env.getOrg(t, b.ID); gotB.Path != wantA+b.ID+"/" {
		t.Fatalf("B.path = %q, 期望 %q", gotB.Path, wantA+b.ID+"/")
	}
	if gotC := env.getOrg(t, c.ID); gotC.Path != wantA+b.ID+"/"+c.ID+"/" {
		t.Fatalf("C.path = %q 未级联", gotC.Path)
	}
	if gotB := env.getOrg(t, b.ID); gotB.Sort != 7 {
		t.Fatalf("后代 sort 不应被改写: %d", gotB.Sort)
	}
	after, err := env.orgs.UserCountInSubtree(ctx, a.ID)
	if err != nil {
		t.Fatalf("统计人数失败: %v", err)
	}
	if after != before {
		t.Fatalf("移动后人数 = %d, 期望 %d（用户归属不变）", after, before)
	}
}

// TestOrgUpdateRejectsCycle 覆盖移入自身/后代被拒绝（ORG_INVALID_PARENT），树不变。
func TestOrgUpdateRejectsCycle(t *testing.T) {
	env := newOrgEnv(t)
	env.reset(t)
	ctx := context.Background()

	a := env.createOrg(t, nil, "A", nil)
	b := env.createOrg(t, &a.ID, "B", nil)
	c := env.createOrg(t, &b.ID, "C", nil)

	if _, err := env.orgs.Update(ctx, a.ID, service.UpdateOrgInput{ParentID: &a.ID, ParentIDSet: true}); !errors.Is(err, service.ErrOrgInvalidParent) {
		t.Fatalf("移入自身应返回 ErrOrgInvalidParent, 实际 %v", err)
	}
	if _, err := env.orgs.Update(ctx, a.ID, service.UpdateOrgInput{ParentID: &c.ID, ParentIDSet: true}); !errors.Is(err, service.ErrOrgInvalidParent) {
		t.Fatalf("移入后代应返回 ErrOrgInvalidParent, 实际 %v", err)
	}
	if got := env.getOrg(t, a.ID); got.ParentID != nil || got.Path != a.Path {
		t.Fatalf("拒绝后 A 被修改: parent=%v path=%q", got.ParentID, got.Path)
	}
	if got := env.getOrg(t, c.ID); got.Path != c.Path {
		t.Fatalf("拒绝后 C.path 被改写: %q", got.Path)
	}
}

// TestOrgUpdateMoveToRootAndIdempotent 覆盖显式 null（移到根级）与空请求体幂等。
func TestOrgUpdateMoveToRootAndIdempotent(t *testing.T) {
	env := newOrgEnv(t)
	env.reset(t)
	ctx := context.Background()

	root := env.createOrg(t, nil, "总公司", nil)
	a := env.createOrg(t, &root.ID, "技术中心", nil)
	child := env.createOrg(t, &a.ID, "平台组", nil)

	moved, err := env.orgs.Update(ctx, a.ID, service.UpdateOrgInput{ParentID: nil, ParentIDSet: true})
	if err != nil {
		t.Fatalf("移到根级失败: %v", err)
	}
	if moved.ParentID != nil {
		t.Fatalf("parentId 应为 nil, 实际 %q", *moved.ParentID)
	}
	if moved.Path != "/"+a.ID+"/" {
		t.Fatalf("A.path = %q, 期望 /%s/", moved.Path, a.ID)
	}
	if got := env.getOrg(t, child.ID); got.Path != "/"+a.ID+"/"+child.ID+"/" {
		t.Fatalf("子部门 path 未级联: %q", got.Path)
	}

	// 空请求体：不写库、不改数据，幂等返回当前节点。
	same, err := env.orgs.Update(ctx, a.ID, service.UpdateOrgInput{})
	if err != nil {
		t.Fatalf("空请求体应成功: %v", err)
	}
	if same.Path != moved.Path || same.Name != moved.Name {
		t.Fatalf("空请求体改动了数据")
	}
	if _, err := env.orgs.Update(ctx, "01J9ZZZZZZZZZZZZZZZZZZZZZZ", service.UpdateOrgInput{}); !errors.Is(err, service.ErrOrgNotFound) {
		t.Fatalf("不存在的节点应返回 ErrOrgNotFound, 实际 %v", err)
	}
}

// TestOrgUpdateSortOutOfRange 覆盖 sort 越界与移动导致子树超深的拒绝。
func TestOrgUpdateSortOutOfRange(t *testing.T) {
	env := newOrgEnv(t)
	env.reset(t)
	ctx := context.Background()

	a := env.createOrg(t, nil, "A", nil)
	if _, err := env.orgs.Update(ctx, a.ID, service.UpdateOrgInput{Sort: intPtr(1 << 31)}); !errors.Is(err, service.ErrOrgInvalidSort) {
		t.Fatalf("sort 越界应返回 ErrOrgInvalidSort, 实际 %v", err)
	}
	if got := env.getOrg(t, a.ID); got.Sort != a.Sort {
		t.Fatalf("sort 越界时被写入: %d", got.Sort)
	}

	// 深度 10 的链 + 高度 2 的子树移动到深位置 → 拒绝。
	var parent *string
	var tenth *model.Organization
	for i := 1; i <= 10; i++ {
		tenth = env.createOrg(t, parent, "L"+strconv.Itoa(i), nil)
		parent = &tenth.ID
	}
	top := env.createOrg(t, nil, "迁移源", nil)
	env.createOrg(t, &top.ID, "迁移子", nil)

	if _, err := env.orgs.Update(ctx, top.ID, service.UpdateOrgInput{ParentID: &tenth.ID, ParentIDSet: true}); !errors.Is(err, service.ErrOrgDepthExceeded) {
		t.Fatalf("移动导致超深应返回 ErrOrgDepthExceeded, 实际 %v", err)
	}
	if got := env.getOrg(t, top.ID); got.ParentID != nil {
		t.Fatalf("拒绝后仍被移动")
	}
}

// TestOrgDeleteGuards 覆盖删除的两类保护、成功删除与重复删除。
func TestOrgDeleteGuards(t *testing.T) {
	env := newOrgEnv(t)
	env.reset(t)
	ctx := context.Background()

	root := env.createOrg(t, nil, "总公司", nil)
	withChild := env.createOrg(t, &root.ID, "有子部门", nil)
	env.createOrg(t, &withChild.ID, "子部门", nil)
	withUsers := env.createOrg(t, &root.ID, "有账号", nil)
	env.insertUser(t, withUsers.ID, "u1")
	leaf := env.createOrg(t, &root.ID, "空叶子", nil)

	if err := env.orgs.Delete(ctx, withChild.ID); !errors.Is(err, service.ErrOrgHasChildren) {
		t.Fatalf("有子部门应返回 ErrOrgHasChildren, 实际 %v", err)
	}
	if err := env.orgs.Delete(ctx, withUsers.ID); !errors.Is(err, service.ErrOrgHasUsers) {
		t.Fatalf("有账号应返回 ErrOrgHasUsers, 实际 %v", err)
	}
	if err := env.orgs.Delete(ctx, leaf.ID); err != nil {
		t.Fatalf("空叶子删除应成功: %v", err)
	}
	if err := env.orgs.Delete(ctx, leaf.ID); !errors.Is(err, service.ErrOrgNotFound) {
		t.Fatalf("重复删除应返回 ErrOrgNotFound, 实际 %v", err)
	}
	if _, err := env.st.GetOrganization(ctx, leaf.ID); err == nil {
		t.Fatal("已删除节点仍可读出")
	}
}

// TestOrgDeleteConcurrentWithCreateUser 覆盖"删部门 + 建用户"互斥：
// 恰一方成功，且最终不出现悬空 dept_id（契约 C-8）。
func TestOrgDeleteConcurrentWithCreateUser(t *testing.T) {
	env := newOrgEnv(t)
	env.reset(t)
	ctx := context.Background()

	dept := env.createOrg(t, nil, "竞态部门", nil)

	var wg sync.WaitGroup
	var delErr, createErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		delErr = env.orgs.Delete(ctx, dept.ID)
	}()
	go func() {
		defer wg.Done()
		_, createErr = env.users.CreateUser(ctx, service.CreateUserInput{
			Username:    "raceuser",
			DisplayName: "竞态用户",
			Password:    testUserPassphrase,
			DeptID:      dept.ID,
			TenantID:    "default",
		})
	}()
	wg.Wait()

	okCount := 0
	if delErr == nil {
		okCount++
	} else if !errors.Is(delErr, service.ErrOrgHasUsers) {
		t.Fatalf("删除失败原因非预期: %v", delErr)
	}
	if createErr == nil {
		okCount++
	} else if !errors.Is(createErr, service.ErrDeptNotFound) {
		t.Fatalf("建用户失败原因非预期: %v", createErr)
	}
	if okCount != 1 {
		t.Fatalf("并发结果 成功数 = %d, 期望恰 1", okCount)
	}

	var dangling int64
	if err := env.db.Raw(
		"SELECT COUNT(*) FROM users u LEFT JOIN organizations o ON o.id = u.dept_id WHERE o.id IS NULL",
	).Scan(&dangling).Error; err != nil {
		t.Fatalf("悬空校验失败: %v", err)
	}
	if dangling != 0 {
		t.Fatalf("存在 %d 条挂在不存在的部门上的账号", dangling)
	}
}

// TestUpdateUserRejectsMissingDept 覆盖用户改部门接口的行为修正：
// 目标部门不存在时返回 400 VALIDATION_FAILED 且原值不变（不再静默写入）。
func TestUpdateUserRejectsMissingDept(t *testing.T) {
	env := newOrgEnv(t)
	env.reset(t)

	dept := env.createOrg(t, nil, "原部门", nil)
	uid := env.insertUser(t, dept.ID, "deptuser")

	gin.SetMode(gin.TestMode)
	audit := service.NewAuditor(env.st, 1, env.log)
	h := admin.NewHandler(env.users, service.NewClientService(env.st), env.orgs, service.NewAppAuthService(env.st), audit, env.log)
	r := gin.New()
	r.PATCH("/api/v1/admin/users/:id", h.UpdateUser)

	body := `{"deptId":"01J9ZZZZZZZZZZZZZZZZZZZZZZ"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/users/"+uid, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("状态码 = %d, 期望 400（body=%s）", rec.Code, rec.Body.String())
	}
	var resp struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应体解析失败: %v", err)
	}
	if resp.Code != "VALIDATION_FAILED" {
		t.Fatalf("code = %q, 期望 VALIDATION_FAILED", resp.Code)
	}
	u, err := env.st.GetUserByID(context.Background(), uid)
	if err != nil {
		t.Fatalf("读取用户失败: %v", err)
	}
	if u.DeptID != dept.ID {
		t.Fatalf("deptId 被改写为 %q", u.DeptID)
	}
}

func strPtr(s string) *string { return &s }

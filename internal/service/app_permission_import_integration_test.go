package service_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/omino/omino-sso/internal/api/admin"
	"github.com/omino/omino-sso/internal/api/dto"
	"github.com/omino/omino-sso/internal/api/me"
	"github.com/omino/omino-sso/internal/api/middleware"
	"github.com/omino/omino-sso/internal/config"
	"github.com/omino/omino-sso/internal/pkg/id"
	"github.com/omino/omino-sso/internal/service"
	"github.com/omino/omino-sso/internal/service/notification"
	"github.com/omino/omino-sso/internal/store"
	"github.com/omino/omino-sso/internal/store/model"
)

// 权限点导入 / 导出的端到端行为（TEST_MYSQL_DSN 门控，specs/011-app-permission-import T009–T035）。
// 全部走真实 handler + 真库，覆盖：预览只读、事务写入、幂等、并发、审计口径、导出回导、两端一致。

type importEnv struct {
	*orgEnv
	admin    *gin.Engine
	portal   *gin.Engine
	clientID string
	clients  *service.ClientService
	appAuth  *service.AppAuthService
	meH      *me.Handler
}

type apiResp struct {
	Code    string          `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func newImportEnv(t *testing.T) *importEnv {
	t.Helper()
	env, appAuth, clients, clientID := newAppAuthEnv(t)
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{SessionTTL: time.Hour, SessionTTLRemember: 7 * 24 * time.Hour}
	audit := service.NewAuditor(env.st, 256, env.log)
	ctx, cancel := context.WithCancel(context.Background())
	go audit.Run(ctx)
	t.Cleanup(cancel)

	notif, err := notification.New(cfg, env.st, audit, env.log)
	if err != nil {
		t.Fatalf("构造验证码服务失败：%v", err)
	}
	files := service.NewFileService(env.st, store.NewNoopFileStore(), audit)
	adminH := admin.NewHandler(env.users, clients, env.orgs, appAuth, files, notif, audit, env.log)
	meH := me.NewHandler(cfg, env.users, service.NewSessionService(env.st, cfg), clients, files,
		service.NewMFAService(env.st, cfg, notif, audit, env.log), appAuth, adminH, audit, env.log)

	adminR := gin.New()
	adminR.POST("/api/v1/admin/clients/:id/permissions/import/validate", adminH.ValidatePermissionImport)
	adminR.POST("/api/v1/admin/clients/:id/permissions/import", adminH.ConfirmPermissionImport)
	adminR.GET("/api/v1/admin/clients/:id/permissions/export", adminH.ExportPermissions)

	portalR := gin.New()
	portalR.POST("/api/v1/me/apps/:id/permissions/import/validate", meH.AppValidatePermissionImport)
	portalR.POST("/api/v1/me/apps/:id/permissions/import", meH.AppConfirmPermissionImport)
	portalR.GET("/api/v1/me/apps/:id/permissions/export", meH.AppExportPermissions)

	return &importEnv{
		orgEnv: env, admin: adminR, portal: portalR, clientID: clientID,
		clients: clients, appAuth: appAuth, meH: meH,
	}
}

// confirmAsPortal 走门户「确认导入」的真实 handler（该路由不挂 AppAdmin 中间件，
// 鉴权在 handler 内完成）；user 为 nil 时模拟未登录。
func (e *importEnv) confirmAsPortal(t *testing.T, user *model.User, doc string) (int, apiResp) {
	t.Helper()
	r := gin.New()
	r.POST("/api/v1/me/apps/:id/permissions/import", func(c *gin.Context) {
		if user != nil {
			c.Set(middleware.CtxUser, user)
		}
		c.Next()
	}, e.meH.AppConfirmPermissionImport)
	return doJSON(t, r, http.MethodPost, "/api/v1/me/apps/"+e.clientID+"/permissions/import", doc)
}

// addAppAdmin 把用户加为应用成员并设为应用管理员（门户鉴权用例的夹具）。
func (e *importEnv) addAppAdmin(t *testing.T, userID string, isAdmin bool) {
	t.Helper()
	if _, _, err := e.appAuth.AddMembers(context.Background(), e.clientID, []string{userID}, ""); err != nil {
		t.Fatalf("添加成员失败：%v", err)
	}
	if isAdmin {
		if _, _, err := e.appAuth.SetMemberAdmin(context.Background(), e.clientID, userID, true); err != nil {
			t.Fatalf("设为应用管理员失败：%v", err)
		}
	}
}

/* ---------------- 请求与断言辅助 ---------------- */

func doJSON(t *testing.T, r *gin.Engine, method, path, body string) (int, apiResp) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	var out apiResp
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("响应不是统一响应包：%v（body=%s）", err, rec.Body.String())
		}
	}
	return rec.Code, out
}

func (e *importEnv) adminPath(suffix string) string {
	return "/api/v1/admin/clients/" + e.clientID + "/permissions" + suffix
}

func (e *importEnv) portalPath(suffix string) string {
	return "/api/v1/me/apps/" + e.clientID + "/permissions" + suffix
}

func (e *importEnv) validate(t *testing.T, doc string, source string) (int, dto.PermissionImportPreview) {
	t.Helper()
	path := e.adminPath("/import/validate")
	if source != "" {
		path += "?source=" + source
	}
	status, resp := doJSON(t, e.admin, http.MethodPost, path, doc)
	var preview dto.PermissionImportPreview
	if status == http.StatusOK {
		if err := json.Unmarshal(resp.Data, &preview); err != nil {
			t.Fatalf("预览响应解析失败：%v", err)
		}
	}
	return status, preview
}

func (e *importEnv) confirm(t *testing.T, doc string, source string) (int, apiResp, dto.PermissionImportResult) {
	t.Helper()
	path := e.adminPath("/import")
	if source != "" {
		path += "?source=" + source
	}
	status, resp := doJSON(t, e.admin, http.MethodPost, path, doc)
	var result dto.PermissionImportResult
	if status == http.StatusOK {
		if err := json.Unmarshal(resp.Data, &result); err != nil {
			t.Fatalf("导入响应解析失败：%v", err)
		}
	}
	return status, resp, result
}

func (e *importEnv) permissionRows(t *testing.T, clientID string) []model.ClientPermission {
	t.Helper()
	rows := []model.ClientPermission{}
	if err := e.db.Where("client_id = ?", clientID).Order("code ASC").Find(&rows).Error; err != nil {
		t.Fatalf("读取权限点失败：%v", err)
	}
	return rows
}

func (e *importEnv) auditCount(t *testing.T, eventType string) int64 {
	t.Helper()
	var n int64
	if err := e.db.Model(&model.AuditEvent{}).Where("event_type = ?", eventType).Count(&n).Error; err != nil {
		t.Fatalf("统计审计事件失败：%v", err)
	}
	return n
}

func (e *importEnv) waitAuditCount(t *testing.T, eventType string, want int64) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if got := e.auditCount(t, eventType); got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("审计事件 %s 数量未达到 %d（当前 %d）", eventType, want, e.auditCount(t, eventType))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (e *importEnv) latestAuditDetail(t *testing.T, eventType string) map[string]any {
	t.Helper()
	var ev model.AuditEvent
	if err := e.db.Where("event_type = ?", eventType).Order("created_at DESC").Take(&ev).Error; err != nil {
		t.Fatalf("读取审计事件失败：%v", err)
	}
	if ev.Detail == nil {
		t.Fatal("审计事件缺少 detail")
	}
	var detail map[string]any
	if err := json.Unmarshal([]byte(*ev.Detail), &detail); err != nil {
		t.Fatalf("detail 不是合法 JSON：%v（%s）", err, *ev.Detail)
	}
	return detail
}

/* ---------------- 用例文档 ---------------- */

func flatImportDoc(n int) string {
	var sb strings.Builder
	sb.WriteString(`{"version":1,"mode":"merge","permissions":[`)
	for i := 0; i < n; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, `{"code":"perm.%03d","name":"权限%03d"}`, i, i)
	}
	sb.WriteString(`]}`)
	return sb.String()
}

const threeLevelDoc = `{"version":1,"mode":"merge","permissions":[
  {"code":"order","name":"订单","children":[
    {"code":"order.read","name":"查询","children":[
      {"code":"order.read.self","name":"查自己"},
      {"code":"order.read.all","name":"查全部"}
    ]},
    {"code":"order.write","name":"创建","children":[
      {"code":"order.write.self","name":"建自己"},
      {"code":"order.write.all","name":"建全部"}
    ]}
  ]},
  {"code":"pay","name":"支付","children":[
    {"code":"pay.read","name":"查询","children":[
      {"code":"pay.read.self","name":"查自己"},
      {"code":"pay.read.all","name":"查全部"}
    ]},
    {"code":"pay.write","name":"创建","children":[
      {"code":"pay.write.self","name":"建自己"},
      {"code":"pay.write.all","name":"建全部"}
    ]}
  ]}
]}`

/* ---------------- 预览 / 写入 ---------------- */

// T009：预览只读且两次结果一致（US1 验收 1）。
func TestPermissionImportPreviewIsReadOnly(t *testing.T) {
	env := newImportEnv(t)
	doc := flatImportDoc(50)

	status, preview := env.validate(t, doc, "")
	if status != http.StatusOK {
		t.Fatalf("预览 status = %d，期望 200", status)
	}
	if !preview.Ok || preview.Added != 50 || preview.Updated != 0 || preview.Unchanged != 0 || preview.Total != 50 {
		t.Fatalf("预览计数不对：%+v", preview)
	}
	if got := len(env.permissionRows(t, env.clientID)); got != 0 {
		t.Fatalf("预览后库里应仍为 0 条，实际 %d", got)
	}

	_, again := env.validate(t, doc, "file")
	if again.Added != preview.Added || again.Total != preview.Total {
		t.Fatalf("同一文档两次预览不一致：%+v vs %+v", preview, again)
	}

	status, _, result := env.confirm(t, doc, "file")
	if status != http.StatusOK {
		t.Fatalf("确认导入 status = %d，期望 200", status)
	}
	if result.Added != 50 || result.Source != service.ImportSourceFile {
		t.Fatalf("导入结果不对：%+v", result)
	}
	rows := env.permissionRows(t, env.clientID)
	if len(rows) != 50 {
		t.Fatalf("库里条数 = %d，期望 50", len(rows))
	}
	if rows[0].Code != "perm.000" || rows[0].Name != "权限000" || rows[0].Sort != 0 {
		t.Fatalf("首条字段与输入不一致：%+v", rows[0])
	}
}

// T011：内容非法时预览 200 + ok=false；确认导入 400 且 data=null，不写审计（FR-005 / FR-029）。
func TestPermissionImportInvalidContent(t *testing.T) {
	env := newImportEnv(t)
	bad := `{"permissions":[{"code":"order read","name":"订单"},{"code":"a","name":"A","id":"x"}]}`

	status, preview := env.validate(t, bad, "")
	if status != http.StatusOK {
		t.Fatalf("内容非法时预览应仍为 200，实际 %d", status)
	}
	if preview.Ok || len(preview.Issues) == 0 {
		t.Fatalf("非法内容应给出 issues，实际 %+v", preview)
	}
	if preview.Added != 0 || preview.Updated != 0 || preview.Unchanged != 0 || preview.Moved != 0 {
		t.Fatalf("ok=false 时计数必须归零，实际 %+v", preview)
	}

	status, resp, _ := env.confirm(t, bad, "")
	if status != http.StatusBadRequest {
		t.Fatalf("非法内容确认导入 status = %d，期望 400", status)
	}
	if resp.Code != "APP_PERMISSION_IMPORT_INVALID" {
		t.Fatalf("code = %q，期望 APP_PERMISSION_IMPORT_INVALID", resp.Code)
	}
	if string(resp.Data) != "null" {
		t.Fatalf("失败响应 data 必须为 null，实际 %s", resp.Data)
	}
	if got := len(env.permissionRows(t, env.clientID)); got != 0 {
		t.Fatalf("非法导入后库内条数 = %d，期望 0", got)
	}
	env.waitAuditCount(t, model.EventAppPermissionsImported, 0)
}

// T010 / T018：三层结构父先子后落库，父子关系与输入一致（US2 验收 1/2）。
func TestPermissionImportWritesTree(t *testing.T) {
	env := newImportEnv(t)
	status, _, result := env.confirm(t, threeLevelDoc, "")
	if status != http.StatusOK || result.Added != 14 {
		t.Fatalf("导入失败：status=%d result=%+v", status, result)
	}

	rows := env.permissionRows(t, env.clientID)
	byCode := map[string]model.ClientPermission{}
	for _, row := range rows {
		byCode[row.Code] = row
	}
	if len(rows) != 14 {
		t.Fatalf("条数 = %d，期望 14", len(rows))
	}
	order := byCode["order"]
	if order.ParentID != nil {
		t.Fatal("根节点 parent 必须为空")
	}
	read := byCode["order.read"]
	if read.ParentID == nil || *read.ParentID != order.ID {
		t.Fatal("order.read 的父级应为 order")
	}
	all := byCode["order.read.all"]
	if all.ParentID == nil || *all.ParentID != read.ID {
		t.Fatal("order.read.all 的父级应为 order.read")
	}
	// 省略 sort 时按同级书写顺序 0..n-1
	if byCode["pay"].Sort != 1 || byCode["order"].Sort != 0 {
		t.Fatalf("同级 sort 未按书写顺序分配：order=%d pay=%d",
			byCode["order"].Sort, byCode["pay"].Sort)
	}
	if byCode["order.read"].Sort != 0 || byCode["order.write"].Sort != 1 {
		t.Fatalf("子级 sort 未按书写顺序分配：read=%d write=%d",
			byCode["order.read"].Sort, byCode["order.write"].Sort)
	}
}

// T017 / T018：上级改写按输入整体重写、不成环（US2 验收 7/8）。
func TestPermissionImportRewritesParentsWithoutCycle(t *testing.T) {
	env := newImportEnv(t)
	first := `{"permissions":[{"code":"A","name":"A","children":[{"code":"B","name":"B"}]}]}`
	if status, _, _ := env.confirm(t, first, ""); status != http.StatusOK {
		t.Fatalf("首次导入失败：%d", status)
	}

	swapped := `{"permissions":[{"code":"B","name":"B","children":[{"code":"A","name":"A"}]}]}`
	status, preview := env.validate(t, swapped, "")
	if status != http.StatusOK || !preview.Ok {
		t.Fatalf("预览失败：%d %+v", status, preview)
	}
	if preview.Moved != 2 || len(preview.MovedItems) != 2 {
		t.Fatalf("moved/movedItems = %d/%d，期望 2/2", preview.Moved, len(preview.MovedItems))
	}
	if preview.MovedItems[0].ToParent != "" && preview.MovedItems[0].Code != "B" {
		t.Fatalf("首条上级变更应为 B → 根级，实际 %+v", preview.MovedItems[0])
	}

	status, _, result := env.confirm(t, swapped, "")
	if status != http.StatusOK || result.Moved != 2 {
		t.Fatalf("确认导入失败：%d %+v", status, result)
	}

	rows := env.permissionRows(t, env.clientID)
	if len(rows) != 2 {
		t.Fatalf("条数 = %d，期望 2（不应产生新节点）", len(rows))
	}
	byCode := map[string]model.ClientPermission{}
	for _, row := range rows {
		byCode[row.Code] = row
	}
	if byCode["B"].ParentID != nil {
		t.Fatal("B 应变为根级")
	}
	if byCode["A"].ParentID == nil || *byCode["A"].ParentID != byCode["B"].ID {
		t.Fatal("A 应挂到 B 之下")
	}
}

// T020 / SC-003：原样重导 → 无变化，且不产生第二条成功事件。
func TestReimportNoopWritesNoAudit(t *testing.T) {
	env := newImportEnv(t)
	doc := flatImportDoc(20)

	if status, _, result := env.confirm(t, doc, "paste"); status != http.StatusOK || result.Added != 20 {
		t.Fatalf("首次导入失败：%d %+v", status, result)
	}
	env.waitAuditCount(t, model.EventAppPermissionsImported, 1)

	status, resp, second := env.confirm(t, doc, "paste")
	if status != http.StatusOK {
		t.Fatalf("二次导入 status = %d", status)
	}
	if resp.Code != "OK" {
		t.Fatalf("二次导入 code = %q", resp.Code)
	}
	if second.Added != 0 || second.Updated != 0 || second.Unchanged != 20 {
		t.Fatalf("二次导入计数 = %+v，期望 0/0/20", second)
	}
	if got := len(env.permissionRows(t, env.clientID)); got != 20 {
		t.Fatalf("条数 = %d，期望 20", got)
	}
	// 给异步审计一点时间，确认没有新增事件
	time.Sleep(100 * time.Millisecond)
	env.waitAuditCount(t, model.EventAppPermissionsImported, 1)
}

// T020：改 3 个名称 → 更新 3、无变化 17，只更新字段不新建。
func TestPermissionImportUpdatesChangedFields(t *testing.T) {
	env := newImportEnv(t)
	doc := flatImportDoc(20)
	if status, _, _ := env.confirm(t, doc, ""); status != http.StatusOK {
		t.Fatalf("首次导入失败：%d", status)
	}

	changed := strings.NewReplacer(
		`"name":"权限001"`, `"name":"改名001"`,
		`"name":"权限002"`, `"name":"改名002"`,
		`"name":"权限003"`, `"name":"改名003"`,
	).Replace(doc)

	_, preview := env.validate(t, changed, "")
	if preview.Updated != 3 || preview.Unchanged != 17 || preview.Added != 0 {
		t.Fatalf("预览计数 = %+v，期望 updated3/unchanged17/added0", preview)
	}

	status, _, result := env.confirm(t, changed, "")
	if status != http.StatusOK || result.Updated != 3 || result.Unchanged != 17 {
		t.Fatalf("导入结果 = %+v (status=%d)", result, status)
	}
	rows := env.permissionRows(t, env.clientID)
	if len(rows) != 20 {
		t.Fatalf("条数 = %d，期望 20", len(rows))
	}
	for _, row := range rows {
		switch row.Code {
		case "perm.001":
			if row.Name != "改名001" {
				t.Fatalf("perm.001 未更新：%q", row.Name)
			}
		case "perm.004":
			if row.Name != "权限004" {
				t.Fatalf("未改动的 perm.004 被覆盖：%q", row.Name)
			}
		}
	}
}

// T021：并发导入同一份文档不产生重复，失败方得到 409 APP_RESOURCE_STALE。
func TestPermissionImportConcurrentNoDuplicates(t *testing.T) {
	env := newImportEnv(t)
	doc := flatImportDoc(20)

	var wg sync.WaitGroup
	statuses := make([]int, 2)
	codes := make([]string, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			status, resp := doJSON(t, env.admin, http.MethodPost, env.adminPath("/import"), doc)
			statuses[idx], codes[idx] = status, resp.Code
		}(i)
	}
	wg.Wait()

	success := 0
	for i, status := range statuses {
		switch status {
		case http.StatusOK:
			success++
		case http.StatusConflict:
			if codes[i] != "APP_RESOURCE_STALE" {
				t.Fatalf("冲突方 code = %q，期望 APP_RESOURCE_STALE", codes[i])
			}
		default:
			t.Fatalf("并发导入出现非预期状态码 %d（code=%q）", status, codes[i])
		}
	}
	if success == 0 {
		t.Fatal("并发导入至少应有一方成功")
	}

	rows := env.permissionRows(t, env.clientID)
	if len(rows) != 20 {
		t.Fatalf("并发后条数 = %d，期望 20", len(rows))
	}
	seen := map[string]bool{}
	for _, row := range rows {
		if seen[row.Code] {
			t.Fatalf("出现重复编码 %q", row.Code)
		}
		seen[row.Code] = true
	}
}

/* ---------------- 审计（FR-023 / FR-025） ---------------- */

// T034 / T035：1000 条只产生 1 条聚合事件，addedIds 截断 20、addedCount 全量、docSha256 64 位。
func TestImportWritesExactlyOneAuditEvent(t *testing.T) {
	env := newImportEnv(t)
	doc := flatImportDoc(1000)

	status, _, result := env.confirm(t, doc, "file")
	if status != http.StatusOK || result.Added != 1000 {
		t.Fatalf("导入失败：%d %+v", status, result)
	}
	env.waitAuditCount(t, model.EventAppPermissionsImported, 1)

	detail := env.latestAuditDetail(t, model.EventAppPermissionsImported)
	if detail["clientId"] != env.clientID {
		t.Fatalf("detail.clientId = %v", detail["clientId"])
	}
	if added, _ := detail["added"].(float64); int(added) != 1000 {
		t.Fatalf("detail.added = %v，期望 1000", detail["added"])
	}
	if count, _ := detail["addedCount"].(float64); int(count) != 1000 {
		t.Fatalf("detail.addedCount = %v，期望 1000", detail["addedCount"])
	}
	ids, _ := detail["addedIds"].([]any)
	if len(ids) != 20 {
		t.Fatalf("detail.addedIds 长度 = %d，期望 20（截断）", len(ids))
	}
	sum, _ := detail["docSha256"].(string)
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(sum) {
		t.Fatalf("detail.docSha256 = %q，期望 64 位十六进制", sum)
	}
	if detail["source"] != service.ImportSourceFile {
		t.Fatalf("detail.source = %v，期望 file", detail["source"])
	}
}

// T035：预览（含非法内容）连续 5 次也不写任何审计事件。
func TestImportPreviewWritesNoAudit(t *testing.T) {
	env := newImportEnv(t)
	bad := `{"permissions":[{"code":"order read","name":"订单"}]}`
	for i := 0; i < 5; i++ {
		if status, _ := env.validate(t, flatImportDoc(3), ""); status != http.StatusOK {
			t.Fatalf("预览 status = %d", status)
		}
		if status, _ := env.validate(t, bad, ""); status != http.StatusOK {
			t.Fatalf("非法预览 status = %d", status)
		}
	}
	time.Sleep(150 * time.Millisecond)
	env.waitAuditCount(t, model.EventAppPermissionsImported, 0)

	// 预览在「应用不存在 / 已停用」时也不写任何审计（FR-002 / FR-025）
	t.Run("应用不存在", func(t *testing.T) {
		missing := newImportEnv(t)
		missing.clientID = "01J9MISSING0000000000000"
		if status, _ := missing.validate(t, flatImportDoc(2), ""); status != http.StatusNotFound {
			t.Fatalf("预览 status = %d，期望 404", status)
		}
		time.Sleep(100 * time.Millisecond)
		missing.waitAuditCount(t, model.EventAppPermissionsImported, 0)
	})

	t.Run("应用停用", func(t *testing.T) {
		disabled := newImportEnv(t)
		if err := disabled.db.Exec("UPDATE clients SET status = 0 WHERE client_id = ?", disabled.clientID).Error; err != nil {
			t.Fatalf("停用应用失败：%v", err)
		}
		if status, _ := disabled.validate(t, flatImportDoc(2), ""); status != http.StatusForbidden {
			t.Fatalf("预览 status = %d，期望 403", status)
		}
		time.Sleep(100 * time.Millisecond)
		disabled.waitAuditCount(t, model.EventAppPermissionsImported, 0)
	})
}

// T035：业务拒绝写 1 条 failure（含 reason），内容非法不写。
func TestImportFailureReason(t *testing.T) {
	cases := []struct {
		name   string
		setup  func(t *testing.T, env *importEnv)
		doc    string
		status int
		code   string
		reason string
	}{
		{
			name:   "条数超限",
			doc:    flatImportDoc(1001),
			status: http.StatusRequestEntityTooLarge,
			code:   "APP_PERMISSION_IMPORT_TOO_LARGE",
			reason: "too_large",
		},
		{
			name:   "应用不存在",
			setup:  func(t *testing.T, env *importEnv) { env.clientID = "01J9NOTEXIST000000000000" },
			doc:    flatImportDoc(1),
			status: http.StatusNotFound,
			code:   "NOT_FOUND",
			reason: "client_not_found",
		},
		{
			name: "应用停用",
			setup: func(t *testing.T, env *importEnv) {
				if err := env.db.Exec("UPDATE clients SET status = 0 WHERE client_id = ?", env.clientID).Error; err != nil {
					t.Fatalf("停用应用失败：%v", err)
				}
			},
			doc:    flatImportDoc(1),
			status: http.StatusForbidden,
			code:   "FORBIDDEN",
			reason: "client_disabled",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := newImportEnv(t)
			if c.setup != nil {
				c.setup(t, env)
			}
			status, resp, _ := env.confirm(t, c.doc, "paste")
			if status != c.status || resp.Code != c.code {
				t.Fatalf("status/code = %d/%q，期望 %d/%q（body=%+v）", status, resp.Code, c.status, c.code, resp)
			}
			// 条数超限（413）必须告诉管理员实际条数，便于裁剪（FR-013）
			if c.reason == "too_large" && !strings.Contains(resp.Message, "本次共 1001 条") {
				t.Fatalf("条数超限文案应含实际条数：%q", resp.Message)
			}
			env.waitAuditCount(t, model.EventAppPermissionsImported, 1)
			detail := env.latestAuditDetail(t, model.EventAppPermissionsImported)
			if detail["reason"] != c.reason {
				t.Fatalf("detail.reason = %v，期望 %s", detail["reason"], c.reason)
			}
		})
	}

	t.Run("内容非法不写", func(t *testing.T) {
		env := newImportEnv(t)
		status, _, _ := env.confirm(t, `{"permissions":[{"code":"order read","name":"订单"}]}`, "paste")
		if status != http.StatusBadRequest {
			t.Fatalf("status = %d，期望 400", status)
		}
		time.Sleep(150 * time.Millisecond)
		env.waitAuditCount(t, model.EventAppPermissionsImported, 0)
	})
}

/* ---------------- 导出与回导（FR-019） ---------------- */

// T028：A 导出 → B 导入，编码集合与父子结构完全一致；原样回导 A 为无变化。
func TestExportImportRoundTrip(t *testing.T) {
	env := newImportEnv(t)
	source := `{"version":1,"mode":"merge","permissions":[
	  {"code":"order","name":"订单","children":[
	    {"code":"order.item:read","name":"读商品","children":[
	      {"code":"order.item:read.all","name":"读全部"}
	    ]},
	    {"code":"order.write","name":"写订单"}
	  ]},
	  {"code":"pay","name":"支付","children":[
	    {"code":"pay.refund","name":"退款"}
	  ]}
	]}`
	if status, _, _ := env.confirm(t, source, ""); status != http.StatusOK {
		t.Fatalf("源应用导入失败：%d", status)
	}

	status, resp := doJSON(t, env.admin, http.MethodGet, env.adminPath("/export"), "")
	if status != http.StatusOK {
		t.Fatalf("导出 status = %d（body=%s）", status, resp.Message)
	}
	var exported dto.PermissionExportDocument
	if err := json.Unmarshal(resp.Data, &exported); err != nil {
		t.Fatalf("导出响应解析失败：%v", err)
	}
	if !exported.Importable.Ok || len(exported.Importable.Reasons) != 0 {
		t.Fatalf("导出应可回导，实际 %+v", exported.Importable)
	}
	if len(exported.Permissions) != 2 {
		t.Fatalf("根节点数 = %d，期望 2", len(exported.Permissions))
	}
	raw, _ := json.Marshal(exported.Permissions)
	for _, banned := range []string{`"id"`, `"parentId"`, `"sort"`} {
		if strings.Contains(string(raw), banned) {
			t.Fatalf("导出内容不得含内部标识 %s：%s", banned, raw)
		}
	}
	if !strings.Contains(string(raw), "order.item:read") {
		t.Fatalf("特殊字符编码被改写：%s", raw)
	}

	// 下载文件 = 剔除 importable 后的 2 空格缩进序列化
	file := map[string]any{
		"version":     exported.Version,
		"mode":        exported.Mode,
		"permissions": exported.Permissions,
	}
	payload, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		t.Fatalf("序列化失败：%v", err)
	}
	if len(payload) > service.MaxImportBodyBytes {
		t.Fatalf("样例文件不应超过 512KB")
	}

	// 空应用 B
	cl, _, err := env.clients.Create(context.Background(), service.CreateClientInput{
		ClientName:   "搬运目标",
		HomeURL:      "https://b.example.com",
		RedirectURIs: []string{"https://b.example.com/cb"},
	})
	if err != nil {
		t.Fatalf("创建应用 B 失败：%v", err)
	}

	// 空应用导出：permissions=[] 且 importable.ok=false/reasons=["empty"]（不能声称可回导）
	status, resp = doJSON(t, env.admin, http.MethodGet,
		"/api/v1/admin/clients/"+cl.ClientID+"/permissions/export", "")
	if status != http.StatusOK {
		t.Fatalf("B 导出失败：%d", status)
	}
	var emptyExport dto.PermissionExportDocument
	_ = json.Unmarshal(resp.Data, &emptyExport)
	if len(emptyExport.Permissions) != 0 || emptyExport.Importable.Ok {
		t.Fatalf("空应用导出应为空且不可回导：%+v", emptyExport)
	}
	if len(emptyExport.Importable.Reasons) != 1 || emptyExport.Importable.Reasons[0] != "empty" {
		t.Fatalf("空应用 reasons = %v，期望 [empty]", emptyExport.Importable.Reasons)
	}

	status, resp = doJSON(t, env.admin, http.MethodPost,
		"/api/v1/admin/clients/"+cl.ClientID+"/permissions/import", string(payload))
	if status != http.StatusOK {
		t.Fatalf("B 导入失败：%d %s", status, resp.Message)
	}
	var bResult dto.PermissionImportResult
	_ = json.Unmarshal(resp.Data, &bResult)
	if bResult.Added != 6 {
		t.Fatalf("B 新增 = %d，期望 6", bResult.Added)
	}

	aRows := env.permissionRows(t, env.clientID)
	bRows := env.permissionRows(t, cl.ClientID)
	if len(aRows) != len(bRows) {
		t.Fatalf("条数不一致：A=%d B=%d", len(aRows), len(bRows))
	}
	structure := func(rows []model.ClientPermission) map[string]string {
		codeByID := map[string]string{}
		for _, row := range rows {
			codeByID[row.ID] = row.Code
		}
		out := map[string]string{}
		for _, row := range rows {
			parent := ""
			if row.ParentID != nil {
				parent = codeByID[*row.ParentID]
			}
			out[row.Code] = parent
		}
		return out
	}
	aStruct, bStruct := structure(aRows), structure(bRows)
	for code, parent := range aStruct {
		if bStruct[code] != parent {
			t.Fatalf("%s 的父级不一致：A=%q B=%q", code, parent, bStruct[code])
		}
	}

	// 原样回导 A → 无变化
	status, _, back := env.confirm(t, string(payload), "")
	if status != http.StatusOK {
		t.Fatalf("回导 A 失败：%d", status)
	}
	if back.Added != 0 || back.Updated != 0 || back.Unchanged != 6 {
		t.Fatalf("回导计数 = %+v，期望 0/0/6", back)
	}
}

// T025：条数超过列表页上限时导出不截断（证明未复用 LIMIT 1000），并给出 too_many。
func TestExportDoesNotTruncate(t *testing.T) {
	env := newImportEnv(t)
	// 直接落库绕过"单次导入 ≤1000 条"的上限，模拟历史大应用。
	now := time.Now().UnixMilli()
	rows := make([]*model.ClientPermission, 1001)
	for i := range rows {
		rows[i] = &model.ClientPermission{
			ID: id.New(), ClientID: env.clientID,
			Code: fmt.Sprintf("bulk.%04d", i), Name: fmt.Sprintf("批量%04d", i),
			Sort: i, CreatedAt: now, UpdatedAt: now,
		}
	}
	if err := env.st.CreateClientPermissions(context.Background(), rows); err != nil {
		t.Fatalf("预置 1001 条失败：%v", err)
	}

	status, resp := doJSON(t, env.admin, http.MethodGet, env.adminPath("/export"), "")
	if status != http.StatusOK {
		t.Fatalf("导出 status = %d", status)
	}
	var exported dto.PermissionExportDocument
	_ = json.Unmarshal(resp.Data, &exported)
	if len(exported.Permissions) != 1001 {
		t.Fatalf("导出条数 = %d，期望 1001（不得静默截断）", len(exported.Permissions))
	}
	if exported.Importable.Ok {
		t.Fatal("1001 条应判为不可回导")
	}
	if len(exported.Importable.Reasons) != 1 || exported.Importable.Reasons[0] != "too_many" {
		t.Fatalf("reasons = %v，期望 [too_many]", exported.Importable.Reasons)
	}
}

// T033：同一份输入在 admin 与门户两端结论逐字一致；应用不存在时响应体相同（不泄露存在性）。
func TestPermissionImportPreviewParity(t *testing.T) {
	env := newImportEnv(t)
	bad := `{"permissions":[
	  {"code":"order read","name":"订单"},
	  {"code":"pay","name":"支付","children":[{"code":"order.read","name":"查询"}]},
	  {"code":"pay","name":"支付"},
	  {"code":"x","name":"X","id":"bad"}
	]}`

	adminStatus, adminResp := doJSON(t, env.admin, http.MethodPost, env.adminPath("/import/validate"), bad)
	portalStatus, portalResp := doJSON(t, env.portal, http.MethodPost, env.portalPath("/import/validate"), bad)
	if adminStatus != http.StatusOK || portalStatus != http.StatusOK {
		t.Fatalf("两端预览状态 = %d/%d，期望 200/200", adminStatus, portalStatus)
	}

	var a, p dto.PermissionImportPreview
	_ = json.Unmarshal(adminResp.Data, &a)
	_ = json.Unmarshal(portalResp.Data, &p)
	if a.Ok != p.Ok || a.IssueTotal != p.IssueTotal || len(a.Issues) != len(p.Issues) {
		t.Fatalf("两端结论不一致：admin=%+v portal=%+v", a, p)
	}
	for i := range a.Issues {
		if a.Issues[i].Path != p.Issues[i].Path ||
			a.Issues[i].Code != p.Issues[i].Code ||
			a.Issues[i].Message != p.Issues[i].Message {
			t.Fatalf("第 %d 条问题不一致：%+v vs %+v", i, a.Issues[i], p.Issues[i])
		}
	}

	// 应用不存在：admin 端 404（平台管理员本就知道应用清单），门户端经 AppAdmin 中间件统一 403。
	// 两端都不得暴露更多信息（FR-021）；这里按生产路径（带中间件）断言，而不是直接调门户 handler。
	missingID := "01J9MISSING0000000000000"
	adminStatus, adminResp = doJSON(t, env.admin, http.MethodPost,
		"/api/v1/admin/clients/"+missingID+"/permissions/import", flatImportDoc(1))
	if adminStatus != http.StatusNotFound || adminResp.Code != "NOT_FOUND" {
		t.Fatalf("admin 端不存在应用应 404 NOT_FOUND，实际 %d %+v", adminStatus, adminResp)
	}
	if string(adminResp.Data) != "null" {
		t.Fatal("失败响应 data 必须为 null")
	}

	portalR := gin.New()
	portalR.POST("/api/v1/me/apps/:id/permissions/import",
		func(c *gin.Context) { c.Set(middleware.CtxUser, &model.User{ID: id.New()}); c.Next() },
		middleware.AppAdmin(env.appAuth, env.log), env.meH.AppConfirmPermissionImport)
	portalStatus, portalResp = doJSON(t, portalR, http.MethodPost,
		"/api/v1/me/apps/"+missingID+"/permissions/import", flatImportDoc(1))
	if portalStatus != http.StatusForbidden || portalResp.Code != "FORBIDDEN" {
		t.Fatalf("门户不存在应用应 403 FORBIDDEN（不泄露存在性），实际 %d %+v", portalStatus, portalResp)
	}
	if string(portalResp.Data) != "null" || strings.Contains(portalResp.Message, "不存在") {
		t.Fatalf("门户 403 不得泄露存在性：%+v", portalResp)
	}
}

// T041 / FR-025：门户「确认导入」的失败审计——停用与不存在都要写，非管理员与未登录不写。
func TestPortalConfirmImportWritesFailureAudit(t *testing.T) {
	t.Run("应用管理员正常导入", func(t *testing.T) {
		env := newImportEnv(t)
		dept := env.createOrg(t, nil, "技术中心", nil)
		uid := env.insertUser(t, dept.ID, "portaladmin1")
		env.addAppAdmin(t, uid, true)

		status, resp := env.confirmAsPortal(t, &model.User{ID: uid}, flatImportDoc(2))
		if status != http.StatusOK || resp.Code != "OK" {
			t.Fatalf("门户导入失败：%d %+v", status, resp)
		}
		env.waitAuditCount(t, model.EventAppPermissionsImported, 1)
		if got := len(env.permissionRows(t, env.clientID)); got != 2 {
			t.Fatalf("条数 = %d，期望 2", got)
		}
	})

	t.Run("应用停用", func(t *testing.T) {
		env := newImportEnv(t)
		dept := env.createOrg(t, nil, "技术中心", nil)
		uid := env.insertUser(t, dept.ID, "portaladmin2")
		env.addAppAdmin(t, uid, true)
		if err := env.db.Exec("UPDATE clients SET status = 0 WHERE client_id = ?", env.clientID).Error; err != nil {
			t.Fatalf("停用应用失败：%v", err)
		}

		status, resp := env.confirmAsPortal(t, &model.User{ID: uid}, flatImportDoc(1))
		if status != http.StatusForbidden || resp.Code != "FORBIDDEN" {
			t.Fatalf("停用应用应 403 FORBIDDEN，实际 %d %+v", status, resp)
		}
		// 门户文案必须通用：不出现"停用"这类只有知道应用状态才说得出的措辞（FR-021）
		if strings.Contains(resp.Message, "停用") {
			t.Fatalf("门户 403 文案泄露了应用状态：%q", resp.Message)
		}
		env.waitAuditCount(t, model.EventAppPermissionsImported, 1)
		detail := env.latestAuditDetail(t, model.EventAppPermissionsImported)
		if detail["reason"] != "client_disabled" || detail["clientId"] != env.clientID {
			t.Fatalf("失败审计内容不对：%+v", detail)
		}
	})

	t.Run("应用不存在", func(t *testing.T) {
		env := newImportEnv(t)
		env.clientID = "01J9MISSING0000000000000"

		status, resp := env.confirmAsPortal(t, &model.User{ID: id.New()}, flatImportDoc(1))
		if status != http.StatusForbidden || resp.Code != "FORBIDDEN" {
			t.Fatalf("不存在的应用应 403 FORBIDDEN，实际 %d %+v", status, resp)
		}
		env.waitAuditCount(t, model.EventAppPermissionsImported, 1)
		detail := env.latestAuditDetail(t, model.EventAppPermissionsImported)
		if detail["reason"] != "client_not_found" {
			t.Fatalf("失败审计 reason = %v，期望 client_not_found", detail["reason"])
		}
	})

	t.Run("非应用管理员不写审计", func(t *testing.T) {
		env := newImportEnv(t)
		dept := env.createOrg(t, nil, "技术中心", nil)
		uid := env.insertUser(t, dept.ID, "plainmember")
		env.addAppAdmin(t, uid, false)

		status, resp := env.confirmAsPortal(t, &model.User{ID: uid}, flatImportDoc(1))
		if status != http.StatusForbidden || resp.Code != "FORBIDDEN" {
			t.Fatalf("非应用管理员应 403，实际 %d %+v", status, resp)
		}
		time.Sleep(150 * time.Millisecond)
		env.waitAuditCount(t, model.EventAppPermissionsImported, 0)
	})

	t.Run("未登录返回 401", func(t *testing.T) {
		env := newImportEnv(t)
		status, resp := env.confirmAsPortal(t, nil, flatImportDoc(1))
		if status != http.StatusUnauthorized || resp.Code != "UNAUTHENTICATED" {
			t.Fatalf("未登录应 401，实际 %d %+v", status, resp)
		}
	})
}

// T042 / FR-017：导入（含上级改写）不得动角色-权限绑定。
func TestImportKeepsRoleBindings(t *testing.T) {
	env := newImportEnv(t)
	first := `{"permissions":[
	  {"code":"order","name":"订单","children":[{"code":"order.read","name":"查询"}]},
	  {"code":"pay","name":"支付"}
	]}`
	if status, _, _ := env.confirm(t, first, ""); status != http.StatusOK {
		t.Fatalf("首次导入失败：%d", status)
	}

	ctx := context.Background()
	role, err := env.appAuth.CreateRole(ctx, env.clientID, "ops", nil)
	if err != nil {
		t.Fatalf("创建角色失败：%v", err)
	}
	var readID string
	for _, row := range env.permissionRows(t, env.clientID) {
		if row.Code == "order.read" {
			readID = row.ID
		}
	}
	if readID == "" {
		t.Fatal("未找到 order.read")
	}
	if _, _, _, err := env.appAuth.PutRolePermissions(ctx, env.clientID, role.ID, []string{readID}, nil); err != nil {
		t.Fatalf("授予角色权限失败：%v", err)
	}

	moved := `{"permissions":[
	  {"code":"order","name":"订单"},
	  {"code":"pay","name":"支付","children":[{"code":"order.read","name":"查询"}]}
	]}`
	status, _, result := env.confirm(t, moved, "")
	if status != http.StatusOK || result.Moved != 1 {
		t.Fatalf("换爹导入失败：%d %+v", status, result)
	}

	bindings := []model.ClientRolePermission{}
	if err := env.db.Where("client_id = ? AND role_id = ?", env.clientID, role.ID).Find(&bindings).Error; err != nil {
		t.Fatalf("读取角色权限绑定失败：%v", err)
	}
	if len(bindings) != 1 || bindings[0].PermissionID != readID {
		t.Fatalf("角色绑定被改了：%+v", bindings)
	}
}

// T043 / SC-005：预览的数据库往返次数与条数无关（一次应用校验 + 一次全量读）。
func TestImportPreviewQueryCountIsConstant(t *testing.T) {
	env := newImportEnv(t)
	var queries int64
	if err := env.db.Callback().Query().Before("gorm:query").
		Register("test:count_import_preview", func(*gorm.DB) { atomic.AddInt64(&queries, 1) }); err != nil {
		t.Fatalf("注册查询计数回调失败：%v", err)
	}
	t.Cleanup(func() { _ = env.db.Callback().Query().Remove("test:count_import_preview") })

	count := func(doc string) int64 {
		atomic.StoreInt64(&queries, 0)
		status, preview := env.validate(t, doc, "")
		if status != http.StatusOK || !preview.Ok {
			t.Fatalf("预览失败：%d %+v", status, preview)
		}
		return atomic.LoadInt64(&queries)
	}

	small := count(flatImportDoc(10))
	large := count(flatImportDoc(1000))
	if small != large {
		t.Fatalf("预览查询次数随条数增长：10 条 %d 次，1000 条 %d 次", small, large)
	}
	if large > 3 {
		t.Fatalf("1000 条预览产生 %d 次查询，期望常量级（≤3）", large)
	}
}

// T044 / FR-004、SC-006：写入中途失败必须整批回滚，不留部分写入。
func TestImportRollsBackOnMidWriteFailure(t *testing.T) {
	env := newImportEnv(t)
	// 用触发器让 code=boom 的插入失败：前两条已插入，验证事务回滚把它们一并撤销。
	err := env.db.Exec(`CREATE TRIGGER test_perm_boom BEFORE INSERT ON client_permissions
	  FOR EACH ROW BEGIN
	    IF NEW.code = 'boom' THEN
	      SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'boom';
	    END IF;
	  END`).Error
	if err != nil {
		t.Fatalf("创建触发器失败：%v", err)
	}
	t.Cleanup(func() { _ = env.db.Exec("DROP TRIGGER IF EXISTS test_perm_boom").Error })

	doc := `{"permissions":[
	  {"code":"aaa","name":"A"},
	  {"code":"mmm","name":"M"},
	  {"code":"boom","name":"B"}
	]}`
	status, resp, _ := env.confirm(t, doc, "")
	if status != http.StatusInternalServerError || resp.Code != "INTERNAL_ERROR" {
		t.Fatalf("中途失败应 500 INTERNAL_ERROR，实际 %d %+v", status, resp)
	}
	if rows := env.permissionRows(t, env.clientID); len(rows) != 0 {
		t.Fatalf("回滚失败：残留 %d 条（%v）", len(rows), rows)
	}
	time.Sleep(150 * time.Millisecond)
	env.waitAuditCount(t, model.EventAppPermissionsImported, 0)
}

// T045 / US2 边界：被移动节点的既有子节点（未出现在本次输入中）不随之移动。
func TestImportKeepsChildrenOfMovedNode(t *testing.T) {
	env := newImportEnv(t)
	first := `{"permissions":[
	  {"code":"order","name":"订单","children":[
	    {"code":"order.read","name":"查询","children":[{"code":"order.read.self","name":"查自己"}]}]},
	  {"code":"pay","name":"支付"}
	]}`
	if status, _, _ := env.confirm(t, first, ""); status != http.StatusOK {
		t.Fatalf("首次导入失败：%d", status)
	}

	moved := `{"permissions":[
	  {"code":"order","name":"订单"},
	  {"code":"pay","name":"支付","children":[{"code":"order.read","name":"查询"}]}
	]}`
	status, _, result := env.confirm(t, moved, "")
	if status != http.StatusOK || result.Moved != 1 {
		t.Fatalf("换爹导入失败：%d %+v", status, result)
	}

	byCode := map[string]model.ClientPermission{}
	for _, row := range env.permissionRows(t, env.clientID) {
		byCode[row.Code] = row
	}
	if byCode["order.read.self"].ParentID == nil ||
		*byCode["order.read.self"].ParentID != byCode["order.read"].ID {
		t.Fatal("未出现在输入中的子节点被顺带移动了")
	}
	if byCode["order.read"].ParentID == nil || *byCode["order.read"].ParentID != byCode["pay"].ID {
		t.Fatal("order.read 未按输入挂到 pay 下")
	}
}

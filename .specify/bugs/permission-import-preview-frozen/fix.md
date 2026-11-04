# Bug Fix: 权限导入「校验预览」成功后弹窗卡死（movedItems 为 null 触发渲染异常）

- **Slug**: permission-import-preview-frozen
- **Fixed**: 2026-11-04
- **Assessment**: ./assessment.md
- **Status**: applied

## Summary

后端在「预览成功且无上级变更」时把 `movedItems` 序列化成 `null`（nil 切片），而两端前端模板都对它直接取 `.length`，渲染期抛 `TypeError` 导致整块 DOM 不再更新（loading 不复位、确认按钮一直 disabled，接口却已 200）。修复分三层：让 `importPlan` 从源头就持有非 nil 切片、在 DTO 边界再归一一次、前端改用空值安全的 computed 兜底，并各补一条回归用例。

## Changes

| File | Change | Notes |
|------|--------|-------|
| `internal/service/app_permission_import.go` | modified | `buildImportPlan` 初始化 `movedItems: []dto.PermissionImportMoved{}`（第 368 行）；新增 `normalizeMovedItems`（第 628 行）并在 `ValidateImport` 的 DTO 边界使用（第 622 行） |
| `internal/service/app_permission_import_test.go` | added test | `TestMovedItemsSerializesAsEmptyArray`（第 315 行）：断言计划产出与 nil 输入都序列化为 `[]` |
| `web/admin/src/views/client/PermissionImportDialog.vue` | modified | 新增 `const movedItems = computed(() => preview.value?.movedItems ?? [])`（第 52 行）；模板中 4 处 `preview.movedItems` 改用它 |
| `web/portal/src/views/app-admin/PermissionImportDialog.vue` | modified | 同上（computed 在第 61 行） |
| `web/admin/src/views/client/PermissionImportDialog.spec.ts` | added test | 第 232 行：`movedItems: null` 时仍完整渲染、可确认、loading 复位；并新增 `previewButton()` 辅助函数 |
| `web/portal/src/views/app-admin/PermissionImportDialog.spec.ts` | added test | 第 157 行：与 admin 同构的用例 |

## Diff Highlights

后端——源头非 nil：

```go
func buildImportPlan(doc *importDoc, existing []model.ClientPermission) *importPlan {
	plan := &importPlan{movedItems: []dto.PermissionImportMoved{}}
```

DTO 边界归一（与 `truncateIssues` 对 `issues` / `warnings` 的口径一致）：

```go
	preview.Moved = plan.moved
	preview.MovedItems = normalizeMovedItems(plan.movedItems)
```

前端——渲染侧兜底（两端各一份）：

```ts
const movedItems = computed(() => preview.value?.movedItems ?? [])
```

```html
<div v-if="movedItems.length > 0" class="moved-block">
  <el-table :data="movedItems" size="small" border>
```

## Tests Added or Updated

- `internal/service/app_permission_import_test.go::TestMovedItemsSerializesAsEmptyArray` — 钉死「无上级变更」时 `plan.movedItems` 非 nil，且 `normalizeMovedItems` 对计划产出与 nil 输入都序列化为 `[]`（把 null 挡在 wire format 之外）。
- `web/admin/src/views/client/PermissionImportDialog.spec.ts::movedItems 为 null 时仍完整渲染且可确认` — 服务端仍返回 `null` 时，弹窗不得冻结：计数正常渲染、确认按钮可用、"校验预览"按钮无 `is-loading`。
- `web/portal/src/views/app-admin/PermissionImportDialog.spec.ts::movedItems 为 null 时仍完整渲染且可确认` — 门户侧同构用例。

## Local Verification

- Commands run: `go build ./...` → 通过
- Commands run: `go test ./internal/service/ ./internal/api/... -count=1` → 全部 ok（service / api / admin / auth / docs / dto / me / middleware）
- Commands run: `cd web/admin && npx vitest run` → Test Files 8 passed，Tests 38 passed
- Commands run: `cd web/portal && npx vitest run` → Test Files 9 passed，Tests 64 passed
- Commands run: `cd web/admin && npm run lint`、`cd web/portal && npm run lint` → 均无输出（干净）
- Commands run: `gofmt -d internal/service/app_permission_import_test.go` → 无差异（改动区域 gofmt 干净；`app_permission_import.go` 的既有格式漂移未动，避免无关 diff）
- Manual checks: 反证（临时 `git stash` 掉两处 `.vue` 修复后重跑新用例）——admin 与 portal 均复现 `TypeError: Cannot read properties of null (reading 'length')`，各 1 条用例失败；恢复修复后全绿。这独立印证了 assessment 的根因判断（渲染期异常 → patch 中止 → 按钮状态冻结）。

## Deviations from Assessment

- 前端实现形态：assessment 写的是在模板里用 `preview.movedItems?.length` / `?? []` 就地兜底；实际改为一个 `movedItems` computed 集中兜底，模板 4 处引用同一来源。行为等价，避免同一条表达式在四处重复写 `?? []`，也更难被后人改回去。
- 集成用例（`TEST_MYSQL_DSN` 门控的「导入到空应用后断言 `preview.MovedItems != nil` 且响应含 `"movedItems":[]`」）**未添加**：本机无 MySQL，无法运行验证，故改用不触库的纯函数单测覆盖同一保证（计划层非 nil + 序列化为 `[]`）。已列入 Follow-ups。

## Follow-ups

- 补一条 DSN 门控的集成用例：导入到空应用（`moved=0`）后断言 `preview.MovedItems != nil`，并断言原始响应 JSON 含 `"movedItems":[]`，覆盖 HTTP 层的真实序列化。
- 为 `dto.PermissionImportPreview` 的所有数组字段（`issues` / `warnings` / `movedItems`）加一条统一的"非 nil"守护或表驱动测试，防止后续新增数组字段重犯同类问题。
- `internal/api/docs/openapi.json` 无需重新生成：该字段本就声明为 `type: array`，本次是让实现回到文档契约。
- 建议在 `AGENTS.md` 的 API 约定处补一句：Go nil 切片一律在 DTO 边界归一为 `[]`，前端渲染数组字段仍须空值安全（渲染期异常会冻结整个组件，代价远大于一个 `?.`）。

# Bug Assessment: 权限导入「校验预览」成功后按钮卡死（movedItems 为 null 触发渲染异常）

- **Slug**: permission-import-preview-frozen
- **Created**: 2026-11-04
- **Source**: pasted text（用户在 `/speckit.bug.assess` 输入的自由描述，无 URL，未触发 URL Trust Policy）
- **Verdict**: valid
- **Severity**: high

## Report (verbatim or summarized)

> portal和admin端导入应用权限，点击"校验预览"按钮，按钮loading不消失，确认按钮无法点击，实际校验接口早就响应了。admin端

## Symptom

在 admin（及门户「我的应用」）的权限点批量导入弹窗里点「校验预览」，服务端已返回 200 且携带预览结果，但界面停在请求进行中的状态：「校验预览」按钮的 loading 一直转，「确认导入」始终不可点，弹窗内其余信息也不刷新。预期是预览结果渲染出来、确认按钮按 `preview.ok` 启用。

关键触发条件：**校验通过（`ok=true`）且没有任何权限点需要改父级（`moved=0`）**——这是最常见的路径（例如往空应用导入一份全新清单）。校验不通过时不会出现该现象。

## Reproduction

1. 以平台管理员登录 admin，进入任一应用详情 → 权限页，点「批量导入」。
2. 在「粘贴 JSON」中填入一份合法的全新清单（与库中既有编码无重叠、不改变任何既有权限点的上级），例如：
   ```json
   {"version":1,"mode":"merge","permissions":[{"code":"order","name":"订单"},{"code":"pay","name":"支付"}]}
   ```
3. 点「校验预览」→ 浏览器网络面板显示 `POST /api/v1/admin/clients/{id}/permissions/import/validate` **200 OK**，响应体 `data.ok=true`、`data.moved=0`、**`data.movedItems=null`**。
4. 观察界面：按钮 loading 不复位、确认按钮保持 `disabled`；浏览器控制台出现 `TypeError: Cannot read properties of null (reading 'length')`（指向模板 `preview.movedItems.length`）。
5. 反证：改用一份会让某既有权限点改父级的文档（`moved>0`，响应里 `movedItems` 是非空数组），同一操作正常渲染——说明缺陷与「无上级变更」强相关。

门户端（`/me/apps/{id}/...`）步骤相同，表现一致。

## Suspected Code Paths

- `internal/service/app_permission_import.go:591-596` — `ValidateImport` 构造 `PermissionImportPreview` 时把 `MovedItems` 初始化为 `[]dto.PermissionImportMoved{}`。
- `internal/service/app_permission_import.go:616` — 随后 `preview.MovedItems = plan.movedItems` **覆盖**了上面的空切片；而 `plan.movedItems` 在无上级变更时是 nil。
- `internal/service/app_permission_import.go:363-364, 418-422` — `buildImportPlan` 里 `plan := &importPlan{}`，`movedItems` 只在 `len(plan.movedItems) < maxImportMovedItems` 时 `append`，从未初始化；同文件 `truncateIssues`（第 621-629 行）对 `issues` / `warnings` 做了 nil→`[]` 归一，唯独 `movedItems` 漏了这一步。
- `internal/api/dto/dto.go:539` — `MovedItems []PermissionImportMoved \`json:"movedItems"\`` 无 `omitempty`，nil 切片被 `encoding/json` 序列化为 **`null`**（不是 `[]`）。
- `internal/api/docs/openapi.json`（`dto.PermissionImportPreview.movedItems`）— 生成物声明为 `{"type":"array"}` 且非 nullable，即**实现与自家文档契约不符**。
- `web/admin/src/views/client/PermissionImportDialog.vue:325, 327, 344-345` — 模板 `v-if="preview.movedItems.length > 0"`、`:data="preview.movedItems"`、`preview.movedItems.length`，对 `null` 取 `.length` 直接抛 `TypeError`。
- `web/portal/src/views/app-admin/PermissionImportDialog.vue:327, 329, 346-347` — 同一模板的拷贝，同样未做空值兜底。
- `internal/service/app_permission_import_integration_test.go:386-390` — 现有集成用例只覆盖 `moved == 2` 的路径（此时 `movedItems` 非 nil），没有覆盖 `moved == 0`；`web/*/src/views/*/PermissionImportDialog.spec.ts` 的 mock 一律提供 `movedItems: []`，因此**测试全绿但线上必现**。

## Root Cause Hypothesis

**置信度：high。**

后端在「预览成功且无上级变更」时返回 `"movedItems": null`（nil 切片），前端两端模板都用 `preview.movedItems.length` 取值，渲染期抛 `TypeError`。Vue 在开发态会把渲染函数异常向上抛出并**中止本次 patch**：`preview` / `previewing` 的响应式状态其实已经更新，但 DOM 没有被更新，于是表现为「loading 不复位 + 确认按钮仍是旧的 disabled 值」，而请求本身早就 200 —— 与报告的三个现象逐条吻合。生产构建下异常被吞掉、该组件子树被替换为注释节点，表现为弹窗内容整体消失，同一根因的另一种表象。

为什么只有「预览成功且 `moved=0`」中招：`ok=false` 分支在第 600-605 行提前 return，返回的是初始化好的 `MovedItems: []`；只有走到第 616 行才会被 nil 覆盖。而 `moved=0` 恰恰是最常见的导入场景。

## Proposed Remediation

**Preferred（后端归一 + 前端兜底，两处都做）：**

1. 后端在 DTO 边界归一，作为权威修复：在 `ValidateImport` 里把 `plan.movedItems` 归一为 `[]dto.PermissionImportMoved{}`（例如 `preview.MovedItems = append([]dto.PermissionImportMoved{}, plan.movedItems...)`，或先判 nil 再赋值），并在 `buildImportPlan` 初始化 `movedItems: []dto.PermissionImportMoved{}`，形成"双保险"。这样 `movedItems` 与 `issues` / `warnings` 口径一致，响应体与 OpenAPI 的 `type: array` 对齐。
2. 前端（admin + portal 各一处）把渲染改成空值安全的：`v-if="preview.movedItems?.length"`、`:data="preview.movedItems ?? []"`，并在同文件注释里写明"防御后端数组字段为 null，避免单字段异常冻结整个弹窗"。理由：渲染期异常会冻结**整个**组件（dev）或清空子树（prod），一个 `?.` 的代价远小于"整块功能不可用"。

**Alternatives**：
- 只改前端兜底：见效快，但后端仍对外承诺 `null`，与 `openapi.json` 及其他潜在消费者（脚本、接入方）的契约不符，不推荐作为唯一修复。
- 给 `dto` 字段加 `omitempty`：**错误方向**——字段会消失，前端拿到 `undefined`，`.length` 照样抛错，且会让 OpenAPI 契约更难对齐。

**Files likely to change**：
- `internal/service/app_permission_import.go`
- `web/admin/src/views/client/PermissionImportDialog.vue`
- `web/portal/src/views/app-admin/PermissionImportDialog.vue`
- `internal/service/app_permission_import_test.go` 或 `internal/service/app_permission_import_integration_test.go`
- `web/admin/src/views/client/PermissionImportDialog.spec.ts`
- `web/portal/src/views/app-admin/PermissionImportDialog.spec.ts`

**Tests to add or update**：
- Go（不触库）：断言「无上级变更」时 `buildImportPlan` 产出的 `movedItems` 非 nil（若在 plan 层修）；或对预览响应体做 `json.Marshal` 断言，确保 `movedItems` 序列化为 `[]` 而不是 `null`，且 `issues` / `warnings` / `movedItems` 三个数组均非 nil。
- Go 集成（`TEST_MYSQL_DSN` 门控）：导入到**空应用**（`moved=0`）后断言 `preview.MovedItems != nil`，并断言原始响应 JSON 中含 `"movedItems":[]`。
- 前端（两套）：在现有 spec 里补一条 `movedItems: null`（`as unknown as ...`）的用例，断言不抛错、counts 正常渲染、`确认导入` 变为可点、「校验预览」按钮 `loading` 复位（用 `button.classList` 或 `is-loading` 断言），把这条回归钉死。

## Risks & Considerations

- 契约兼容性：把 `null` 改成 `[]` 是更严格的同义表示，`type: array` 本就允许空数组，无破坏性；无需数据迁移，不涉及写库。
- 前端兜底属"契约外防御"，必须写清注释与原因，避免后人当成冗余删掉；它不得替代后端修复。
- 同类隐患复查：`PermissionImportPreview` 现有四个数组字段中 `issues` / `warnings` 已归一、`movedItems` 是唯一漏项；`PermissionExportDocument.permissions` 在无权限点时已返回 `[]`（`buildExportTree` 第 748 行附近），暂无同类问题。建议顺手补一条"响应中所有数组字段非 nil"的断言，防止后续新增字段重犯。
- 可观测性：本次故障无任何服务端错误日志（响应是 200），纯前端渲染期异常，服务端审计也不会记录（预览本就不写审计）——这也是为什么它逃过了后端排查；建议修复时在前端 spec 里保留该场景，避免"接口正常即认为功能正常"的盲区。
- 无安全、性能影响。

## Open Questions

- [NEEDS CLARIFICATION: 复现时浏览器是否运行在 dev（`vite dev` / HMR）下？dev 下表现为"弹窗卡死"，生产构建下更可能表现为"弹窗内容变空"。若控制台能确认 `Cannot read properties of null (reading 'length')`，则根因闭合，无需继续分叉。]
- [NEEDS CLARIFICATION: 报告结尾的"admin端"是指问题**仅在 admin 复现**，还是"以 admin 为主"？按代码两端同构且门户侧只是把 `el-*` 换成 `El*`，缺陷应在两端同时存在；若门户侧确实不复现，需要额外确认门户那次导入的 `moved` 是否大于 0。]

# Bug Verification: 权限导入「校验预览」成功后弹窗卡死（movedItems 为 null 触发渲染异常）

- **Slug**: permission-import-preview-frozen
- **Tested**: 2026-11-04
- **Assessment**: ./assessment.md
- **Fix**: ./fix.md
- **Result**: partial

## Summary

修复在服务端（nil 切片归一为 `[]`）与前端（`movedItems` 空值安全 computed）两侧都生效：新增的两条前端回归用例与一条 Go 单测全绿，且反证实验证明"去掉前端修复即复现 `TypeError: Cannot read properties of null (reading 'length')`"。但 assessment 里列的是**真实浏览器 + 真实后端**的手动复现步骤，本次没有运行中的完整栈（需 MySQL/Redis/登录态），因此未在该形态下实机点过一次，按规范不判 `verified`。

## Checks Performed

| Check | Command / Action | Result | Notes |
|-------|------------------|--------|-------|
| Reproduction (post-fix) | 组件级等价复现：`npx vitest run src/views/{client,app-admin}/PermissionImportDialog.spec.ts`（mock 返回 `movedItems: null`） | pass | 弹窗正常渲染计数、确认按钮可点、预览按钮无 `is-loading` —— 症状不再出现 |
| Reproduction (post-fix, 实机) | assessment「Reproduction」1–5 步（浏览器 + 真实后端 + 真实 axios） | not-run | 本机无运行中的 sso/MySQL/Redis 与登录态；未在浏览器里实际点过 |
| Negation check（证明用例真正"钉住"缺陷） | `git stash` 掉两处 `.vue` 修复 → 重跑同一批用例 → `git stash pop` | pass | admin / portal 均报 `TypeError: Cannot read properties of null (reading 'length')`，各 1 条用例失败；恢复后全绿（本次验证沿用 fix 阶段记录，工作区已确认干净） |
| New / updated tests（Go） | `go test ./internal/service/ -run TestMovedItemsSerializesAsEmptyArray -v -count=1` | pass | `--- PASS`；断言计划产出与 nil 输入均序列化为 `[]` |
| New / updated tests（前端） | `npx vitest run src/views/client/PermissionImportDialog.spec.ts`（admin 8 用例）／`src/views/app-admin/PermissionImportDialog.spec.ts`（portal 4 用例） | pass | 含新增的 `movedItems 为 null 时仍完整渲染且可确认` |
| Regression suite（Go） | `go build ./...` + `go test ./internal/... -count=1` | pass | api / admin / auth / docs / dto / me / middleware / config / oidc / pkg / service / store 全 ok |
| Regression suite（前端） | `cd web/admin && npx vitest run`；`cd web/portal && npx vitest run` | pass | admin：8 文件 / 38 用例；portal：9 文件 / 64 用例 |
| Lint | `cd web/admin && npm run lint`；`cd web/portal && npm run lint` | pass | 两端 exit=0，无告警输出 |
| Format | `gofmt -d internal/service/app_permission_import_test.go` | pass | 改动区域无差异（`app_permission_import.go` 既有格式漂移刻意未动） |
| Scope check | `git status --short` | pass | 本步骤未改动源码；工作区仍只有 fix 阶段的 6 个文件 + 未跟踪的 `.specify/bugs/` |

## Output Excerpts

```
=== RUN   TestMovedItemsSerializesAsEmptyArray
--- PASS: TestMovedItemsSerializesAsEmptyArray (0.00s)
ok  	github.com/omino/omino-sso/internal/service	0.765s
```

```
 ✓ src/views/client/PermissionImportDialog.spec.ts (8 tests) 348ms
 Test Files  1 passed (1)
      Tests  8 passed (8)

 ✓ src/views/app-admin/PermissionImportDialog.spec.ts (4 tests) 199ms
 Test Files  1 passed (1)
      Tests  4 passed (4)
```

反证（临时移除 `.vue` 修复后）：

```
⎯⎯ Failed Tests 1 ⎯⎯
TypeError: Cannot read properties of null (reading 'length')
      Tests  1 failed | 7 passed (8)     # admin
      Tests  1 failed | 3 passed (4)     # portal
```

## Residual Risks

- **实机链路未覆盖**：真实浏览器 + 真实后端 + 真实 axios 的组合未执行（需要 MySQL/Redis/登录态）。理论上仍存在"后端在别的分支/旧镜像上返回 `null`"的部署错配风险——不过前端兜底已能吸收 `null`，即使后端未升级，弹窗也不会再冻结。
- **生产构建下的表象未实测**：dev 下是"DOM 冻结"，生产构建下同一异常会让该子树被替换为注释节点（弹窗内容变空）；本次只验证了 dev/vitest（jsdom）形态。
- **DSN 门控集成用例仍缺**：`preview.MovedItems != nil` 与真实响应含 `"movedItems":[]` 的 HTTP 层断言未加（本机无 MySQL），目前由纯函数 + JSON 序列化单测覆盖；若后续改动 `ValidateImport` 的装配路径，该保证需重新确认。
- **同类字段**：本次只对 `movedItems` 加了回归用例，`issues` / `warnings` / 导出侧 `permissions` 的空数组保证没有表驱动测试守护。

## Recommendation

修复本身可信（三层防护 + 反证成立 + 全量回归绿），但 assessment 记录的复现步骤是浏览器手点，本次未实机执行，故按规范记为 `partial`。建议由报缺陷的人在升级后的环境里点一次「校验预览」（`moved=0` 的文档，如往空应用导入两条）确认三件事——按钮 loading 复位、确认按钮可点、控制台无 `TypeError`——确认后即可关闭该 bug；若人工确认受阻，请保持 `partial`，不要按 `verified` 关闭。

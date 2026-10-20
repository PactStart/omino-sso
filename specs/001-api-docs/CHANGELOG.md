# Changelog: API 接口文档与自助调试门户

**Feature**: `001-api-docs` | **生成日期**: 2026-10-20 | **生成方式**: `/speckit.changelog.generate`

本文件记录本特性**设计产物（spec / plan / tasks / research / data-model / contracts / quickstart）的版本演进**，按需求粒度归类，非代码变更日志。数据来自 git 提交历史与逐版本 blob 比对；`.specify/spec.md` 不存在，故以 `specs/001-api-docs/` 为权威源。

格式遵循 Keep a Changelog 分类（Added / Changed / Removed / Fixed），版本按时间倒序。

---

## [未提交 · 工作区] — 2026-10-20（T025 闭环）

### Added

- `api-evolve/orchestration.md`：`before_implement` 门禁执行记录，5 条规则逐条判定 **PASS**（无 `diff.json` / `bump.json` / `deprecations.json` / `policy.yml`，不存在 breaking 或 removal 条目）
- `CHANGELOG.md`（本文件）
- `quickstart.md` 场景 5 执行记录：prod 实机冒烟（5 条文档路径全部 404，且与不存在路径同构）

### Changed

- `tasks.md` T025：`- [ ]` → `- [x]`，任务完成度 **31/32 → 32/32**（全特性任务清零）
- `quickstart.md` 前置条件：Go `1.24.x` → `1.26.x`（与 `plan.md`、`AGENTS.md` §2.1 口径对齐）

---

## [76f4d9f] — 2026-10-20 · `/speckit.analyze` 发现 H1/H2/H3 后的修订

### Changed（需求级）

- **FR-009**：原「记录运行时通用失败路径（404 / 429）」→ 追加「这类失败不属于任何单个端点，应记录在**全局说明**而非逐端点重复，且须给出两个结构的**真实响应体样例**」
- **SC-006**：原判定「新增端点后 `git status` **仅**显示 handler 文件 + `docs-check` 退出 0」→ 改写为「改动只允许三类文件（handler 注解 / `internal/api/router.go` 注册 / `make docs` 产物）+ `docs-check` 退出 0」，并显式声明「产物未重新生成时 `docs-check` **必须**失败」
- **plan.md** 口径同步：Go `1.24.1 → 1.26.0`；规范外依赖 `4 → 3`（`gin-swagger` 实施期弃用）；`kin-openapi` 版本天花板（`v0.136+` 需 go ≥ 1.25）标记失效；`internal/api/docs` 依赖面补 `swaggo/files`；E6 体积预算 `3–5 MB` → 实测 `+8.57 MB`；Constitution Gate 表与 Complexity Tracking 同步

### Added（任务级）

- **T031**：闭合 FR-009 —— 全局注解补 404 / 429 真实响应样例，新增 `TestGlobalFailurePathsDocumented`
- **T032**：落实 H1/H3 修订（`spec.md` SC-006 与 `plan.md` 口径）

### Fixed

- **原 SC-006 与 FR-012/FR-013 互斥**：新增端点必然要改 `router.go`，而产物未重新生成时 `docs-check` 必然失败，原判定三项无法同时成立。通过修改 SC-006 而非放宽 FR-012 解决 —— 防腐化优先级高于判定式简洁。

---

## [3d39d3e] — 2026-10-20 · 勾选 T017/T026/T029，回写实测结论

### Changed

- **E6 体积预算结案**：`quickstart.md` 场景 3 记录 `30,268,880 → 39,255,088 B`（+8.57 MB），超出 3–5 MB 预算，记入 `data-model.md` §8（`files v1.0.1` 内嵌 4 份 bundle + 4 份 `.map`，仅三个 `.map` 就占约 17 MB 明文）
- **研究项结案**：`research.md`「`openapi2conv` 示例保真度」→ 字段级 `example` 与 `x-` 扩展均保留；另记录 basePath 缺失的修复
- **CT-3 闭环**：`/docs/openapi.json` 实测返回构建版本与 `环境：dev`；真实 Chrome（headless + CDP）验收 16 个 operation / 6 个分组 / 外部域请求 0 个

---

## [e4a07d5] · [3960a70] · [abc7704] — 2026-10-20 · 实施期实测约束固化

### Changed（任务事实回写）

- **T004**（e4a07d5）：记录 basePath 折叠故障 —— `servers = "/"` 下 OpenAPI 3 无 basePath 承载者，必须把 `/api/v1` 折进每条 path，否则文档页 Try it out 全部 404
- **T030**（3960a70）：提交纪律核验 —— `openapi.json` 与 handler 注解同提交，`.tmp/` 未入库
- **T006–T028**（abc7704）：四处实测约束 ——
  1. `@x-auth-level` 必须是合法 JSON 值（裸 `admin` 使 swag 中断）；
  2. swag 的 responses 以状态码为键，同一 `200` 写两条只保留最后一条；
  3. gin 不能同时注册 `/docs/openapi.json` 与 `/docs/*any`（树冲突 panic），改为单 handler 内部分发；
  4. `{data=nil}` 会被渲染成 `{"type":"object"}`，故无数据端点不写 override

---

## [a91e5bb] — 2026-10-20 · 工具链改为独立 module

### Changed

- **T003** 重写：`tools.go` 纳入根 `go.mod` → 改为独立模块 `tools/go.mod`（`swag` CLI + `kin-openapi`）。理由：实测纳入根模块会强制升级 `golang.org/x/crypto v0.31.0 → v0.36.0` 等，违反 AGENTS.md §2.1 版本锁定
- `plan.md` / `research.md` 同步「构建期工具与运行时依赖物理隔离」结论

---

## [8add2fb] — 2026-10-20 · 登记依赖，关闭 T001/T002

### Added

- `AGENTS.md` §2.4「接口文档生成链」：登记 4 个依赖（`swag v1.16.6` / `kin-openapi v0.135.0` / `gin-swagger v1.6.1` / `files v1.0.1`，均 MIT）与锁定理由；§3 补 `.tmp/`、`tools/`、`internal/api/docs/`；§4.0 增 `/docs/*` 行与三条硬性约定（豁免统一响应、仅非 prod 注册、不挂限流）

### Changed

- **T001 spike 结论**：`Set-Cookie: sid=...` 同源后续请求**携带** → US1 的 Try it out 闭环成立，SC-002 不下调
- `plan.md` 依赖表由「`swag` 系列最新补丁」改为精确版本；注明 `kin-openapi v0.136.0+` 要求 `go >= 1.25`，故锁 `v0.135.0`

---

## [db33f08] — 2026-10-20 · 初版设计产物

### Added

- `spec.md`：3 个用户故事（P1 查阅调试 / P2 防腐化 / P3 prod 隔离）、**23 条 FR**（覆盖范围 3 / 内容完整性 6 / 生成维护 5 / 环境隔离 6 / 契约交付 3）、**6 条 SC**、7 条 Edge Cases、5 条澄清结论（协议端点不纳入、OpenAPI 3.0.x、示例用虚构数据、不新增 CI 配置、非 prod 无公网可达）
- `plan.md`：技术方案与 Constitution Gate、E1–E10 风险登记、Complexity Tracking（4 个规范外依赖）
- `tasks.md`：**30 个任务**（T001–T030，6 个阶段）
- `research.md` / `data-model.md` / `contracts/`（`doc-endpoints.md`、`annotation-conventions.md`）/ `quickstart.md` / `checklists/`

---

## Statistics

| Metric | Value |
|---|---|
| Spec 版本数（已提交） | 9（`db33f08` → `76f4d9f`），另有 1 个未提交版本 |
| 时间跨度 | 2026-10-20 单日（同日完成设计 → 实施 → 验收） |
| 需求单元总数 | 32（23 FR + 6 SC + 3 US） |
| 需求新增 | +32（全部在 `db33f08` 一次引入） |
| 需求修改 | 2（FR-009、SC-006，均在 `76f4d9f`） |
| 需求删除 | 0 |
| 任务总数 | 30 → **32**（`76f4d9f` 新增 T031/T032，属 analyze 补漏而非范围扩张） |
| 任务完成度 | 0/30 → 2/30 → 6/30 → 25/30 → 26/30 → 29/30 → 31/32 → **32/32** |
| Plan 修订次数 | 3（依赖版本精确化 → 独立 module → 实施事实回填） |
| 规范外依赖 | 4（计划）→ **3（落地）**：`gin-swagger` 实施期弃用 |
| 预算偏差 | 二进制体积预算 3–5 MB，实测 **+8.57 MB**（超 71%） |

> 任务完成度按 `tasks.md` 各提交版本中 `- [x]` 计数；`abc7704` 的 25 与提交标题「勾选 T006-T028」不完全一致，原因是个别任务在同一提交内仅回写实测说明而未勾选，以文件实际状态为准。

---

## Requirement Lifecycle

| Requirement | Added | Modified | Status |
|---|---|---|---|
| FR-001 端点覆盖（16 个，基线锁定） | db33f08 | — | Active |
| FR-002 鉴权三分类标注 | db33f08 | — | Active |
| FR-003 会话 Cookie 载体说明 | db33f08 | — | Active |
| FR-004 入参三区完整性 | db33f08 | — | Active |
| FR-005 成功响应结构 | db33f08 | — | Active |
| FR-006 错误码清单（六类适用部分） | db33f08 | — | Active |
| FR-007 统一响应封装复用一次 | db33f08 | — | Active |
| FR-008 分页参数默认值与上限 | db33f08 | — | Active |
| **FR-009 通用失败路径（404/429）** | db33f08 | **76f4d9f**（改为记入全局说明 + 须附真实响应体样例） | Active |
| FR-010 注解驱动生成、禁止手维护描述文件 | db33f08 | — | Active |
| FR-011 生成 / 校验 / 预览三目标 | db33f08 | — | Active |
| FR-012 校验非零退出且定位字段 | db33f08 | — | Active |
| FR-013 产物入库、与代码同处评审 | db33f08 | — | Active |
| FR-014 生成与校验离线可用 | db33f08 | — | Active |
| FR-015 非 prod 免登录可访问 | db33f08 | — | Active |
| FR-016 prod 不注册路由、响应同「路径不存在」 | db33f08 | — | Active |
| FR-017 环境判定单一来源 | db33f08 | — | Active |
| FR-018 复用管理 API 端口 | db33f08 | — | Active |
| FR-019 页面显示环境与版本 | db33f08 | — | Active |
| FR-020 不依赖外部网络资源 | db33f08 | — | Active |
| FR-021 OpenAPI 3.0.x 产出 | db33f08 | — | Active |
| FR-022 示例数据（虚构、无真实凭证） | db33f08 | — | Active |
| FR-023 命令与 CI 挂载写入 README | db33f08 | — | Active |
| SC-001 16 端点覆盖 100% | db33f08 | — | Active（Verified） |
| SC-002 10 分钟完成管理链路 | db33f08 | — | Active（Verified，Chrome 实测） |
| SC-003 1 分钟内报出一致性偏差 | db33f08 | — | Active（Verified） |
| SC-004 prod 404 命中 100% 且响应同构 | db33f08 | — | Active（Verified，T025 实机） |
| SC-005 离线 30 秒内生成 | db33f08 | — | Active（Verified，0.84s） |
| **SC-006 新增接口零手工维护** | db33f08 | **76f4d9f**（判定口径改为三类文件 + 声明 docs-check 必须失败） | Active |
| US1 自助查阅并调试（P1 / MVP） | db33f08 | — | Active（Delivered） |
| US2 文档随代码演进（P2） | db33f08 | — | Active（Delivered） |
| US3 prod 不暴露文档（P3） | db33f08 | — | Active（Delivered） |

> 无需求被删除或降级；唯一的口径冲突（SC-006 ↔ FR-012/FR-013）通过修改 SC-006 解决。

---

## 数据来源

```bash
git log --follow --format="%h|%ad|%s" --date=short -- specs/001-api-docs/spec.md
git log --follow --format="%h|%ad|%s" --date=short -- specs/001-api-docs/plan.md
git log --follow --format="%h|%ad|%s" --date=short -- specs/001-api-docs/tasks.md
git cat-file -p <commit>:specs/001-api-docs/tasks.md | grep -c "^- \[x\]"   # 逐版本完成度
git diff db33f08 76f4d9f -- specs/001-api-docs/spec.md                     # FR-009 / SC-006 修订
```

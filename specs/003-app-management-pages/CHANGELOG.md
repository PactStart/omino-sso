# Changelog: 应用详情子页面与应用内授权体系

**Feature**: `003-app-management-pages` | **生成日期**: 2026-10-21 | **生成方式**: `/speckit.changelog.generate`

本文件记录本特性**设计产物（spec / plan / tasks / data-model / contracts / quickstart）的版本演进**，按需求粒度归类，非代码变更日志。数据来自 git 提交历史与逐版本 blob 比对；`.specify/spec.md` 不存在，故以 `specs/003-app-management-pages/` 为权威源。

格式遵循 Keep a Changelog 分类（Added / Changed / Removed / Fixed），版本按时间倒序。

---

## [218e3a9] — 2026-10-21 · `/speckit.converge` 收敛验收留证

### Added（任务级）

- **T071**：userinfo 三段 curl 端到端留证（`conv-a`/`conv-b` 公共客户端 + `convtest`，授权码 + PKCE）
  - 成员含角色（`rA`，权限 `conv:read`/`conv:write`）→ `{"app_roles":["rA"],"permissions":["conv:read","conv:write"]}` ✓
  - 解绑 `rA` 后**同一 access_token** 立即再调 → `[]` / `[]`（无缓存）✓
  - 跨应用隔离（`conv-b` 令牌，成员但无角色）→ `[]` / `[]` ✓
- **T072**：200 节点权限树浏览器实测（`conv-a`，202 节点 / 3 层）
  - 树渲染（列表→树形单方向，5 次）：249–343ms，最大 343ms ≤1s ✓
  - 树↔列表切换（往返 5 次）：802–948ms（单方向约 400–475ms）✓
- `tasks.md` 新增 `## Phase 10: Convergence`

### Changed

- 任务完成度 68/70 → **70/72**（T071/T072 新增并勾选；T068/T069 保持未勾选为发布前门禁）

---

## [a03e3ac] — 2026-10-21 · `/speckit.analyze` 发现 F1–F8 后的修订 + 性能/接入文档

### Changed（需求级）

- **FR-004**：登出地址由「可编辑」改为「仅只读展示、不可编辑」
- **FR-015**：追加术语澄清——「应用内角色」与「应用角色」同义，均指 `client_roles`，菜单文案固定为「应用角色」
- **US1**：填补原模板占位 `[Describe this user journey in plain language]`，补一段中文故事概述
- **Edge Case（权限编码大小写）**：由「保存时若仅大小写不同需提示确认」改为「MUST 拒绝并提示编码已存在（无二次确认流）」
- **Edge Case（应用暂无成员）**：由「成员页 MUST 显式提示 + 黄色警告横幅」改为「表格空态 + 批量添加入口」

### Changed（计划与任务口径）

- `plan.md`：`ops` 断言 `19 → 35` → `19 → 36`（笔误修正）；`contracts/` 描述 `16 个管理端操作` → `17`；集成测试建库口径「`0001+0002` 两段」→「全量 `*.up.sql`（0001–0003）」
- `tasks.md` T066：补「覆盖 SC-001 完整闭环」；T070：补「200 节点浏览器计时（SC-004）」并细化 EXPLAIN 门禁（`memberCount` 严格、成员列表/候选关键字允许）
- `quickstart.md` / `data-model.md` / `contracts/admin-app-auth-api.md`：更正「排序不触发文件排序」的表述（成员列表跨表排序列、候选关键字前导通配符允许 filesort，真实门禁以 SC-002 为准）

### Added（文档）

- `docs/接入指南.md` 新增 §6.1「应用内角色与权限（userinfo 专属）」+ 术语表 / 能力边界 / 差异点（`app_roles`/`permissions` 仅进 userinfo、与平台 `roles` 互不推导）
- 任务完成度 66/70 → **68/70**（T066/T070 勾选并记录验收结果）

---

## [bdb989a] — 2026-10-21 · 初版设计产物（与实现同提交）

### Added

- `spec.md`：**37 条 FR**（US1 详情 6 / US2 成员与可见性 8 / US3 角色 4 / US4 权限 5 / US5 授权 3 / US6 userinfo 4 / 跨切面 7）、**8 条 SC**（SC-001~SC-008）、**6 个用户故事**（US1–US6，P1 两前半 + P2 四项）、7 条 Edge Cases、3 条澄清结论（成员制一律生效、权限树/列表同源、详情页可编辑字段范围）
- `plan.md`：技术方案、Constitution Gate（五原则 + 技术约束全 PASS）、数据模型（6 张表：`client_members` / `client_roles` / `client_permissions` / `client_role_members` / `client_role_permissions` / `access_tokens`）、9 个 Phase
- `tasks.md`：**70 个任务**（T001–T070，9 个阶段），完成度 66/70（T066/T068/T069/T070 待验收）
- `data-model.md` / `contracts/`（`admin-app-auth-api.md`、`userinfo-claims.md`）/ `quickstart.md` / `research.md` / `checklists/requirements.md`

---

## Statistics

| Metric | Value |
|---|---|
| Spec 版本数（已提交） | 3（`bdb989a` → `218e3a9`） |
| 时间跨度 | 2026-10-21 单日（设计 → 实施 → 验收） |
| 需求单元总数 | 51（37 FR + 8 SC + 6 US） |
| 需求新增 | +51（全部在 `bdb989a` 一次引入） |
| 需求修改 | 2（FR-004 登出地址只读、FR-015 术语澄清，均在 `a03e3ac`） |
| 需求删除 | 0 |
| 任务总数 | 70 → **72**（`218e3a9` 新增 T071/T072，属 converge 补漏而非范围扩张） |
| 任务完成度 | 66/70 → 68/70 → **70/72** |
| 遗留未勾选 | 2（T068 发布前补员、T069 conformance-suite，均为发布前门禁） |

> 任务完成度按 `tasks.md` 各提交版本中 `- [x]` 计数；`bdb989a` 的 66 与「实现完成」不完全一致，原因是个别验收类任务（T066/T070）留待验证留证，以文件实际状态为准。

---

## Requirement Lifecycle

| Requirement | Added | Modified | Status |
|---|---|---|---|
| FR-001 详情独立子页面入口 | bdb989a | — | Active |
| FR-002 五菜单固定顺序/刷新保持 | bdb989a | — | Active |
| FR-003 详情页只读字段 | bdb989a | — | Active |
| **FR-004 详情页可编辑字段** | bdb989a | **a03e3ac**（登出地址改只读） | Active |
| FR-005 标识与密钥只读 | bdb989a | — | Active |
| FR-006 非法输入拒绝 | bdb989a | — | Active |
| FR-007 成员列表字段 | bdb989a | — | Active |
| FR-008 成员筛选 | bdb989a | — | Active |
| FR-009 候选列表（排除已成员） | bdb989a | — | Active |
| FR-010 增量添加成员 | bdb989a | — | Active |
| FR-011 移除成员 | bdb989a | — | Active |
| FR-012 按部门整批加入 | bdb989a | — | Active |
| FR-013 成员制可见性 | bdb989a | — | Active |
| FR-014 停用账号语义 | bdb989a | — | Active |
| **FR-015 角色 CRUD** | bdb989a | **a03e3ac**（术语同义声明） | Active |
| FR-016 角色名唯一 | bdb989a | — | Active |
| FR-017 角色删除保护 | bdb989a | — | Active |
| FR-018 角色列表计数 | bdb989a | — | Active |
| FR-019 权限树/列表双视图 | bdb989a | — | Active |
| FR-020 权限编码规则 | bdb989a | — | Active |
| FR-021 权限父子结构 | bdb989a | — | Active |
| FR-022 权限删除保护 | bdb989a | — | Active |
| FR-023 权限被引用提示 | bdb989a | — | Active |
| FR-024 授权管理入口 | bdb989a | — | Active |
| FR-025 覆盖式权限授权 | bdb989a | — | Active |
| FR-026 覆盖式成员授权 | bdb989a | — | Active |
| FR-027 userinfo 返回 app_roles | bdb989a | — | Active（T071 留证） |
| FR-028 userinfo 返回 permissions | bdb989a | — | Active（T071 留证） |
| FR-029 空数组语义 | bdb989a | — | Active（T071 留证） |
| FR-030 即时生效 | bdb989a | — | Active（T071 留证） |
| FR-031 审计事件 | bdb989a | — | Active |
| FR-032 仅看无成员 | bdb989a | — | Active |
| FR-033 部门含子部门筛选 | bdb989a | — | Active |
| FR-034 乐观锁 updatedAt | bdb989a | — | Active |
| FR-035 门户空态引导 | bdb989a | — | Active |
| FR-036 列表成员数列 | bdb989a | — | Active |
| FR-037 审计快照 before/after | bdb989a | — | Active |
| SC-001 三分钟完整闭环 | bdb989a | — | Active（T066 覆盖） |
| SC-002 三接口 ≤1s | bdb989a | — | Active（Verified，18/242/4ms） |
| SC-003 userinfo 一致性 | bdb989a | — | Active（Verified，T071） |
| SC-004 权限树 200 节点 ≤1s | bdb989a | — | Active（Verified，343ms） |
| SC-005 跨页全选 | bdb989a | — | Active |
| SC-006 零回归门禁 | bdb989a | — | Active（Verified） |
| SC-007 无零成员窗口 | bdb989a | — | Active（发布前 T068 复核） |
| SC-008 发布后结果指标 | bdb989a | — | 上线后跟踪（非构建性） |
| US1 应用详情子页面（P1） | bdb989a | — | Active（Delivered） |
| US2 应用成员与门户可见性（P1） | bdb989a | — | Active（Delivered） |
| US3 应用角色管理（P2） | bdb989a | — | Active（Delivered） |
| US4 应用权限管理（P2） | bdb989a | — | Active（Delivered） |
| US5 授权管理（P2） | bdb989a | — | Active（Delivered） |
| US6 用户信息返回应用内角色与权限（P2） | bdb989a | — | Active（Delivered，T071 留证） |

> 无需求被删除或降级；唯一的口径修正（FR-004 登出地址只读、FR-015 术语）与两处 Edge Case 语义对齐，均属澄清而非回退。

---

## 数据来源

```bash
git log --follow --format="%h|%ad|%s" --date=short -- specs/003-app-management-pages/spec.md
git log --follow --format="%h|%ad|%s" --date=short -- specs/003-app-management-pages/plan.md
git log --follow --format="%h|%ad|%s" --date=short -- specs/003-app-management-pages/tasks.md
git show <commit>:specs/003-app-management-pages/tasks.md | grep -c '\- \[x\]'   # 逐版本完成度
git diff bdb989a a03e3ac -- specs/003-app-management-pages/spec.md              # FR-004/FR-015/Edge Case 修订
```

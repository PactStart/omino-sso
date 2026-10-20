---

description: "Task list for API 接口文档与自助调试门户"
---

# Tasks: API 接口文档与自助调试门户

**Input**: Design documents from `specs/001-api-docs/`

**Prerequisites**: `plan.md`（必需）、`spec.md`（用户故事来源）、`research.md`、`data-model.md`、`contracts/`、`quickstart.md`、`critiques/`

**Tests**: 本特性在 `plan.md` 的 Testing 段与 `contracts/` 的测试锚点中**显式要求**测试，故各阶段包含测试任务；但**不采用 TDD 顺序**（先实现后补测），因为多数断言依赖已生成的产物。

**Organization**: 任务按用户故事分组，每个故事可独立实现与验证。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 可并行（不同文件、无未完成依赖）
- **[Story]**: 所属用户故事（US1 / US2 / US3）
- 描述中给出精确文件路径

## Path Conventions

单一 Go 模块，仓库根即项目根：

- 源码：`cmd/`、`internal/`
- 构建期工具：`tools/`
- 生成产物：`internal/api/docs/openapi.json`
- 特性文档：`specs/001-api-docs/`

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: 先验证可行性、再同步规范、再引入依赖。**顺序不可调换**——宪法要求"先改规范再改代码"，而 `quickstart.md` 场景 0 的 spike 决定 US1 是否要缩水，必须先做。

- [x] T001 ✅ **已完成 2026-10-20** — 执行 `quickstart.md` 场景 0 的可行性 spike：`APP_ENV=dev make dev` 后登录，确认响应下发 `Set-Cookie: sid=...` 且同源后续请求携带 `Cookie: sid=...`。**结论：携带，US1 的 Try it out 闭环成立，SC-002 不下调**。已回写 `specs/001-api-docs/research.md`「未决与风险登记」；残余验证点（用真实 `/docs/` 页面复核）保留在 T017 中
- [x] T002 ✅ **已完成 2026-10-20** — 更新 `AGENTS.md`：新增 §2.4「接口文档生成链」登记 4 个依赖的锁定版本、许可证与锁定理由（`swag v1.16.6` / `kin-openapi v0.135.0` / `gin-swagger v1.6.1` / `files v1.0.1`，均 MIT）；§3 目录树新增 `.tmp/`、`tools/go.mod`、`tools/openapi3/`、`internal/api/docs/`；§4.0 新增 `/docs/*` 行与三条硬性约定（豁免统一响应、仅非 prod 注册、不挂限流）。**关键发现**：`kin-openapi v0.136.0+` 要求 `go >= 1.25`，故锁定 `v0.135.0`（`go 1.22.5`）
- [x] T003 ✅ **已完成 2026-10-20** — 建立 `tools/` 独立 module（`module github.com/omino/omino-sso/tools`），require 锁定 `swaggo/swag v1.16.6` + `getkin/kin-openapi v0.135.0`；`tools/tools.go` 用 `//go:build tools` + blank import 固定 swag CLI，防止 `go mod tidy` 掉依赖。`.gitignore` 已加 `.tmp/`。**两处与原计划的偏离（已核实）**：① `tools/go.mod` 的 `go` 指令为 `1.26.0` 而非计划的 `1.24.1`，因主模块同日升级到 Go 1.26.0（见 AGENTS.md §2.1）；② `gin-swagger` / `files` 未进 tools，它们是 `/docs/` 的**请求路径**依赖，归属在 T008 决定。**根模块校验**：`go.sum` 与 HEAD 逐字节一致，`go.mod` 仅剩 Go 版本升级，`swaggo/swag`、`kin-openapi` 均未进入根模块，`golang.org/x/crypto` 仍锁 `v0.31.0`

**Checkpoint**: 规范与依赖就位，`go build ./...` 仍通过。

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: 生成链、嵌入包、路由与环境门禁、响应类型具名化。**任何用户故事都依赖本阶段**，必须整体完成后再进入 US1。

- [x] T004 ✅ **已完成 2026-10-20** — `tools/openapi3/main.go` 的 `convert()` 用 `openapi2conv.ToV3` 转换，强制 `openapi = 3.0.3`、`servers = [{url:"/"}]`，输出前 `MkdirAll` 保证 `internal/api/docs/` 目录存在；默认参数即 `-in .tmp/swagger/swagger.json -out internal/api/docs/openapi.json`
- [x] T005 ✅ **已完成 2026-10-20** — `-check` 模式用 `openapi3` 加载已提交产物与新生成文档，比对 operation 增删、`summary`、参数、requestBody 与 `components.schemas` 字段，输出 `METHOD /path: <定位>` 行并按路径排序后以退出码 1 结束（读文件/解析失败为 2）；`tools/openapi3/main_test.go` 覆盖「无差异 → 0」「漂移 → 1 且输出含字段名」两个样例，`go test ./...` 通过
- [ ] T006 写入全局注解：在 `cmd/sso/main.go` 顶部添加 `@title`、`@version 1.0`、`@description`（两行，含「协议端点不在本文档范围」）、`@BasePath /api/v1`、`@securityDefinitions.apikey cookieAuth`、`@in cookie`、`@name sid`；仅注释，不改任何逻辑
- [ ] T007 新增文档包 `internal/api/docs/docs.go`：`//go:embed openapi.json` 嵌入产物；提供 gin handler 响应 `GET /docs/openapi.json`，读取内嵌字节后把 `info.version` 覆写为包级变量 `Version`（由 `-ldflags` 注入，缺省回退 `debug.ReadBuildInfo()`，最终兜底 `"dev"`）、把运行环境写入 `info.description`（格式 `环境：<APP_ENV>`）；`internal/api/docs/docs_test.go` 断言版本回退链与环境注入
- [ ] T008 注册文档路由并隔离环境：在 `internal/api/router.go` 用 `if cfg.AppEnv != "prod"` 包裹 `/docs/*any` 与 `/docs/openapi.json` 注册（挂在 `v1` 组之外、复用 `internal/api/docs` 的 handler 与 `gin-swagger` + `swaggo/files`）；同时在 `internal/api/middleware/ratelimit.go` 对 `/docs/` 前缀提前放行，使文档页不依赖 Redis；`internal/api/router_test.go` 断言 prod 下两者均不注册（CT-4、CT-5）
- [x] T009 ✅ **已完成 2026-10-20** — 8 处 `gin.H` 已全部替换为具名 DTO（`auth/login.go` 两处、`auth/mfa.go`、`admin/clients.go` 两处、`me/profile.go` 两处、`admin/audit.go`），JSON tag 与旧键逐字对应、无 `omitempty`、`mfaTicket`/`clientSecret`/`expiresAt` 的 `null` 语义保留；`internal/api/dto/dto_test.go` 提供 8 项语义等价性单测（`canonical()` 反序列化为 map 后 `reflect.DeepEqual` + 排序键集合逐一比对），全量 `go test ./...` 通过。**附注**：`dto_test.go` 的对照组直接用 `map[string]any` 字面量而非 `gin.H`，二者类型等价（`gin.H` 即 `map[string]any` 的别名），避免测试反向依赖 gin。以下为原始任务描述，保留备查：具名化 8 处匿名响应：在 `internal/api/dto/dto.go` 新增 `LoginMfaChallenge`、`LoginResult`、`MfaResult`、`AppItem`、`SessionItem`、`CreatedClient`、`RotatedSecret`、`AuditItem`（字段名与 JSON tag 逐字对应现有 `gin.H` 键；`mfaTicket`/`clientSecret` 用指针表达可空；`RotatedSecret.expiresAt` 保留恒为 `null` 的键；**禁止** `omitempty`、**禁止**合并键集不同的分支），替换 `internal/api/auth/login.go:79`、`:112`、`internal/api/auth/mfa.go:63`、`internal/api/admin/clients.go:75`、`:105`、`internal/api/me/profile.go:58`、`:130`、`internal/api/admin/audit.go:54` 的 `gin.H` 构造；在 `internal/api/dto/dto_test.go` 按 AC-7 写等价性单测（同值构造 `gin.H` 与结构体 → `json.Unmarshal` 后 `reflect.DeepEqual` 为真且键集合相同）

**Checkpoint**: `make docs` 能产出 `openapi.json` 且 `make build && go test ./...` 通过；此阶段结束时 16 个端点尚无注解，文档 `paths` 为空属预期。

---

## Phase 3: User Story 1 - 内部前端研发与后端维护者自助查阅并调试接口 (Priority: P1) 🎯 MVP

**Goal**: 16 个管理端点全部具备可读、可调用的文档条目，研发能在文档页内完成一轮真实调用。

**Independent Test**: `quickstart.md` 场景 4 —— 非 prod 启动后，仅用浏览器，不查源码，完成「登录 → 创建用户 → 查询用户列表」并得到正确响应。

**Tests**: 注解完整性由 `T016` 与 `T017` 的断言覆盖（AC-1~AC-6）。

- [ ] T010 [P] [US1] 为认证端点补注解：在 `internal/api/auth/login.go` 的 `Login`、`Logout` 与 `internal/api/auth/mfa.go` 的 `VerifyMFA` 上按 `contracts/annotation-conventions.md` §2 骨架写全 `@Summary`/`@Description`/`@Tags 认证`/`@Param`/`@Success`/`@Failure`/`@Router`；`Login` 的两个分支各用一个 `@Success 200`，`data` 分别写 `dto.LoginMfaChallenge` 与 `dto.LoginResult`；`public` 端点不写 `@Security`
- [ ] T011 [P] [US1] 为「我的」端点补注解：在 `internal/api/me/profile.go` 的 `Profile`、`Apps`、`ChangePassword`、`Sessions`、`RevokeSession` 上补注解，`data` 分别为 `dto.UserDetail`、`[]dto.AppItem`、`nil`、`[]dto.SessionItem`、`nil`；全部标 `@Security cookieAuth`；`Sessions` 与 `RevokeSession` 的路径参数写作 `{sid}`
- [ ] T012 [P] [US1] 为用户与组织端点补注解：在 `internal/api/admin/users.go` 的 `ListUsers`、`CreateUser`、`UpdateUser`、`OrgTree` 上补注解，`data` 分别为 `response.Page{list=[]dto.UserAdmin}`、`dto.UserAdmin`、`dto.UserAdmin`、`[]admin.OrgNode`；全部标 `@Security cookieAuth` 与 `@x-auth-level admin`；`UpdateUser` 路径参数写 `{id}`
- [ ] T013 [P] [US1] 为应用与审计端点补注解：在 `internal/api/admin/clients.go` 的 `ListClients`、`CreateClient`、`RotateSecret` 与 `internal/api/admin/audit.go` 的 `ListAuditLogs` 上补注解，`data` 分别为 `response.Page{list=[]dto.Client}`、`dto.CreatedClient`、`dto.RotatedSecret`、`response.Page{list=[]dto.AuditItem}`；`CreateClient` 的 `@Description` 必须写明「`clientSecret` 仅此一次返回」
- [ ] T014 [US1] 补示例数据：按 `contracts/annotation-conventions.md` §5 在各 `dto` 结构体字段加 `example:"..."` tag，写操作端点补请求体示例；`token`/`clientSecret`/`password` 类字段统一用占位串，个人信息用虚构值，同一实体的示例值跨端点保持一致（INV-9、FR-022）
- [ ] T015 [US1] 校验错误码覆盖：逐端点检查 `@Failure` 是否满足 `contracts/annotation-conventions.md` §2.1 的最低要求（全部含 `500`；需登录含 `401`；管理端点含 `401`+`403`；写操作含 `400`），业务码只能取自 `internal/api/response/errors.go`（INV-11）
- [ ] T016 [US1] 编写文档结构断言：在 `internal/api/docs/docs_test.go` 增加断言——`openapi == "3.0.3"`；`paths` 展开后的 `(method,path)` 组合数为 16 且与 `router.go` 的 `r.Routes()` 中 `/api/v1` 前缀集合一致（AC-1）；每个 operation 都有 `summary`/`tags`/`1 个 2xx`/`500`（AC-2）；`components.schemas` 无逐接口信封且名字不重复（AC-3、AC-4）
- [ ] T017 [US1] 补齐文档页的版本与环境显示并人工验收：确认 `GET /docs/openapi.json` 的 `info.description` 含当前 `APP_ENV`、`info.version` 为构建版本（CT-3）；按 `quickstart.md` 场景 4 完成浏览器端「登录 → 创建用户 → 查询用户列表」全链路，并勾选 Disable cache 确认无外部域请求（FR-020、SC-002）

**Checkpoint**: US1 独立可交付——文档页可读可调，16 端点齐备。

---

## Phase 4: User Story 2 - 文档随代码演进保持一致 (Priority: P2)

**Goal**: 一条命令生成、一条命令查漂移、一条命令本地预览；`make build` 不再可能发布陈旧文档。

**Independent Test**: `quickstart.md` 场景 2 —— 连续两次生成产物哈希一致；给任一接口加必填参数后 `make docs-check` 退出码 1 并定位到字段；恢复后退出码 0 且不修改工作区。

**Tests**: 由 T020 的生成幂等性与 T005 的比对单测共同覆盖。

- [ ] T018 [US2] 在 `Makefile` 新增 `docs` 目标：先 `go run github.com/swaggo/swag/cmd/swag init -g cmd/sso/main.go -o .tmp/swagger --outputTypes json`，再 `go run ./tools/openapi3` 产出 `internal/api/docs/openapi.json`；不得带 `@version`，不得依赖网络（FR-014、E9）
- [ ] T019 [US2] 在 `Makefile` 新增 `docs-check` 目标：生成到临时目录后由 `tools/openapi3 -check` 与已提交产物比对，非零退出并打印字段级定位；**不得修改工作区文件**（FR-012）
- [ ] T020 [US2] 在 `Makefile` 新增 `docs-serve` 目标并把 `build` 改为依赖 `docs-check`：`docs-serve` 先执行 `docs` 再启动服务并回显文档地址；`build` 在编译前执行 `docs-check`，并在编译时通过 `-ldflags "-X github.com/omino/omino-sso/internal/api/docs.Version=$(VERSION)"` 注入版本（`VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)`）（E1）
- [ ] T021 [US2] 执行生成幂等性验证：连续两次 `make docs` 后比对 `internal/api/docs/openapi.json` 的哈希必须一致（INV-2、场景 2 前半），不一致则修正 swag 输出排序或转换工具的 map 遍历顺序
- [ ] T022 [US2] 更新 `README.md`：在「开发流程规范（Spec Kit）」段落补充 `make docs` / `make docs-check` / `make docs-serve` 三条命令、一行可直接粘贴到流水线的 `make docs-check`、以及文档入口 `http://127.0.0.1:8081/docs/`（仅非 prod）（FR-023、P5）

**Checkpoint**: US1 + US2 交付后，文档不再腐化。

---

## Phase 5: User Story 3 - 生产环境不暴露接口文档 (Priority: P3)

**Goal**: 生产环境默认关闭且请求表现与"路径不存在"完全一致；阻断文档关闭的旁路。

**Independent Test**: `quickstart.md` 场景 5 —— `APP_ENV=prod` 启动后，5 条文档路径全部返回 404 且响应体与不存在路径同构，业务接口仍正常返回 400。

> 环境门禁的实现已在 T008 完成（`router.go` 单点 `cfg.AppEnv` 判断）。本阶段交付**独立验证与加固**，不重复实现。

**Tests**: T023、T024 即本故事的测试交付物。

- [ ] T023 [P] [US3] 编写 prod 隔离断言：在 `internal/api/router_test.go` 覆盖 CT-4（`/docs/index.html` 与 `/docs-absent` 的响应体除 `traceId` 外完全一致、均为 `NOT_FOUND`）与 CT-5（`/docs/openapi.json` 返回 404 且不含任何 `openapi` 字段）
- [ ] T024 [P] [US3] 审查环境判断单一来源：全仓搜索 `AppEnv` 与 `"prod"` 字面量，确认除 `internal/api/router.go` 的文档门禁与既有 `gin.SetMode` 判断外无第三处环境分支（FR-017）；若发现散落判断，收敛到 `router.go`
- [ ] T025 [US3] 执行 prod 冒烟：按 `quickstart.md` 场景 5 逐路径验证 404 与响应体同构，并确认 `POST /api/v1/auth/login` 仍返回 400（业务无回归）；同时在 `specs/001-api-docs/plan.md` 的「已接受风险」确认「prod 二进制含文档资源但不可路由」一项已被记录（E5）

**Checkpoint**: 三个故事全部独立可验收。

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: 全门禁回归、文档一致性收尾、提交纪律检查。

- [ ] T026 [P] 执行全门禁：`make build`（含 `docs-check`）、`go test ./...`、`make lint`、`make build-web` 全部通过；记录 `ls -lh bin/sso` 体积并与改造前对比，把实测增量回写 `specs/001-api-docs/data-model.md` §8（E6）
- [ ] T027 [P] 执行 Redis 可用性验收：按 `quickstart.md` 场景 4 的 CT-7 步骤停掉 Redis 后访问 `/docs/index.html` 与 `/docs/openapi.json`，期望均为 200，验证限流前缀跳过已生效（E2/E3）
- [ ] T028 [P] 执行离线生成验收：`GOPROXY=off make docs` 退出码必须为 0，验证 swag CLI 已随 `go.mod` 进入本地缓存（E9、场景 1 断网复跑）
- [ ] T029 收尾 `quickstart.md` 全部场景（0–6）并核对 `contracts/` 的 CT-1~CT-7、AC-1~AC-7 是否均有对应实现或测试；更新 `specs/001-api-docs/research.md` 中「`openapi2conv` 示例保真度」的待验证项结论
- [ ] T030 提交纪律检查：确认 `internal/api/docs/openapi.json` 与其对应的 handler 注解改动在同一提交内，无跨提交拆分（E10）；临时目录 `.tmp/` 未被纳入版本控制

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1（Setup）**: 无依赖。T001 → T002 → T003 严格串行（spike 结论可能改变范围；规范必须先于代码；依赖必须先于实现）
- **Phase 2（Foundational）**: 依赖 Phase 1 全部完成。T004 与 T005 同文件，须串行（先转换后比对）；T006/T007/T008/T009 相互独立，可并行
- **Phase 3（US1）**: 依赖 Phase 2 全部完成。T010–T013 可并行；T014/T015 依赖 T010–T013；T016/T017 依赖 T014/T015
- **Phase 4（US2）**: 依赖 Phase 2 完成；T018–T022 内部串行（T018 → T019 → T020 → T021 → T022）。**不依赖 US1**——注解未齐时 `docs-check` 同样可工作
- **Phase 5（US3）**: 依赖 T008 完成；T023/T024 可并行，T025 依赖前两者
- **Phase 6（Polish）**: 依赖 US1 + US2 + US3 全部完成

### Story Completion Order

```
Phase 1 → Phase 2 ──┬─→ US1 (P1, MVP) ──┐
                     ├─→ US2 (P2)        ├─→ Phase 6
                     └─→ US3 (P3) ───────┘
```

US1 / US2 / US3 在 Phase 2 之后**可并行推进**，三者改动的文件几乎不重叠（US1 改 handler 注解、US2 改 `Makefile`/`README`、US3 改测试与审查）。

---

## Parallel Execution Examples

### Phase 2

```bash
# T006 / T007 / T008 / T009 分属不同文件，可同时开工：
Task: "写入全局注解到 cmd/sso/main.go"                     # T006
Task: "新增 internal/api/docs/docs.go 与 docs_test.go"     # T007
Task: "在 internal/api/router.go 注册 /docs/* 并隔离 prod"  # T008
Task: "在 internal/api/dto/dto.go 新增 8 个具名响应类型"     # T009
```

### Phase 3（US1）

```bash
Task: "auth 包 3 个 handler 注解 in internal/api/auth/"      # T010
Task: "me 包 5 个 handler 注解 in internal/api/me/profile.go" # T011
Task: "admin users 4 个 handler 注解"                        # T012
Task: "admin clients + audit 注解"                           # T013
```

### Phase 5（US3）

```bash
Task: "prod 隔离断言 in internal/api/router_test.go"  # T023
Task: "环境判断单一来源审查（只读）"                   # T024
```

---

## Implementation Strategy

### MVP First（仅 US1）

1. Phase 1 Setup —— 尤其 T001 的 spike，结论可能直接改变 US1 的验收口径
2. Phase 2 Foundational —— **关键阻塞**，尤其 T009 具名化：不做完，注解拿不到可引用类型
3. Phase 3 US1 —— 16 端点注解 + 示例
4. **STOP & VALIDATE**：跑 `quickstart.md` 场景 4，浏览器内完成一轮真实调用
5. 此时文档已可用，但**尚未防腐化**（无 `docs-check`），不建议在此状态停留超过一个迭代

### Incremental Delivery

1. Setup + Foundational → 生成链通，文档为空壳
2. + US1 → 可查阅可调试（MVP），交给前端试用并收集反馈
3. + US2 → 新增漂移检测与构建门禁，文档进入受控状态
4. + US3 → 生产隔离完成，可发布
5. + Polish → 体积、Redis、离线三项横切验收

### 风险驱动的排期建议

- T001 若返回「Cookie 不携带」，**先停**：需要评估 swagger-ui 的 `withCredentials` 补齐成本，再决定 US1 是否降级为"阅读 + 手工 curl"。此结论影响 SC-002 与 US1 验收动作，越晚发现返工越大
- T009 若发现某处 `gin.H` 的键集与 `contracts/annotation-conventions.md` §4 记载不符，以源码为准并回写契约表，**不要**为迁就文档而改字段名

---

## Notes

- [P] 任务 = 不同文件、无未完成依赖
- 标注 `critique` 编号的任务来自 `critiques/critique-20261020-132851.md`，均为 Must-Address 或强建议的落地
- 每个 Phase 结束都有 Checkpoint，建议在 Checkpoint 处提交一次，便于定位回归
- 本特性**不新增表、不写迁移**，任何要求改 `migrations/` 的实现方向都应立即停止并复核
- 禁止顺手重构：`router.go` 的中间件顺序、`response` 包结构与既有 handler 业务逻辑一律不动

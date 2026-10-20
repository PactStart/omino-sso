# Phase 0 Research: API 接口文档与自助调试门户

**Feature**: `001-api-docs` | **Date**: 2026-10-20

Technical Context 中不存在 `NEEDS CLARIFICATION` 标记（本特性的技术选择空间已被 spec 的 4 条澄清与项目锁定栈压缩到唯一可行路径）。以下 7 项决策仍逐一记录依据，供评审与后续维护者复核。

---

## D1. 注解解析工具：`swaggo/swag`（v1.16.x）

- **Decision**: 使用 `github.com/swaggo/swag` 的注解语法与 CLI 作为唯一注解来源。
- **Rationale**:
  1. 用户诉求明确定位为"swagger 文档注释"，Go 生态中注解驱动的事实标准就是 swag 的 `// @Summary` / `// @Param` / `// @Success` 注释族，接入成本最低；
  2. 静态解析源码，不依赖运行时反射，因此**不需要启动应用、不需要连 MySQL/Redis**即可生成（直接满足 FR-014）；
  3. 与 Gin 的 handler 签名天然契合（`func(c *gin.Context)`），16 个 handler 无需改签名；
  4. 版本线稳定，`v1.16.x` 为长期维护分支补丁序列，符合本项目"版本锁定、拒绝 alpha"的取向。
- **Alternatives considered**:
  - `swaggo/swag/v2`（开发分支）：目标是 OpenAPI 3.x，可省去转换步骤，但长期处于 alpha，注释语法与 v1 存在差异，且与"版本一经锁定不得随意升级"冲突 → 拒绝。
  - `swaggest/openapi-go`：可程序化产出 OpenAPI 3.0，但以 Go 代码构建 schema，等价于把文档写进代码逻辑，**不符合** FR-010"由源码中的结构化注释生成"，且会让 handler 被文档代码污染 → 拒绝。
  - `danielgtaylor/huma`：以 handler 签名为契约自动推导，但要求改造整个 API 层（框架替换），违反"不重构既有分层" → 拒绝。

## D2. OpenAPI 3.0 产出路径：`swag`(2.0) → `kin-openapi/openapi2conv`(3.0.3)

- **Decision**: 生成链为两步：`swag init` 产出 Swagger 2.0 JSON（写入临时目录）→ `tools/openapi3` 用 `openapi2conv.ToV3` 转为 OpenAPI 3.0.3（写入 `internal/api/docs/openapi.json`）。
- **Rationale**:
  1. **swag 原生只支持 Swagger 2.0**。其仓库定位描述即为 "Automated Swagger 2.0 Documentation for Go"，OpenAPI 3 支持自 2019 年起以 proposal 形式挂在 issue #548，至今未合入主线。网络上"加 `// @openapi 3.0.0` 注释即可输出 3.0"的说法与该事实不符，不可作为依据。
  2. `openapi2conv` 是 kin-openapi 的官方子包，专门做 2.0 → 3.0 的结构映射（`definitions`→`components.schemas`、`host/basePath/schemes`→`servers`、`body` 参数→`requestBody`、`securityDefinitions`→`securitySchemes`），比自行拼装可靠。
  3. 转换是纯内存操作，无外部依赖，天然满足离线要求。
- **Alternatives considered**:
  - 直接交付 Swagger 2.0 → 违反 FR-021（澄清已锁定 3.0.x），且接入方代码生成器普遍只认 3.0+ → 拒绝。
  - `npx swagger2openapi`：给 Go 项目引入 Node 工具链，离线与可重现性都更差 → 拒绝。
  - 用 `kin-openapi` 从零构造 3.0 文档（丢弃 swag）：等价于放弃注解解析 → 拒绝。
  - **一次性脚本把 `AGENTS.md` §4 的接口表格转成 OpenAPI**（critique P4 提出）：零运行时依赖、无转换链，但产出物与源码脱钩，手写表格必然腐化，且直接违反 FR-010 → 拒绝。仅作为最后的降级方案（若 4 个依赖均被否决）保留在案。

## D3. 文档 UI：`gin-swagger` + `swaggo/files`（资源内嵌）

- **Decision**: 用 `ginSwagger.WrapHandler(swaggerfiles.Handler, ginSwagger.URL("/docs/openapi.json"))` 在 `/docs/*any` 提供 UI，静态资源来自 `swaggo/files` 的嵌入文件系统。
- **Rationale**:
  1. `swaggo/files` 把 swagger-ui 的 HTML/JS/CSS 全部 `embed` 进 Go 包，二进制自带、**不请求任何外部 CDN**，直接满足 FR-020 的离线要求（内网部署是 IdP 的常态）；
  2. swagger-ui 自 3.0 起原生支持 OpenAPI 3.0 文档渲染，因此把 UI 指向转换后的 3.0 文件可行；
  3. 与 Gin 集成只需一行注册，不引入新的中间件栈，不干扰 `:8080`。
- **Alternatives considered**:
  - `swaggest/swgui` + `v5emb`：同样内嵌、OAS3 支持更好，但需要额外维护 spec 路由与 UI 配置，收益不足以抵消新增依赖 → 备选保留。
  - RapiDoc / ReDoc：默认从 CDN 拉取，需自行打包资源 → 与 FR-020 冲突，拒绝。
  - 自研静态页 + `redoc` 打包：等于重写已经成熟的组件 → 拒绝。

## D4. 生成产物落位：`internal/api/docs/openapi.json`

- **Decision**: 生成的 3.0 文件放在 `internal/api/docs/` 包目录内，由同包的 `docs.go` 通过 `//go:embed openapi.json` 嵌入；swag 的 2.0 中间产物写入 `.tmp/swagger/`（已 gitignore）。
- **Rationale**:
  1. `//go:embed` 只能引用**当前包目录及其子目录**内的文件，放仓库根 `docs/` 会导致无法嵌入，除非再复制一份（产生两份真源，漂移基准含糊）；
  2. 中间产物不入库，评审时 `git diff` 只看得到最终格式变化，不会被 2.0/3.0 两份 JSON 的噪声淹没（FR-013 的可评审性）；
  3. 不新增顶层目录，结构增量控制在 `internal/api/` 之内。
- **Alternatives considered**:
  - 根 `docs/` + `go:generate` 复制：双真源 → 拒绝。
  - 两个版本都入库：`git diff` 噪声翻倍，且易出现"改了 2.0 忘改 3.0"→ 拒绝。

## D5. 环境隔离实现点：`router.go` 单处 `cfg.AppEnv` 判断

- **Decision**: 在 `internal/api/router.go` 中，以 `if cfg.AppEnv != "prod"` 包裹 `docs` 路由注册；其余任何位置不出现环境判断。
- **Rationale**:
  1. FR-017 要求"由运行环境单一配置项决定、禁止多处重复判断"；`router.go:30` 已有 `cfg.AppEnv == "prod"` 用于 Gin ReleaseMode，与之同源，不引入第二个判定源；
  2. prod 下**不注册**路由（而非注册后中间件拦截），使请求落入既有 `NoRoute` 分支，返回的统一 404 与任意不存在路径**完全一致**（仅 `traceId` 每次不同）——这正是 FR-016/SC-004 的判定方式；
  3. 无需新增环境变量（宪法"不加配置项"）。
- **Alternatives considered**:
  - 注册路由 + 中间件按环境返回 404：`r.Routes()` 里仍能看到该路由，且多一条可能被漏配的分支 → 拒绝。
  - 在 `config` 里新增 `DOCS_ENABLED`：增加配置面，且与"非 prod 即可访问"的语义重复 → 拒绝。

## D6. 漂移检测策略：临时目录重生成 + 语义比对

- **Decision**: `make docs-check` 先 `swag init` 到临时目录、转换为 3.0，再由 `tools/openapi3 -check` 用 `kin-openapi/openapi3` 加载"已提交"与"刚生成"两份文档，逐 path / method / parameter / schema field 比对，输出差异清单并以非零码退出；工作区文件不被修改。
- **Rationale**:
  1. FR-012 要求"定位到具体端点或字段"，纯文本 diff 对 JSON 只能给出行号，语义比对才能报出 `POST /api/v1/admin/users: request.body.email.type` 这种定位；
  2. 不写回工作区，保证"检查"与"生成"职责分离，且 CI 中可安全执行；
  3. 比对逻辑可单测（正例：无差异返回 0；反例：新增必填参数返回 1 并含字段名）——覆盖 SC-003。
- **Alternatives considered**:
  - `git diff --exit-code`：只能报"文件变了"，无法定位字段，且会污染工作区 → 拒绝。
  - 哈希比对：能判等但不能定位，且对字段顺序敏感（swag 输出顺序可能波动）→ 拒绝。

## D7. 文档页显示环境与版本（FR-019）：运行时改写 info 块

- **Decision**: 用自定义 handler 响应 `GET /docs/openapi.json`：读取内嵌文档 → 把 `info.version` 覆写为构建版本、把环境标识追加进 `info.description` → 序列化返回。UI 渲染 `info` 区域时即展示二者。
- **Rationale**:
  1. swagger-ui 的页头信息完全来自文档的 `info` 块，无需改 UI 资源或维护自定义 HTML 模板；
  2. 生成产物保持"环境无关"，同一份 `openapi.json` 在 dev/test/prod 通用，避免把环境写进版本库文件（否则每次切环境都会产生假漂移）；
  3. 可用 `curl /docs/openapi.json | jq .info` 直接断言，测试成本低。
- **Alternatives considered**:
  - 自定义 `index.html` 注入横幅：需要维护 swagger-ui 的 HTML 模板，随依赖升级易碎 → 拒绝。
  - 生成时写入环境：同一文件无法适配多环境，且造成漂移误报 → 拒绝。

## D8. 版本来源：`-ldflags` 注入 + `debug.ReadBuildInfo` 兜底

- **Decision**: `Makefile` 的 `build` 目标通过 `-X github.com/omino/omino-sso/internal/api/docs.Version=$(VERSION)` 注入；`VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)`。未注入时（`go run`）回退到 `debug.ReadBuildInfo()` 的 vcs 信息，最终兜底 `dev`。
- **Rationale**: 不新增配置项、不新增构建脚本文件；`go run`/`go build` 两条路径都能给出可辨识版本，满足 FR-019 "使研发确认自己调用的实例"。
- **Alternatives considered**: 读 `APP_VERSION` 环境变量 → 增加配置面且容易忘设；写死在 `config` → 每次发版要改代码。

## D9. 依赖版本锁定与规范同步

- **Decision**: 4 个新依赖在实施时以 `go get` 解析到的最新稳定补丁版为准，落定后把**精确版本、许可证、维护状态**三列写回 `AGENTS.md` §2 锁定表并注明理由（"接口文档生成链，构建期依赖，不进入请求路径"）；同时在 §3 加入 `internal/api/docs/`、`tools/openapi3/`，在 §4.0 注明 `/docs/*` 不套统一响应。
- **Rationale**: 宪法"技术与接口约束"第 1 条要求"确需新增依赖必须先更新 §2 并写明理由"。把版本回写作为同一提交的一部分，避免规范与 `go.mod` 脱节。许可证（`swag`/`gin-swagger`/`swaggo-files` 为 MIT，`kin-openapi` 为 MIT）需逐项核实后登记，避免把 AGPL 类依赖带进企业内网产品（critique E11）。维护状态同样留档：`swaggo/swag` 的 OpenAPI 3 提案自 2019 年挂起至今，属"稳定但停滞"，这决定了我们不把 OAS3 期待寄托在它身上，而是自建转换步。
- **工具形态（critique E9）**：`swag` CLI 通过 `tools.go`（`//go:build tools` + blank import）纳入 `go.mod` 的 require，`make docs` 使用 `go run github.com/swaggo/swag/cmd/swag`（**不带 `@version`**）。带 `@version` 的 `go run` 在冷缓存机器上必须访问模块代理，与 FR-014"离线可生成"冲突；纳入 `go.mod` 后，`go mod download` 一次即永久可用，且版本被 `go.sum` 锁定。
- **Alternatives considered**: 先合代码再补文档 → 宪法明令"先改规范再改代码"的时序，不合规。把 swag 装成全局二进制并写进前置条件 → 每个开发者与 CI 都要手工安装并各自管版本，与"版本锁定"冲突。

## D10. 匿名响应具名化（实施前置，源码核验后新增）

- **Decision**: 先把 8 处 `gin.H` 响应改为 `dto` 包内的具名结构，再写注解；键集不同的分支使用不同类型，不合并。
- **Rationale**:
  1. `gin.H = map[string]any`，swag 静态分析只能看到"任意对象"，生成的 `data` 会是空 schema，等于文档里"什么都说了但什么字段都没有"，直接违反 FR-004/FR-005；
  2. 已知的键集差异：`login.go:79`（`mfaRequired/mfaTicket/expiresIn`）与 `login.go:112`（多 `redirectTo/user`、`mfaTicket` 恒 `null`）不可合并；`clients.go:75`（`clientId/clientSecret`）与 `clients.go:105`（多 `expiresAt:null`）同样不可合并。合并会改变响应字节；
  3. 只改构造方式、不改 JSON tag，属"类型具名化"而非行为改造，与 spec Assumptions"不改变任何业务端点的字段"相容；验收手段是**语义等价比对**（反序列化为 map 后 `DeepEqual` + 键集合相同），因为 `gin.H` 是 map（键按字典序）而结构体按字段序，逐字节比对不可达成（critique E7）。
- **Alternatives considered**:
  - 保留 `gin.H` 并用 `@Success 200 {object} map[string]any`：文档退化为无字段信息 → 拒绝。
  - 在注解里手写内联 schema（swag 支持 `{object} map[string]interface{}` 之外的匿名结构语法有限）：无法表达嵌套 `user` 与可空语义，且与真实 DTO 脱钩 → 拒绝。
  - 为一个分支建一个包装体、另一分支复用：见 Rationale 2，会改变字节 → 拒绝。

---

## 未决与风险登记

| 项 | 状态 | 处理 |
|---|---|---|
| `openapi2conv` 对 `x-` 扩展与 `example` 的保真度 | 需实施时验证 | 若字段级 `example` 在转换中丢失，改用 `@Success ... Example` 行内示例（swag 行内 example 语法转换后仍保留在 response 对象上）；验证点写入 T010 的验收项 |
| `response.Body.Data` 为 `any` 类型 | 已知限制 | 注解使用 swag 的组合语法 `{object} response.Body{data=dto.User}` 逐接口指定内层类型；若某接口的 `data` 为数组，使用 `{array} response.Body{data=[]dto.App}` 形式 |
| 文档页 Try it out 的同源限制 | 已记录（`requirements.md` Notes） | 本次不引入 CORS；文档页与业务接口同源，`make docs-serve` 的说明中明示 |
| **swagger-ui 是否携带 `sid` cookie** | **未验证，且决定 US1 核心价值**（critique X1） | 列为 `/speckit.tasks` 的第一个任务：30 分钟 spike，非 prod 启动后用页面登录并调用 `/api/v1/me`，抓包确认请求是否带 `Cookie`。若不携带，需通过 swagger-ui 的 `requestInterceptor`/`withCredentials` 补齐，属计划外工作量；结论必须回写本表 |
| 离线可生成 | 已解决（critique E9） | swag CLI 经 `tools.go` 纳入 `go.mod`，`make docs` 不带 `@version`；`quickstart.md` 场景 1 增加"断网复跑"验证 |
| 非 prod 实例的网络暴露面 | 已确认（critique E4） | 负责人确认 dev/test 无公网可达部署，故零鉴权访问可接受；该前提写入 `spec.md` Clarifications 与 Assumptions，条件变化时须先收紧策略 |

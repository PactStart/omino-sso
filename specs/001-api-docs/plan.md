# Implementation Plan: API 接口文档与自助调试门户

**Branch**: `001-api-docs` | **Date**: 2026-10-20 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `specs/001-api-docs/spec.md`

## Summary

为管理 API（`:8081`）的 16 个端点补齐注释驱动的接口文档：注解取自 handler 源码，生成 OpenAPI 3.0.3 描述文件并纳入版本控制，通过内嵌资源的文档页在非 prod 环境以 HTTP 直接访问并可 Try it out；prod 环境不注册文档路由。配套三条 `make` 目标：生成、漂移校验、本地预览。

技术路径的关键约束：Go 生态事实标准 `swaggo/swag` **只产出 OpenAPI 2.0**（其 OpenAPI 3 支持仍停留在 proposal 阶段），而 FR-021 要求 3.0.x，因此在生成链中插入一步 2.0 → 3.0 转换（`kin-openapi/openapi2conv`），由仓库内一个只做格式转换的小程序承载。这是本计划唯一的额外构件，已在 Complexity Tracking 中说明。

## Technical Context

**Language/Version**: Go `1.26.0`（`go.mod` 与 `tools/go.mod` 均为 `go 1.26.0`）。**2026-10-20 更新**：本特性实施期间主模块由 `1.24.1` 升级到 `1.26.0`（AGENTS.md §2.1 已同步），因此下文中一切"受 Go 1.24 限制"的判断均已失效

**Primary Dependencies**:
- 现有：`github.com/gin-gonic/gin v1.10.0`（文档路由挂载点）、`go.uber.org/fx v1.23.0`（装配）、`internal/api/response`（统一响应结构 `Body` / `Page`）
- 新增（已在 AGENTS.md §2 登记，见 Constitution Check）。最终引入 **3 个**依赖，按"构建期 / 运行期"物理隔离：
  - **构建期，在独立模块 `tools/go.mod`**（不进入请求路径，主模块依赖图零污染）：
    - `github.com/swaggo/swag` `v1.16.6`（CLI + 注解解析库）。`make docs` 在 `tools/` 内执行 `go run github.com/swaggo/swag/cmd/swag`（不带 `@version`），版本由 `tools/go.sum` 锁定、下载一次后离线可用（修正 critique E9）。**不得并入根 `go.mod`**：实测会强制升级 `golang.org/x/crypto` 到 `v0.36.0` 等，违反 §2.1 版本锁定
    - `github.com/getkin/kin-openapi` **`v0.135.0`**（`openapi2conv` 做 2.0→3.0 转换，`openapi3` 做漂移语义比对）。**版本维持 `v0.135.0` 不再因 Go 受限**（当初锁它的原因是 `v0.136.0+` 要求 `go >= 1.25`，Go 1.26 已解除该天花板）；升级需单独提交并回归 `make docs` / `make docs-check`，属独立议题
  - **运行期，在根 `go.mod`**：`github.com/swaggo/files` `v1.0.1`（内嵌 swagger-ui 静态资源，随二进制发布并在 `/docs/*` 被浏览器直接请求，因此属请求路径依赖，登记在 AGENTS.md §2.1）。它只要求 `x/net v0.7.0`，不触发任何版本升级
  - **弃用**：`github.com/swaggo/gin-swagger` `v1.6.1`。计划阶段曾拟用它挂载文档页；实施时改为 `internal/api/docs` 自写约 20 行 index.html（固定 `url: "./openapi.json"`、只引用同源内嵌资源），少一个依赖且不依赖其 URL 重写逻辑
  - 三个依赖均为 MIT；许可证与锁定理由见 `AGENTS.md` §2.1 / §2.4

**Storage**: 无。本特性不新增表、不写库、不做迁移（宪法原则 V 无触发点）；生成的 `openapi.json` 作为源文件随仓库提交。

**Testing**: `go test ./...`（现有基线）；新增测试覆盖：① 生成产物结构断言（16 个 operationId 齐全、`openapi: 3.0.3`）；② 漂移检测器正负样例；③ prod/非 prod 路由注册差异；④ `/docs/openapi.json` 的环境与版本注入；⑤ **8 处具名化的等价性单测**（同一组值分别用 `gin.H` 与 `dto.X` 构造，序列化后 `DeepEqual` + 键集合相同，不依赖 DB/Redis，修正 critique E8）；⑥ 文档 `paths` 与 `r.Routes()` 的集合一致性（INV-3/AC-1）；⑦ 文档业务码集合与 `errors.go` 的一致性（INV-11/AC-5）。

**Target Platform**: Linux 服务端（容器）；生成工具在开发者本机与 CI 均可离线运行。

**Project Type**: 单体 Web 服务（后端 Go + 管理 API），文档能力内聚在后端，不涉及前端工程。

**Performance Goals**: 单次生成（含注解扫描 + 转换）≤ 30s（FR-014/SC-005 的对应指标）；漂移校验 ≤ 1min（FR-012）；文档页首屏不引入外部网络请求（FR-020）。

**Constraints**:
- 文档路由必须挂在 `:8081`，不得触碰 `:8080`（宪法原则 II、FR-018）
- 环境判定只能读 `cfg.AppEnv` 一处（FR-017），与 `router.go:30` 现有判断同源
- 生成过程不得连接 MySQL / Redis（FR-014），即不得通过 `fx.New` 启动应用
- 示例值禁止出现真实令牌、`client_secret`、真实手机号（宪法原则 IV、FR-022）
- 不得引入 OpenAPI 3.0 之外的格式版本（FR-021）

**Scale/Scope**: 16 个端点、8 个 handler 文件、5 个可复用结构（`response.Body`、`response.Page`、`response.Error` 语义、`dto` 请求体、`dto` 响应体）、17 个已登记错误码。

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 原则 | 判定 | 依据 |
|---|---|---|
| I. 协议层单一来源 | **PASS** | 不触碰 `internal/oidc/*` 与 `:8080`；OIDC 端点按澄清结论排除在文档范围外（`spec.md` Clarifications 第 1 条） |
| II. 端口与信任边界物理分离 | **PASS** | 文档路由全部注册在 Gin 的 `:8081`；`:8080` 的 `net/http` mux 不新增任何 handler（FR-018） |
| III. 分层与数据一致性 | **PASS** | 新增 `internal/api/docs` 仅做"读内嵌字节 + 改写 info 块 + 返回"，不访问 `store` / `service`；无新增 SQL、无状态变更 |
| IV. 凭证不出服务端 | **PASS（有约束落地）** | 文档不含任何 token 明文；`client_secret` 仅在创建应用响应中出现，示例中用占位串（FR-022）；prod 不暴露文档页（FR-016）降低攻击面 |
| V. 结构变更只走手写 SQL 迁移 | **PASS（空操作）** | 无表结构变更，`migrations/` 无新增文件 |
| 技术与接口约束：依赖必须先在 §2 登记 | **PASS（已落地）** | 实际新增 **3 个**规范外依赖（`swag`、`kin-openapi` 在 `tools/go.mod`；`swaggo/files` 在根 `go.mod`），`gin-swagger` 弃用。`AGENTS.md` §2.1 / §2.4 已登记版本与理由，§3 已补 `internal/api/docs/` 与 `tools/`；规范同步与代码在同批提交内完成 |
| 技术与接口约束：`/api/v1/**` 统一响应 | **PASS** | 文档示例一律展示 `{code,message,data,traceId}` 四字段完整形态；`/docs/*` 属非业务路由，返回 HTML/JSON 原始内容，不套统一响应（与 `/oauth2/*` 豁免同理，需在 T001 中于 AGENTS.md §4.0 显式注明豁免） |
| 开发工作流门禁（`make build` / `test` / `build-web` / `lint`） | **PASS（需验证）** | 新增目标不得改变既有目标语义；`make build` 仍需产出 `bin/sso`。**`build` 依赖 `docs-check`**（≤1 min、纯本地、无网络），防止"源码改了但二进制内嵌旧文档"被发布；因 swag 已固定进 `go.mod`，构建期不需要预装 CLI 也不联网（修正 critique E1/E9） |
| 不生成 README / 演示脚本 / 种子数据 | **PASS** | FR-023 只要求**编辑既有** `README.md` 的"开发流程规范"段，不新建文件；不写种子数据 |

**Gate 结论**：无未说明的违规。需显式裁量的是"3 个新依赖 + 1 个新目录"（`gin-swagger` 实施期弃用，见上表），已通过 T001/T002 的规范同步落地；若评审要求更保守，可退回"仅生成 2.0 并改 FR-021"，但该路与本特性价值冲突（接入方代码生成器不认 2.0），故不采纳。详见 Complexity Tracking。

### Post-Design Re-check（Phase 1 后复审）

设计产物（[research.md](./research.md)、[data-model.md](./data-model.md)、[contracts/](./contracts/)、[quickstart.md](./quickstart.md)）落地后逐条复核，结论**不变，无新增违规**，三点补充：

1. **原则 III 加固**：`internal/api/docs` 的依赖面最终确定为 `embed` + `encoding/json` + `gin` + `github.com/swaggo/files`（用来提供内嵌 swagger-ui 静态资源；`contracts/doc-endpoints.md` §4 只要求改写 `info` 并返回字节），**确认不 import `store` / `service`**；`tools/openapi3` 为独立 `main` 包，不被任何业务包 import，不进入依赖图。
2. **原则 II 边界确认**：`/docs/*` 挂在 `:8081` 的 Gin 上且位于 `v1` 组外，`:8080` 的 `net/http` mux（`internal/oidc/server.go`）零改动；`contracts/doc-endpoints.md` §3 明确文档路由不套统一响应，与 `:8080` 的 RFC 原生报文豁免属同类处理。
3. **原则 I 无回声**：`data-model.md` §7 的生命周期图确认 `openapi.json` 不含 `:8080` 的 7 个协议端点，不存在"文档反向固化协议字段"的风险。

一处遗留裁量升级为明确任务：`AGENTS.md` 需在 §4.0 增补一行"`/docs/*` 不套统一响应"的豁免说明，否则契约与规范字面冲突——已并入 T001，不再列为违规。

## Project Structure

### Documentation (this feature)

```text
specs/001-api-docs/
├── plan.md              # 本文件
├── research.md          # Phase 0：7 项决策与备选方案
├── data-model.md        # Phase 1：文档模型与不变量（无 DB 实体）
├── quickstart.md        # Phase 1：端到端验证脚本
├── contracts/
│   ├── doc-endpoints.md         # 文档路由契约（dev/prod 行为矩阵）
│   └── annotation-conventions.md # 注解约定：16 端点必填注解、信封写法、错误码映射
├── checklists/
│   └── requirements.md  # /speckit.specify 产出
└── tasks.md             # Phase 2 输出（/speckit.tasks 生成，本命令不创建）
```

### Source Code (repository root)

仅列本特性触及与新增的路径；未列出的保持不动。

```text
cmd/
└── sso/
    └── main.go                      # [改] 追加 @title/@version/@BasePath 等全局注解（仅注释）

internal/
└── api/
    ├── router.go                    # [改] 非 prod 分支注册 /docs/*；prod 不注册
    ├── docs/
    │   ├── docs.go                  # [新] //go:embed openapi.json + 运行时 info 注入 + gin handler
    │   ├── openapi.json             # [新·生成物] OpenAPI 3.0.3 描述文件，入库
    │   └── docs_test.go             # [新] 结构断言 + 注入断言
    ├── dto/
    │   └── dto.go                   # [改] 新增 8 个具名响应类型（键名与 null 语义不变；键序变化无影响，见下）
    ├── auth/
    │   ├── login.go                 # [改] gin.H ×2 → dto.LoginMfaChallenge / dto.LoginResult；Login/Logout 注解
    │   └── mfa.go                   # [改] gin.H → dto.MfaResult；VerifyMFA 注解
    ├── me/
    │   └── profile.go               # [改] gin.H ×2 → dto.AppItem / dto.SessionItem；5 个方法注解
    └── admin/
        ├── users.go                 # [改] ListUsers / CreateUser / UpdateUser / OrgTree 注解
        ├── clients.go               # [改] gin.H ×2 → dto.CreatedClient / dto.RotatedSecret；3 个方法注解
        └── audit.go                 # [改] gin.H → dto.AuditItem；ListAuditLogs 注解

tools/
├── go.mod                           # [新] 独立 module：构建期依赖与运行时依赖物理隔离
└── openapi3/
    ├── main.go                      # [新] 2.0→3.0 转换 + -check 漂移比对（不参与运行时）
    └── main_test.go                 # [新] 转换与漂移检测单测

Makefile                             # [改] 新增 docs / docs-check / docs-serve 三个目标
.gitignore                           # [改] 忽略 .tmp/
AGENTS.md                            # [改] §2 新增依赖与版本；§3 新增 internal/api/docs、tools；§4.0 文档路由豁免说明
README.md                            # [改] 开发流程段落补充三条命令与 CI 挂载命令（FR-023）
```

**Structure Decision**: 沿用现有单一 Go 模块结构，不新增 module、不引入前端构建。文档能力拆两处：
- 运行时侧 `internal/api/docs`（与 `internal/api/response` 同级，属 API 层，符合"api → service → store"单向依赖，且它不依赖后两层）；
- 构建期侧 `tools/openapi3`（独立 `main` 包，可被 `go run ./tools/openapi3` 调用，不被业务包 import）。

生成的 `openapi.json` 放在 `internal/api/docs/` 而非仓库根的 `docs/`，原因是 `//go:embed` 无法引用包目录之外的文件；这样既满足 FR-020 的内嵌要求，又避免为"放文件"单独开顶层目录。

**一个必须先做的隐性前置（源码核验后新增）**：现有 16 个端点中有 8 处响应是 `gin.H` 匿名映射（`auth/login.go:79`、`:112`、`auth/mfa.go:63`、`admin/clients.go:75`、`:105`、`me/profile.go:58`、`:130`、`admin/audit.go:54`）。`gin.H` 等价于 `map[string]any`，swag 无法据此推导任何字段，直接写注解只会得到空的 `data`。因此实施顺序上，**具名化必须先于注解**：在 `dto` 包新增 8 个结构体，字段与现有 JSON 键逐字对应，并逐分支保留键集差异（`#1` 与 `#1′`、`#14` 与 `#15` 的键集互不相同，不可复用同一类型）。

改造前后的等价性判据是**语义等价，不是逐字节相等**（修正 critique E7）：`encoding/json` 序列化 `map[string]any` 时按键**字典序**输出，序列化结构体时按**字段声明序**输出，两者键序天然不同。正确判据为：`json.Unmarshal` 到 `map[string]any` 后 `reflect.DeepEqual`，并额外断言**键集合完全相同**（含 `mfaTicket`、`expiresAt` 这类值为 `null` 的键）。明细见 `contracts/annotation-conventions.md` §4。

## Delivery Discipline & Accepted Risks

Critique 复审后固化的四条执行纪律与一项已接受风险（来源：`critiques/critique-20261020-132851.md`）：

| 项 | 结论 |
|---|---|
| **提交粒度**（E10） | `internal/api/docs/openapi.json` 的变更必须与其对应的 handler 注解变更**在同一提交内**，禁止分开提交；回滚 = 单次 `git revert`，不得只回代码不回产物 |
| **限流与可用性**（E2/E3） | `router.go:38` 的 `RateLimit(api)` 目前对 `/docs/*` 同样生效，swagger-ui 首屏 10+ 请求可能触发 429；实施时在限流中间件按 `/docs/` 前缀跳过，使文档页不依赖 Redis 可用性（运维故障时正是最需要查文档的时刻） |
| **二进制体积**（E6） | `swaggo/files` 内嵌的 swagger-ui 资源**实测 +8.57 MB**（30,268,880 → 39,255,088 B），超出预算的 3–5 MB；构成与优化空间见 `data-model.md` §8（该版本把 4 份 bundle 与 4 份 `.map` 全部内嵌，仅三个 `.map` 就占约 17 MB 明文）。优化属独立议题 |
| **构建期强制**（E1） | `make build` 依赖 `docs-check`，杜绝"代码已改、内嵌文档陈旧"的产物被发布 |
| **已接受风险**（E5） | `openapi.json` 与 UI 资源会被 `//go:embed` 编入 **prod 二进制**（虽不注册路由、不可访问）。收益是构建简单、无需外部静态目录；残余风险为"拿到二进制即可提取接口面"，与"源码可见"同级，接受 |

## Complexity Tracking

| Violation | Why Needed | Simpler Alternative Rejected Because |
|---|---|---|
| 引入 3 个规范外依赖（构建期 `swag` / `kin-openapi`，运行期 `swaggo-files`） | 注解驱动生成 + 3.0 输出 + 离线 UI 三者缺一不可 | ① 直接用 `swag` 单一依赖：只产出 OpenAPI 2.0，违反 FR-021；② 引入 `swaggo/swag/v2`（alpha）：可达 3.x 但版本不稳定，与本项目"版本锁定"原则冲突，且 alpha 的注释语法与 v1 不兼容；③ 手写 `openapi.json`：违反 FR-010 且必然腐化。**注**：计划阶段的第 4 个依赖 `gin-swagger` 已被实施期否决（自写 index.html 更少依赖），故不计入 |
| 新增 `tools/openapi3` 一个额外可执行包 | 2.0→3.0 转换与漂移比对需要代码，且必须离线可跑（FR-014） | ① 用 `npx swagger2openapi`：给 Go 构建链引入 Node 依赖，且离线环境不可靠；② 放入 `internal/`：会被业务包误 import，且语义上不属于服务运行时；③ 塞进 `internal/api/docs`：会让测试与运行时依赖转换库 |
| 新增 `internal/api/docs/` 目录（AGENTS.md §3 未列） | `//go:embed` 要求被嵌入文件与源文件同包目录 | 输出到根 `docs/` 后用 `go:generate` 复制一份到包内：产生两份真源，漂移检测基准变得含糊 |
| 文档路由返回非统一响应（HTML / 原始 JSON） | 文档页必须由浏览器直接渲染，JSON 描述文件必须被 OpenAPI 工具消费 | 套 `{code,message,data,traceId}`：`swagger-ui` 与任何 OpenAPI 消费方都无法解析，且与"文档要能被标准工具链消费"直接矛盾 |

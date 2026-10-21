<!--
Sync Impact Report
=================
Version change: 1.0.0 → 1.1.0
Bump rationale: MINOR —— 在「开发工作流与质量门禁」新增测试与影响分析纪律，未删除或重定义既有原则。

Modified principles: 无
Added principles（「开发工作流与质量门禁」小节内）:
  - 改动必须伴随测试
  - 影响分析先行
  - 安全审视先行

Removed sections: 无

Follow-up / Deferred items:
  - 仍不纳入「测试先行（TDD）」「覆盖率门槛」等强约束：仅要求「改动带测试 + 主动自测」，
    不强制 TDD 顺序或覆盖率数字。
  - 本次同步更新 AGENTS.md §8.4 与附录「一致性检查清单」（先改 AGENTS.md，再改本文件，符合 Governance）。
-->

# omino-sso Constitution

本文件是项目**原则层**规范：不可协商的技术与交付原则。实现细节的唯一权威是仓库根目录的
`AGENTS.md`；两者冲突时以 `AGENTS.md` 为准，并按 Governance 的流程先修正本文件。

## Core Principles

### I. 协议层单一来源

- 必须以 `github.com/zitadel/oidc/v3` 作为 OIDC 协议的**唯一**实现来源；JWS / JWKS 统一使用
  `github.com/go-jose/go-jose/v4`（禁止 v3）。
- 禁止手写 OIDC 状态机、授权码签发流程、JWT 签发与验签；禁止引入第二个 OIDC 实现库。
- 对 `op.Storage` 的适配只允许做"数据存取 + 业务校验"，不得在其中实现协议状态流转。
- 理由：协议层是缺陷成本最高的部分，自研等价物必然偏离 RFC 且无法通过一致性套件。

### II. 端口与信任边界物理分离

- 必须保持两个独立 `http.Server`：`:8080` 只挂 `op.Provider`（`net/http`），`:8081` 只挂 Gin。
- `/oauth2/*` 上禁止挂 CORS、业务鉴权、统一响应包装、全局限流；两个端口各自独立的中间件栈与超时配置。
- 理由：协议端点是 RFC 兼容面，任何"统一处理"都会污染报文并破坏接入方的标准实现。

### III. 分层与数据一致性

- 调用方向必须为 `api → service → store`，禁止跨层调用；`oidc` 层只能通过 `service` / `store`
  访问数据，不得直接写 SQL。
- 关键路径（授权码消费、refresh token 轮换、令牌吊销）必须使用单条原子 `UPDATE` 并以
  `RowsAffected` 判定结果；禁止"先查后改"。
- `/oauth2/token` 链路的事务必须通过 `internal/store/tx.go` 的 tx-in-context 传递；所有
  Storage 方法必须传入 `ctx`（`db.WithContext(ctx)`）。
- token / code / session 类表禁止软删除。
- 理由：认证链路天然面对并发重放，"读-改-写"会直接导致授权码重复消费与令牌重放。

### IV. 凭证不出服务端

- 浏览器只允许持有 `HttpOnly + Secure + SameSite=Lax` 的 `sid` cookie。
- 前端禁止用 `localStorage` / `sessionStorage` / 内存变量持有 access_token、refresh_token
  或任何 `client_secret`。
- 响应体与日志中禁止出现 token 明文、密码、`client_secret`；新建 client 的 secret 仅单次返回，
  且必须在日志层屏蔽。
- 理由：一旦 XSS 成立，脚本可读的凭证即可被直接带走；凭证只存在服务端才能把爆炸半径限制在会话内。

### V. 结构变更只走手写 SQL 迁移

- 表结构变更必须在 `migrations/` 下按 `NNNN_name.up.sql` / `NNNN_name.down.sql` 成对提交；
  禁止 `AutoMigrate`，禁止在代码中 `CREATE TABLE`。
- 时间列必须为 `BIGINT` 存储 Unix 毫秒（列名以 `_at` 结尾），禁用 `TIMESTAMP` 与 `DATETIME`。
- 大小写敏感列必须显式声明 `COLLATE utf8mb4_0900_as_cs` 或使用 `BINARY(32)` 哈希。
- 理由：结构变更必须可 review、可回滚，并与代码版本一一对应。

## 技术与接口约束

- 技术栈与版本以 `AGENTS.md` §2 锁定表为准；禁止擅自升级版本或引入规范外依赖，确需新增时
  必须先更新 §2 并写明理由。
- `/api/v1/**` 必须使用统一响应 `{code, message, data, traceId}`：成功 `code="OK"`、
  `message="success"`、`data` 不得省略；失败 `data` 恒为 `null`，`message` 不得泄露内部细节、
  堆栈或 SQL；`traceId` 必须同时写入响应头 `X-Trace-Id` 与日志。
- `/oauth2/*` 与 `/.well-known/*` 必须保持 RFC 原生报文，禁止包装、禁止改写字段名。
- 错误码必须先登记到 `AGENTS.md` §5.3 再使用；禁止在 handler 内直接拼字符串作为 `code`。
- `AGENTS.md` §7.2 的 5 条非目标与"附加禁止项"一律不实现：不预留接口、不写 TODO、不建空表、
  不加配置项。
- 环境变量必须由 `internal/config` 统一加载并在启动时校验；必填项缺失必须非零退出，
  不得静默使用默认值上线；`.env.example` 必须与 `AGENTS.md` §9 清单保持同步。
- 数据模型细节（字段、索引、`utf8mb4` 字符集）以 `AGENTS.md` §6 为准，本文件不复述。

## 开发工作流与质量门禁

- 每次交付前必须执行并通过：`make build`、`make test`、`make build-web`；`make lint` 必须执行；
  涉及协议的改动必须通过 `AGENTS.md` §8.5 的 OpenID 一致性套件（全部用例通过方可发布）。
- 提交前必须逐条自查 `AGENTS.md` 附录「一致性检查清单」的各项，任一项不满足不得提交。
- 改动必须伴随测试：新增或修改业务逻辑、端点、数据访问层时，必须同步新增或更新单元测试；
  提交前主动执行 `make test` 与 `make lint`，不得依赖 pre-commit 兜底。
- 影响分析先行：改动路由、DTO 字段或常量前，必须全局搜索引用（含测试中的硬编码断言）并同步更新；
  新增/删除路由后必须重跑 `make docs` 并核对 operation 数断言。
- 安全审视先行：新增涉及"按目标对象写回"或鉴权的端点前，必须先界定谁能调用、可写谁的字段，
  权限校验先于业务实现。
- 协作纪律：不重构既有目录结构与分层；不"顺手优化"与本任务无关的代码、不批量格式化；
  不生成 `README`、演示脚本或种子数据文件，除非任务明确要求。
- 规范未覆盖的情况必须选择"改动最小、最保守"的实现，并在回复中显式说明该决策点。

## Governance

- `AGENTS.md` 是项目的唯一权威规范，本文件是其原则层摘要；两者冲突时以 `AGENTS.md` 为准。
- 修订程序：先修改 `AGENTS.md` 并写明理由 → 再同步本文件 → 递增版本号 → 更新 `Last Amended`。
  版本语义：MAJOR 为删除或重定义原则，MINOR 为新增原则或实质扩展，PATCH 为措辞澄清与非语义修订。
- 合规审查：每次代码评审必须依据本文件与 `AGENTS.md` 附录清单核对；不合规不得合并。
  任何偏离原则的方案必须在评审中显式说明理由与替代方案，未说明即视为缺陷。
- 非目标不得以"顺手实现"的方式进入代码库；确需变更范围时，先改 `AGENTS.md` 再改代码。

**Version**: 1.1.0 | **Ratified**: 2026-10-20 | **Last Amended**: 2026-10-21

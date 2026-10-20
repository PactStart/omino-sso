# 企业内部单点登录系统（SSO / IdP）技术选型报告

| 项 | 内容 |
|---|---|
| 项目 | omino-sso — 企业内部统一身份与单点登录系统 |
| 文档版本 | v1.0（定稿） |
| 日期 | 2026-10-20 |
| 决策状态 | 已决策，可进入实施 |
| 决策结论 | **自研 OIDC Provider：Go + Gin + fx + GORM + MySQL + Redis，协议层基于 `zitadel/oidc/v3`；前端 Vue 3 + Tailwind（登录页）+ Element Plus（后台）** |

---

## 1. 结论摘要

1. **不引入 Keycloak / Casdoor 作为底座，自研 IdP**。原因是自研能获得技术栈统一（全 Go）、登录体验完全可控、组织模型贴合公司实际，且团队技术储备匹配。代价是比"装 Keycloak"多约 3–5 周。
2. **协议层不自研**。使用 `zitadel/oidc/v3`（OpenID Foundation 认证实现）承载 OIDC/OAuth2/PKCE 的状态机，自研部分只写存储适配、业务策略和 UI。这是本项目最重要的技术边界。
3. **前端拆成两个产物**：`portal`（登录页 + 账号中心，Tailwind 手写，品牌可控）与 `admin`（管理后台，Element Plus，开发效率优先）。
4. **三个必须遵守的工程约束**：IdP 与管理 API 使用两个独立 `http.Server`；Storage 层使用 transaction-in-context 保证令牌兑换的原子性；`client_id` / 授权码 / refresh token 一律按大小写敏感处理并只存 SHA-256。

---

## 2. 需求边界（假设）

若下表中有超过一半不成立，请重新评估"自研"这一决策。

| 类别 | 需求 |
|---|---|
| 协议 | OIDC / OAuth 2.1 为主；SAML 2.0 仅少量老系统（可选）；CAS 可选 |
| 账号来源 | 本地账号 + LDAP / AD 同步 + 企业微信 / 钉钉 / 飞书扫码 |
| 认证强度 | 密码（argon2id）+ MFA（TOTP）；会话管理与单点登出 |
| 管理能力 | 用户 / 组织架构 / 角色、应用注册（client）、审计日志 |
| 接入方 | Spring Boot、Go 服务、Vue SPA、无改造能力的老系统（反向代理鉴权） |
| 规模 | 用户数百至数千，峰值登录 QPS < 50，MVP 单副本即可 |
| 交付定位 | 内部系统，非多租户 SaaS，无需对外售卖 |

---

## 3. 候选方案

| | A. Keycloak 底座 | B. Casdoor 底座 | C. 自研 Java | D. 自研 Go（**选定**） |
|---|---|---|---|---|
| 前端 | Vue 3 门户 + Keycloak 主题 | Vue 3 门户 + Casdoor 自带 React | Vue 3 全自研 | Vue 3 全自研 |
| 后端 | Keycloak 26.x（Java/Quarkus）+ SPI + BFF | Casdoor 4.x（Go）+ Go BFF | Spring Boot 3 + Spring Authorization Server | Go + Gin + fx + `zitadel/oidc/v3` |
| 数据库 | PostgreSQL | MySQL / PostgreSQL | PostgreSQL | MySQL 8 |
| 缓存 | Infinispan（内置） | 可选 Redis | Redis | Redis |
| 部署 | Docker Compose → K8s + Operator / 托管 RHBK | Compose → K8s（无状态） | 标准 Java 服务 | 单二进制 + Compose → K8s |

补充候选（不进入详细评分）：**Logto**（TypeScript/Node，产品体验最佳，但与团队技术栈零重叠）；**authentik**（Python/Go，社区活跃，深度定制进入 Django 世界）；**Zitadel**（Go，功能强，但 AGPL-3.0，若需对外/给子公司独立部署需评估）。

---

## 4. 评分与对比

权重依据项目特征设定：MVP 阶段的开发效率权重最高，其次是长期可维护性（学习成本 + 社区），AI 辅助开发作为效率乘数。

| 维度 | 权重 | A Keycloak | B Casdoor | C 自研 Java | D 自研 Go |
|---|---|---|---|---|---|
| 学习成本（上手 → 能改） | 25% | 6.5 | 8.5 | 7.0 | 8.0 |
| AI 编程支持 | 15% | 8.5 | 6.5 | 9.0 | 8.5 |
| MVP 开发效率 | 35% | 8.0 | 8.5 | 3.0 | 7.0 |
| 社区活跃度与文档 | 25% | 10.0 | 6.5 | 7.5 | 6.0 |
| **加权总分** | | **8.20** | **7.70** | **6.03** | **7.23** |

说明：D 的"社区"分低是因为自研项目本身没有社区，但**协议层依赖的 `zitadel/oidc/v3` 有活跃上游**，实际风险低于分数所示；D 的"开发效率"已计入使用成熟库的加成（若手写协议层，该项降至 3.5，总分 6.0，即与 C 同级——这正说明了"用库"的决定性）。

**关于"D 分数低于 A / B 但仍被选定"**：本评分模型只覆盖四个可量化维度，没有纳入两项对本项目权重极高的定性因素——**与团队主力语言一致**（决定长期维护成本与故障响应速度）和**登录体验 / 组织模型的完全掌控**（决定产品可用性）。当这两项的权重大于 3–5 周的额外工期时，D 的实际排序会反超。若项目约束变为"3 个月内交付、20 个系统接入、必须一次做对"，则应立即改选 A。详见 §4.2 与 §12。

### 4.1 分维度要点

**学习成本**
- Keycloak 的壁垒不是 Java，而是概念模型：realm / client / authentication flow / protocol mapper / user federation / SPI。跑起来 1 小时，改登录页 1 天，写自定义 authenticator 3–5 天。
- Casdoor 概念少（organization / application / provider / user），Go 代码直白，但"深度定制 = 维护 fork"。
- 自研 Go 的风险点集中在 OAuth/OIDC 的协议细节，而非语言本身；用 `zitadel/oidc/v3` 可把这块风险降到可控。

**AI 编程工具支持**
- Vue 3 + Element Plus、Spring Boot 3、Go + Gin + GORM：语料充足，生成准确率高。
- Keycloak：语料绝对量最大，但**版本断层严重**（Wildfly→Quarkus、Account Console 由 FreeMarker 改为 React、Admin REST 端点变更），AI 常输出过时写法。缓解：锁定版本并把官方 docs 纳入仓库。
- Casdoor / Arco / TDesign：语料少，AI 易编造 API。缓解：让 AI 直接索引源码。
- 自研 + `zitadel/oidc/v3`：Go 语料足，但该库的 `op.Storage` 接入方式在语料中较少，需以仓库 `example/server` 为准。
- 落地建议：仓库内放 `AGENTS.md`，写明"框架版本 + 禁止手写协议状态机 + 安全红线清单"，显著降低 AI 跑偏率。

**MVP 开发效率**
- "一天跑通第一个 OIDC 登录"：A / B 都能做到（`docker compose up`）。
- "两周让 5 个内部系统接入 + 企微扫码"：B 最快（内置国内 IM provider + CAS 协议），A 需写 SPI。
- 自研：端到端 2–3 周，完整 MVP 6–9 周（详见 §10）。

**社区与文档**
- Keycloak：CNCF 孵化、25k+ stars、商业托管生态齐全，踩坑必能搜到答案；官方文档偏配置手册，缺最佳实践。
- Casdoor：中文文档友好、响应快，但生产踩坑沉淀少。
- 自研：无社区兜底，靠上游库 + Conformance Suite 验收（见 §11）。

### 4.2 为什么最终没有选 Keycloak

Keycloak 是评分最高的选项，也是最"稳"的选项。放弃它的理由不是能力不足，而是三条组织性因素：

1. 团队主力语言是 Go，Keycloak 的扩展面（SPI、主题）需要长期投入 Java 维护成本；
2. 登录页与账号中心的体验要求高，Keycloak 的主题定制（FreeMarker）投入产出比低；
3. 组织模型需要贴合公司实际的部门树 / 岗位 / 子公司结构，外挂映射比自建模型更复杂。

**对冲措施**：数据模型按标准设计（§8），确保将来若需迁移到 Keycloak/Casdoor，用户与 client 数据可导出；若出现 §12 中的止损信号，立即切换。

---

## 5. 最终技术栈

| 层 | 选型 | 版本基线 | 说明 |
|---|---|---|---|
| 语言 | Go | 1.24+ | |
| HTTP 框架 | Gin | v1.10+ | 仅用于管理 API（`/api/**`） |
| DI | uber **fx** | v1.23+ | 使用 fx 而非 dig：需要 lifecycle 管理 HTTP server、连接池、清理任务 |
| ORM | GORM | v1.25+ | 管理类 CRUD 使用；授权码兑换、令牌轮换、吊销三处使用显式 SQL |
| 数据库 | MySQL | 8.0 / 8.4 LTS | 字符集 `utf8mb4`；敏感列使用 `utf8mb4_0900_as_cs` |
| 缓存 | Redis | 7.x | 单实例 + AOF，MVP 不上 Cluster |
| 协议库 | `github.com/zitadel/oidc/v3` | v3.28.x | OP + RP，OpenID Foundation 认证实现 |
| JOSE | `github.com/go-jose/go-jose/v4` | v4.x | 与协议库一致，禁止混用 v3 |
| 密码哈希 | `golang.org/x/crypto/argon2` | — | argon2id，参数见 §9 |
| TOTP | `github.com/pquerna/otp` | — | MFA |
| ID 生成 | ULID | — | 避免自增枚举，便于分库 |
| 迁移 | 手写 SQL | — | **禁用 `AutoMigrate`**（索引 / 字符集 / BINARY(32) 不可控） |
| 可观测 | `slog` + Prometheus metrics + OpenTelemetry | — | 协议端点必须有延迟与错误率指标 |
| 构建/部署 | Docker 多阶段构建 + docker compose | — | MVP 单机；后续 K8s |
| 前端 | Vue 3 + Vite + TypeScript | Vue 3.5+ | `portal` 与 `admin` 两个产物 |
| 登录页样式 | Tailwind CSS | 4.x | 关闭 `preflight` 或独立产物 |
| 后台组件库 | Element Plus | 2.x | 表格 / 树 / 表单 |
| 状态与路由 | Pinia + vue-router 4 | — | |
| 图表 | vue-echarts | — | 审计统计 |

---

## 6. 系统架构

### 6.1 拓扑

```
                    ┌──────────────────────────────┐
   浏览器 ──────────▶│  portal (Vue, Tailwind)      │  登录页 / 账号中心
                    └──────────────┬───────────────┘
                                   │ HttpOnly Cookie (sid)
                    ┌──────────────▼───────────────┐
                    │  admin/portal BFF (Gin :8081)│  管理 API / 聚合 / 裁剪
                    └───┬──────────────────────┬───┘
                        │                      │
       ┌────────────────▼──────┐      ┌────────▼─────────┐
       │ IdP (net/http :8080)  │      │ MySQL 8          │
       │ op.Provider (oidc)   │─────▶│ Redis 7          │
       └───────────┬───────────┘      └──────────────────┘
                   │
       ┌───────────▼───────────────────────────────────┐
       │ 接入方：Spring Boot / Go 服务（本地验签 JWT）   │
       │ 老系统：oauth2-proxy 反向代理鉴权               │
       └───────────────────────────────────────────────┘
```

### 6.2 两个独立 `http.Server`（强约束）

IdP 与业务 API **不共用 Gin 路由树**：

| 混挂在同一 Gin 的问题 | 分离后的结果 |
|---|---|
| CORS 中间件作用到 `/token`，语义错误或被拦 | 协议端口不挂 CORS |
| `/authorize` 被全局限流 / 鉴权中间件误伤 | 中间件栈各自独立 |
| provider 端点位于 issuer 根路径，加前缀需改 issuer，易配错 | provider 独占端口，issuer 即 `https://sso.example.com` |

`op.Provider` 本身实现 `http.Handler`，直接挂载即可，无需 Gin 适配。

### 6.3 包结构

```
cmd/sso/main.go          // fx.New(fx.Supply(cfg), oidc.Module, api.Module, store.Module)
internal/oidc/           // provider 构造、op.Storage 适配、登录闭环 handler
internal/store/          // GORM 模型 + 关键路径手写 SQL + tx-in-context
internal/api/            // Gin 管理接口 / BFF
internal/pkg/crypto      // argon2id、随机串、SHA-256
internal/pkg/audit       // 事件模型与异步写入
migrations/              // 手写 SQL
```

### 6.4 关键工程约束

**(1) fx 与 `op.NewProvider`**
`op.NewProvider(config, storage, ...op.Option)` 含变长参数，fx 无法直接注入，必须显式构造函数；接口绑定使用 `fx.Annotate(newMySQLStorage, fx.As(new(op.Storage)))`；构造函数中完成 key/config 校验，使错误在启动期暴露。

**(2) transaction-in-context**

`zitadel/oidc` 把一次 `/token` 兑换拆成多次 Storage 调用（查 client → 查 code → 标记已用 → 存 token → 更新 refresh），默认不在同一事务内。若"存 token 成功、标记 code 已用失败"，同一授权码可被兑换两次。

方案：token 端点外层开启事务，将 `*gorm.DB` 放入 `context`；Storage 方法统一经由 `tx(ctx)` 取值：

```go
func (s *store) tx(ctx context.Context) *gorm.DB {
    if tx, ok := ctx.Value(txCtxKey{}).(*gorm.DB); ok {
        return tx.WithContext(ctx)
    }
    return s.db.WithContext(ctx)
}
```

配套要求：
- 每个 Storage 方法必须 `WithContext(ctx)`，否则丢失超时与取消传播；
- `Health(ctx)` 不得开事务或加锁（会被探针高频调用）；
- 授权码消费使用单条原子 UPDATE，以 `RowsAffected == 1` 判定成功，禁止"先查后改"。

**(3) AuthRequestStorage 必须共享**
协议库默认使用内存实现保存"待认证请求"。单副本无碍；一旦多副本，用户在 A 节点登录、回调打到 B 节点即失败。决策：**MVP 先承诺单副本，但同时实现 Redis 版接口（key = auth request id，TTL 10 分钟，成本约半天）**，避免后续扩容时返工。

**(4) 单 issuer + 租户 claim**
多子公司不采用"一租户一 Provider 实例"，统一单 issuer，通过 `tenant` claim 区分。多实例会导致多套 discovery / JWKS / 端点，客户端配置成本翻倍。

---

## 7. 前端方案

### 7.1 拆分原则

```
apps/portal/   → 登录页 + 账号中心（公开、品牌定制、Tailwind、独立构建）
apps/admin/    → 用户/组织/应用/审计（内网、Element Plus）
```

收益：登录页不加载组件库（首屏更快，SSO 是所有系统的入口页）；避免 Tailwind preflight 与组件库基础样式冲突；两套 UI 的升级互不影响。

### 7.2 页面与组件

| 页面 | 实现 |
|---|---|
| 登录页（账号密码 / 验证码 / MFA 二步 / 企微钉钉飞书） | 手写 + Tailwind，**不使用 `el-form`** |
| 账号中心（改密、MFA 绑定二维码） | `el-form` + `el-upload` |
| 我的应用 | 自绘卡片 + `el-empty` |
| 登录设备 / 会话撤销 | `el-table` + `el-tag` |
| 授权记录 | `el-timeline` |
| 用户管理（左树右表） | `el-tree` + `el-table` |
| 应用管理（redirect_uri 列表、scope、密钥轮换） | `el-form` + 自定义 URI 列表编辑器 |
| 审计日志（万级 + 时间筛选） | `el-table-v2` 虚拟滚动 |
| 系统配置 | `el-form` + `el-tabs` |

### 7.3 约束

- 浏览器**不持有任何 token**，仅 `HttpOnly + Secure + SameSite=Lax` 的 `sid` cookie；请求带 `credentials: 'include'`。
- 禁止在浏览器内运行 `oidc-client-js`，否则绕开 BFF，失去凭证保护意义。
- 主题定制优先覆盖 CSS 变量（如 `--el-color-primary`），不改源码 theme。
- 图标按需引入（`unplugin-icons`），避免登录页体积膨胀。

---

## 8. 数据模型

### 8.1 核心表

| 表 | 主键 | 要点 |
|---|---|---|
| `users` | `id` CHAR(26) ULID | `username` 唯一且**大小写敏感**；`password_hash` argon2id；`status`、`tenant_id`、`dept_id` |
| `clients` | `client_id` VARCHAR(64) | `client_secret_hash` BINARY(32)；`redirect_uris` JSON；`grant_types`、`scopes`、`is_public` |
| `auth_codes` | `code_hash` BINARY(32) | `redirect_uri`、`code_challenge`、`code_challenge_method`、`nonce`、`auth_time`、`expires_at`、`used` |
| `refresh_tokens` | `id` CHAR(26) | `token_hash` BINARY(32) 唯一；`family_id`（重放检测时按族吊销）；`revoked`、`rotated_from` |
| `sessions` | `sid` VARCHAR(64) | 存 Redis 为主，MySQL 留审计副本 |
| `audit_events` | `id` | `event_type`、`user_id`、`client_id`、`ip`、`ua`、`result`、`detail` JSON |
| `organizations` | `id` | 部门/组织树，`parent_id`、`path` |
| `consents` | `(user_id, client_id)` | 授权同意记录 |

### 8.2 两个必坑

**排序规则**：MySQL 默认 `utf8mb4_0900_ai_ci` 大小写不敏感。`client_id` 按规范需精确匹配；更严重的是授权码 / refresh token 这类随机串若按 ci 比对，大小写不同的字符串可能校验通过。对策：相关列使用 `utf8mb4_0900_as_cs`，或**只存 SHA-256 的 `BINARY(32)`**（推荐，同时降低脱库影响）。

**时间精度**：`DATETIME` 默认无小数位，60 秒 TTL 的授权码会出现 ±1 秒抖动。统一使用 `BIGINT` 存 Unix 秒 / 毫秒，或 `DATETIME(6)`；禁用 `TIMESTAMP`（2038 + 时区）。

### 8.3 GORM 使用边界

| 风险 | 后果 | 对策 |
|---|---|---|
| `Updates(struct)` 忽略零值 | `revoked=false` / `revoked_at=nil` 更新不生效，吊销失效 | 使用 `map[string]any` 或 `Select()` 指定字段 |
| 软删除 `gorm.DeletedAt` | token 表唯一索引 + 软删 = 插入冲突 | token / code / session 表**禁用**软删 |
| `FirstOrCreate` 并发不原子 | 授权码重复插入 | `INSERT ... ON DUPLICATE KEY` 或唯一索引 + 捕获重复键 |
| 先查后改 | 授权码被并发兑换两次 | 原子 UPDATE + `RowsAffected` 判定 |

---

## 9. 安全红线（不可妥协）

1. `redirect_uri` **精确字符串匹配**，不做前缀或通配；`post_logout_redirect_uri` 使用白名单。
2. PKCE **只接受 `S256`**，拒绝 `plain`；`code_challenge` 与授权码强绑定。
3. 授权码 TTL ≤ 60s，**单次消费**；检测到重用时吊销该 code 已签发的全部 token。
4. refresh token **轮换 + 重用检测**：旧 token 二次使用即吊销整个 `family_id`。
5. 密码使用 **argon2id**（memory ≥ 64MB、iterations ≥ 3），比较使用常量时间。
6. ID Token 必须包含 `iss / aud / exp / iat / sub / auth_time / nonce`，`nonce` 原样回传。
7. 签名密钥 **双 key + `kid` 轮换**；私钥不入仓库，JWKS 带 `Cache-Control`。
8. `client_secret` 支持 `client_secret_basic` 与 `client_secret_post`，比较使用常量时间。
9. 拒绝 `alg: none`；禁止 HS 与 RS 混用（防止算法混淆攻击）。
10. 登出必须真实失效服务端 session，`sid` 从 Redis 删除并通知下游。
11. token 端点严格只接受 `application/x-www-form-urlencoded`，错误响应遵循 RFC 6749 §5.2（`400` + `error` / `error_description`）。
12. 所有认证事件落审计（谁、何时、IP、UA、结果、client），异步写入，不阻塞主流程。
13. 登录失败计数与限流基于 Redis，按 `username + IP` 双维度。
14. 所有外部输入解析（SAML / CAS 若启用）必须关闭外部实体（防 XXE）。

---

## 10. 工作量与里程碑

按 10 年经验的后端工程师单人估算：

| 模块 | 人日 |
|---|---|
| 接入 `zitadel/oidc` + Storage 适配 + discovery / JWKS | 5–8 |
| 登录 / 同意闭环 + session + argon2id | 3–5 |
| 授权码 / 令牌 / 刷新轮换 / PKCE 约束 | 3–5 |
| 用户 / 组织 / 客户端管理 API + Vue 前端 | 8–12 |
| MFA（TOTP） | 2–3 |
| 企微 / 钉钉 / 飞书（均非标准 OIDC，需单独实现） | 各 1–2 |
| LDAP / AD 同步 | 3–5 |
| 审计事件 + 查询页 | 3 |
| 部署 / CI / 可观测 / 备份 | 3 |
| **合计（不含 SAML / CAS）** | **30–45 人日** |

对比：Keycloak + 门户约 8–10 人日。**自研溢价约 3–5 周。**

### 建议排期（单人 6–9 周，双人 3–4 周）

| 阶段 | 目标 | 出口标准 |
|---|---|---|
| W1 | fx 骨架 + 两个 http.Server + MySQL/Redis 接入 + 五张表 DDL | 服务可启动，健康检查通过 |
| W2 | `op.Storage` 适配 + 登录闭环 | 端到端跑通"登录 → 授权码 → token → userinfo" |
| W3 | 安全约束（PKCE / 轮换 / 重放检测）+ Conformance Suite basic | basic OP profile 通过 |
| W4–5 | 管理 API + Vue 后台（用户/组织/应用） | 可新增用户、注册 client 并完成登录 |
| W6 | 账号中心 + MFA + 审计日志 | 用户可自助改密、绑定 MFA、查看记录 |
| W7 | 接入 2 个真实业务系统 + 老系统 oauth2-proxy 样板 | 真实系统可用 |
| W8–9 | 企微/钉钉/飞书、LDAP 同步、加固与文档 | 可交付试点 |

---

## 11. 验收标准

1. **OpenID Conformance Suite**：通过 `basic OP` + `config OP` 两个 profile（Docker 启动官方测试套件）。这是自研方案唯一可靠的外部背书，不可跳过。
2. **安全用例自测**：授权码重放、refresh token 重放、`redirect_uri` 篡改、`alg` 混淆、PKCE 降级为 `plain`，均须被拒绝且产生审计记录。
3. **端到端接入**：至少 1 个 Spring Boot 服务 + 1 个 Go 服务 + 1 个 SPA + 1 个无改造老系统（oauth2-proxy）全部登录成功。
4. **单点登出**：登出后所有接入方的会话在 5 秒内失效。
5. **可观测**：协议端点有 QPS / P95 延迟 / 错误率指标；审计可按用户与时间检索。

---

## 12. 风险登记表

| 风险 | 等级 | 影响 | 缓解 / 触发条件 |
|---|---|---|---|
| 协议细节实现错误导致安全漏洞 | 高 | 会话或令牌被利用 | 使用 `zitadel/oidc/v3`；遵守 §9；跑 Conformance Suite |
| 低估工作量，交付延期 | 中高 | 拖延接入方排期 | 第 3 周末端到端未跑通即触发止损（见下） |
| SAML IdP 能力薄弱 | 中 | 老系统无法接入 | Go 仅有 `crewjam/saml`（部分实现、活跃度低）。**不硬啃**：用 Casdoor 单独做 SAML/CAS 协议网关，联邦回本系统 |
| CAS 协议无成熟 Go 库 | 中 | 额外 2–3 周 | 非必需则不做；必需则评估外挂 |
| 多副本 AuthRequestStorage 失效 | 中 | 灰度扩容时登录随机失败 | MVP 承诺单副本；同步实现 Redis 版 |
| GORM 隐式行为引发吊销失效 | 中 | 安全问题隐蔽 | 遵守 §8.3；关键路径手写 SQL 并加集成测试 |
| 长期维护依赖单人 | 中 | 交接困难 | 文档化架构决策、手写迁移、可观测齐备；数据模型标准化以保留迁移后路 |
| 上游库 CVE 无厂商兜底 | 低中 | 需自行响应 | 订阅 GitHub Security Advisory；锁定 minor 版本并定期升级 |
| 迁移回 Keycloak 成本 | 低 | — | 用户密码（argon2id）与 client 配置可导出，组织模型需重建 |

### 止损条件

**若第 3 周末仍未能跑通"登录 → 授权码 → token → userinfo"端到端，或 Conformance Suite basic profile 无法通过，则立即切换 Casdoor。** 切换成本可控：五张表可平滑导入，`portal` 与 BFF 代码无需改动（两者均为标准 OIDC）。

---

## 13. 不推荐但常见的做法

| 做法 | 为什么常见 | 为什么不推荐 |
|---|---|---|
| Spring Security + 自写 JWT 登录（不用标准 OIDC） | 上手最快，看似够用 | 无法接第三方、无单点登出、无 refresh 轮换；自研的令牌吊销与重放检测普遍存在漏洞 |
| 把 NextAuth / Spring OAuth2 Client 当 IdP | 前端项目顺手引入 | 它们是 RP（依赖方），只能接入别人，无法给其他系统签发身份；到第三个系统即卡死 |
| Redis 共享 Session（Spring Session）当 SSO | 简单，同域可用 | 跨语言、跨系统、跨机房无解，且无授权、同意、审计能力 |
| 把 Keycloak Admin Console 交给业务方使用 | 零开发成本 | realm / client / flow 概念暴露给非技术用户，一次误改即全公司登录失败；升级后 UI 变动 |
| 直接以 LDAP / AD 作为认证接口 | 传统企业惯性 | 域控暴露给所有业务系统，无法做 MFA、审计、自助改密与统一密码策略。应做 IdP 联邦 |
| Auth0 / Okta / Cognito 云服务 | 省事 | 内网访问、数据出境合规、按 MAU 计费在数百人规模不划算 |
| 自研 UI 时混用两个组件库 | 想"取各家之长" | 样式重置冲突、包体积翻倍、主题不可统一 |

---

## 14. 附录

### 14.1 术语

| 术语 | 含义 |
|---|---|
| IdP | Identity Provider，身份提供方（本系统） |
| OP | OpenID Provider，OIDC 中的身份提供方角色 |
| RP | Relying Party，依赖方（接入的业务系统） |
| BFF | Backend for Frontend，前端专属后端，负责持有凭证与聚合 |
| PKCE | Proof Key for Code Exchange，公共客户端的授权码保护机制 |

### 14.2 参考

- OpenID Connect Core 1.0 / RFC 6749 / RFC 7636（PKCE）/ RFC 7009 / RFC 7662
- OAuth 2.1 draft（security best current practice）
- `github.com/zitadel/oidc` v3 文档与 `example/server`
- OpenID Foundation Conformance Suite
- OWASP ASVS 4.0 第 2、3 章（认证与会话管理）

### 14.3 决策记录

| 日期 | 决策 | 说明 |
|---|---|---|
| 2026-10-20 | 放弃 Keycloak / Casdoor 底座，改为自研 | 技术栈统一、登录体验可控、组织模型贴合；以 Conformance Suite 与止损条件对冲风险 |
| 2026-10-20 | 协议层采用 `zitadel/oidc/v3` | 禁止手写 OIDC 状态机 |
| 2026-10-20 | DI 采用 fx（而非 dig） | 需要 lifecycle 管理 server 与后台任务 |
| 2026-10-20 | 前端拆为 portal / admin 两个产物 | 登录页定制与后台效率的冲突无法在一个产物内调和 |

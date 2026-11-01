# Contract: 门户应用管理员端组织授权接口

**Feature**: `010-app-access-org-grant` | **Base**: `http://<api>:8081/api/v1` | **Auth**: `cookieAuth` + `appAdmin`（该应用的应用管理员）

## 定位

三条端点与 admin 端**同一实现**（`internal/api/admin/app_auth.go` 的 handler，门户侧仅做 `h.appMgmt.<Method>(c)` 转发），因此请求/响应体、字段语义、幂等性、上限、审计与 [admin-org-grants.md](./admin-org-grants.md) 完全一致，本文件只规定门户特有的路径与鉴权差异，**不重复字段定义**。

| 门户路径 | 对应 admin 路径 | 说明 |
|---|---|---|
| `GET /me/apps/:id/org-grants` | `GET /admin/clients/:id/org-grants` | 响应同 GT-1 |
| `POST /me/apps/:id/org-grants` | `POST /admin/clients/:id/org-grants` | 请求/响应同 GT-2 |
| `POST /me/apps/:id/org-grants/remove` | `POST /admin/clients/:id/org-grants/remove` | 请求/响应同 GT-3 |

成员列表（`GET /me/apps/:id/members`）与候选列表（`GET /me/apps/:id/members/candidates`）同步获得 MT-1 / MT-2 的新增字段。

## 鉴权语义（AGENTS §4.1「应用管理员」段落）

- 只判断「当前用户是否为该应用的应用管理员」（`client_members.is_admin`），**不挂 `Admin()`**，平台 admin 角色不自动放行。
- 每请求实时查库，**禁止加任何缓存**：授予/取消应用管理员后下一次请求即生效。
- 拒绝语义统一为 `403 FORBIDDEN`（应用不存在 / 非成员 / 非管理员 / 应用已停用同一文案），具体原因只进应用日志，不泄露应用是否存在。
- 未登录 → `401 UNAUTHENTICATED`（`OptionalAuth`/`Auth` 中间件既有行为）。

## 门户开放范围（本特性新增后）

| 能力 | admin 端 | 门户应用管理员端 |
|---|---|---|
| 成员增删 / 候选 | ✅ | ✅ |
| 角色 / 权限 / 授权 | ✅ | ✅ |
| 应用信息（名称/描述/Logo/回调） | ✅ | ✅（006） |
| **组织授权（列表 / 新增 / 撤销）** | ✅ | ✅（本特性） |
| 密钥轮换、设为/取消应用管理员 | ✅ | ❌（门户不注册该 path，直接请求落 `NoRoute`） |

## 鉴权挂载要求（critique E7）

三条门户路由 MUST 注册在 `appAdminGroup`（不得落到 `authed` 组）；否则任何登录用户都能读取任意应用的组织授权列表（含部门名与人数），属信息泄露。`router_test.go` MUST 增加断言：普通登录用户请求 `GET /api/v1/me/apps/:id/org-grants` 返回 `403 FORBIDDEN`。

## 审计与操作者

门户路径复用同一 handler，`Auditor.Emit` 通过 `service.WithActor` 从 request context 回填操作者（006 已实现），因此门户侧的组织授权事件同样带 `userId`/`username`。**不引入 `entry` 字段**（critique E15）：来源可由 `trace_id` 与操作者定位，避免为可选信息增加 wrapper→service 的额外传递面。

## 文档计数

新增 `(method,path)` 组合 6 个（两个端各 3 个），`internal/api/router_test.go` 的 operation 断言由 78 调整为 84；`AGENTS.md` §4.1 需新增 6 行（`scripts/check-registry.sh` 逐条比对）。

# Research: 应用详情子页面与应用内授权体系

**Feature**: `003-app-management-pages` | **Date**: 2026-10-21

本文件记录设计阶段需要拍板的技术决策、依据与被否决的备选方案。除 R1 涉及对外字段命名外，其余均为内部实现取舍。

## R1. userinfo 新增字段的命名：`app_roles` + `permissions`（不覆盖既有 `roles`）

- **Decision**: 用户信息接口新增 `app_roles`（应用内角色名数组）与 `permissions`（权限编码数组），既有 `roles` 继续承载平台角色（`users.roles`，值为 `["user","admin"]` 这类全局角色）。
- **Rationale**: 现状 `internal/oidc/claims.go` 已在 id_token 与 userinfo 中输出 `roles` = `users.roles`，且有接入方依赖；`AGENTS.md` 明确"claim 命名一旦上线不得更改"。把应用内角色塞进同名 `roles` 会造成"同名不同义"，属于静默破坏。`app_roles` 与 `permissions` 均为纯新增，符合 spec FR-029"既有字段不变"。
- **Alternatives**: ① 覆盖 `roles` 为应用内角色，另加 `platform_roles` 承载原值 —— 破坏既有消费者，且需要一次全量接入方通知；② 嵌套对象 `application:{roles,permissions}` —— 与 spec"两个字符串数组"的表述不符，前端与接入方取值更绕；③ 只在 id_token 输出、userinfo 不动 —— 不满足 spec FR-027。

## R2. userinfo 的应用上下文来源：签发时落 `access_tokens` 映射

- **Decision**: 新增表 `access_tokens(token_id CHAR(26) PK, user_id, client_id, expires_at, created_at)`。在 `CreateAccessToken` 与 `CreateAccessAndRefreshTokens` 中写入 `(tokenID, subject, clientID, exp)`；`SetUserinfoFromToken` 用 `tokenID` 反查 `client_id`，再调 `AppAuthService.Snapshot(ctx, clientID, userID)`。
- **Rationale**: 查阅 `zitadel/oidc v3.28.0` 后确认：userinfo 端点调用 `SetUserinfoFromToken(ctx, info, tokenID, subject, origin)`，其中 `origin` 取自 `Origin` 请求头而非 clientID；`CanSetUserinfoFromRequest` 只在 id_token 生成路径（`pkg/op/token.go:226`）被调用。因此**必须**在签发侧记录映射，否则无法满足 FR-028"按令牌所属应用计算"。该表是唯一新增的"非业务实体"表，写入为一次主键插入，读为一次主键查询。
- **写入失败的语义**：插入失败即**签发失败**——`CreateAccessToken` / `CreateAccessAndRefreshTokens` 返回 error、不签发令牌，并打 `slog.Error("access token mapping insert failed", "client_id", …, "sub", …)`。**不得**降级为"照常签发 + userinfo 返回空数组"：那会让接入方把"DB 抖动"误读成"该用户没有任何权限"，前者可重试、后者会按无权限放行或拒绝，且只能靠外部反馈发现。代价是登录/换令牌依赖 DB——但签发路径本就要读 `clients` 校验，依赖面不变。
- **Alternatives**: ① 把 clientID 编进 `jti` 再在 storage 内解析 —— 依赖 op 对 token 形态的处理（JWT 时 jti 可见、不透明时被加密），语义脆弱；② Redis 缓存映射 —— 受淘汰策略影响，可能返回空数组，违反 SC-003 的 100% 一致；③ 在 storage 内验签访问令牌取 `aud` —— 属协议校验，违反原则 I；④ 返回用户在所有应用的角色并集 —— 违反 FR-028。

## R3. 门户可见性：`ClientService.ListForUser` 改为成员制

- **Decision**: `ListForUser` 由"所有 `status=1` 的应用"改为"`client_members` 中存在 `(client_id, user_id)` 且 `clients.status=1`"，排序与 `lastAccessAt` 逻辑保持不变；返回字段不变。
- **Rationale**: spec FR-012 要求非成员不可见，且既有 DTO `AppItem` 已被门户消费，改数据来源而不改报文最稳。实现上用一条 join 查询，避免 N+1。
- **Alternatives**: ① 在门户侧过滤 —— 管理端与门户会各有一套可见性规则，易漂移；② 新增 `is_public` 语义复用（该列已表示"公共客户端"，不可复用）。

## R4. 批量成员操作语义

- **Decision**: 添加用 `INSERT IGNORE`（或 `ON DUPLICATE KEY UPDATE joined_at=joined_at`）一次性写入，返回 `{added, skipped}`；移除用 `DELETE ... WHERE client_id=? AND user_id IN (...)` 加同一事务内的 `client_role_members` 清理，返回 `{removed, roleBindingsRemoved}`；提交前对 `userIds` 去重并剔除不存在/已删除的用户，缺失用户在结果中计数。
- **Rationale**: 幂等由唯一索引保证，不需要"先查后插"；计数直接取 `RowsAffected`，与界面提示一致（SC-005）。批量移除若要显示"连带解除多少角色绑定"，需先按 `IN` 统计一次，再删，两步在同一事务内。
- **Alternatives**: 逐条循环调用单条接口 —— 500 人时产生 500 次往返，前端还需自行汇总；用 `REPLACE INTO` —— 会重置 `joined_at`，破坏"加入时间"语义。

## R5. 唯一性用数据库唯一索引 + 显式大小写规则

- **Decision**: `client_roles` 上 `UNIQUE (client_id, name)`，`name` 列 `COLLATE utf8mb4_0900_as_cs`；`client_permissions` 上 `UNIQUE (client_id, code)`，`code` 列同样 `as_cs`。冲突时把 MySQL `1062` 映射为 `APP_ROLE_NAME_EXISTS` / `APP_PERMISSION_CODE_EXISTS`。
- **Rationale**: 本特性已引入迁移，具备加索引条件；把并发正确性交给数据库，比 002 的"全表 `FOR UPDATE`"更适合 17 个操作的吞吐。spec 要求权限编码区分大小写，角色名同理（与组织名、用户名的既有约定一致）。
- **Alternatives**: ① 事务内 `SELECT ... FOR UPDATE` 判重 —— 无索引兜底时对并发不成立；② 用 `utf8mb4_0900_ai_ci`（库默认）—— 会让 `Admin` 与 `admin` 冲突，与"仅字母数字、大小写敏感"的验收场景不符。

## R6. 本次只在 userinfo 投影角色权限，不改 id_token

- **Decision**: `app_roles` / `permissions` 仅出现在 userinfo 响应；id_token 保持现状（`tenant`/`dept_id`/`roles`）。
- **Rationale**: spec FR-027 只要求 userinfo；id_token 增字段会放大令牌体积、影响所有接入方的解析与缓存，收益不明。`GetPrivateClaimsFromScopes` 保留现状可把风险面压到最小。
- **Alternatives**: 同步加入 id_token（对前端更省一次调用，但超出 spec）；加入 introspect（无明确需求）。

## R7. 授权写接口：`PUT` 整体覆盖

- **Decision**: `PUT /admin/clients/:id/roles/:rid/permissions` 与 `.../members` 接收完整 `permissionIds` / `userIds`，服务端在事务内做"差集删除 + 缺失插入"，返回最终集合与数量。两类覆盖式写入**都不分片**：角色成员实际规模为数十到数百，一次整体提交即可；`userIds` ≤5,000、`permissionIds` ≤1,000 只是防御性边界（分片会破坏"提交最终态"的语义，见 E3 的取舍）。
- **Rationale**: 前端以"勾选后保存"为交互，提交的天然是最终态；整体覆盖幂等、可重试，且一条事务即可保证不变量（有角色必为成员）。并发覆盖用"读取时的 `updatedAt` 不符则提示刷新"处理（spec Edge Cases）。
- **Alternatives**: 增量 `POST/DELETE /roles/:rid/permissions/:pid` —— 500 次勾选会产生大量请求，且前端需自行 diff。

## R8. 应用详情可编辑字段与校验（spec Q3=C）

- **Decision**: `PATCH /admin/clients/:id` 接受 `clientName`、`description`、`logoUrl`、`redirectUris`、`postLogoutUris`、`accessTokenTtl`、`refreshTokenTtl`、`status`；全部为指针字段（缺失=不变）。校验：名称 1–128 且 trim 非空；URL 必须 `http(s)` 且绝对地址；`redirectUris` 至少 1 条、每条 ≤ 512、去重；`accessTokenTtl` ∈ [60, 86400]，`refreshTokenTtl` ∈ [300, 31536000]；`status` ∈ {0,1}。响应返回更新后的详情，并提示有效期"仅对此后签发的令牌生效"。
- **Rationale**: 与 spec FR-004 的字段清单逐项对应；指针字段沿用 002 已验证的"三态"处理方式，避免"未提交被写成零值"。`client_id` 与密钥不在入参中，从协议上保证 FR-005。
- **Alternatives**: 一次性 PUT 全量替换 —— 未提交字段会被清空，误操作风险高；拆成多个细粒度接口 —— 前端保存一次要发多次请求，事务边界模糊。

## R9. 审计事件与错误码的最小集合

- **Decision**: 审计事件 10 个：`client_updated`、`app_member_added`、`app_member_removed`、`app_role_created`、`app_role_updated`、`app_role_deleted`、`app_permission_created`、`app_permission_updated`、`app_permission_deleted`、`app_grant_updated`；错误码 5 个：`APP_ROLE_NAME_EXISTS`(409)、`APP_PERMISSION_CODE_EXISTS`(409)、`APP_PERMISSION_HAS_CHILDREN`(409)、`APP_PERMISSION_INVALID_PARENT`(400)、`APP_RESOURCE_STALE`(409)。
- **Rationale**: 事件粒度与"可筛选、可追溯"的要求匹配，且不为每个字段变更单独建事件；角色名/编码格式错误复用 `VALIDATION_FAILED`（message 说明原因），避免错误码膨胀；删除含子节点与非法父级需要独立的可读提示，因此单列两个码。授权变更用一条 `app_grant_updated`（detail 记录 roleId 与两类差集数量）。`client_updated` 为 002 未覆盖的应用更新事件（既有枚举只有 `client_created` / `client_secret_rotated`），FR-004 要求详情保存必须留痕。
- **detail 的详略规则**（对本特性所有事件一致）：一般只记字段名列表与数量，不记值；**唯一例外是地址类字段（`redirectUris` / `postLogoutUris`）必须记变更前后的完整数组**——它们决定授权码流向哪个域，只记"改过"无法支撑安全复盘，且本就在配置中、非凭证；删除角色/权限时记被连带解除的绑定快照（`removedPermissionIds` / `removedUserIds`，超过 20 项时记数量 + 前 20 项），使误删后能按审计手工重建。
- **Alternatives**: 每个操作都建 `_failed` 事件（审计表翻倍，且失败原因已在 detail）；把所有应用事件合成一条 `app_updated`（无法按事件类型筛选，违反 FR-031）。

## R10. 前端信息架构与跨页全选

- **Decision**: 路由 `/clients/:clientId/:tab(overview|members|roles|permissions|grants)`，`ClientDetail.vue` 负责左侧 `el-menu`（用 `router` 模式）与右侧 `<router-view>`；默认 `overview`。成员页跨页全选由"当前筛选条件下的全部 ID"支撑：进入"全选"时调用一次轻量接口（`pageSize=0` 或 `all=true` 返回 ID 列表）并保存在页面状态，配合"反选"计算差集。
- **Rationale**: 与现有 `Users.vue` 的组织树/表格布局一致，管理员无需重新学习；URL 可直接分享与刷新恢复（FR-002）。
- **Alternatives**: 全选只作用于当前页 —— 违反 FR-010；把选中集合放 Pinia —— 跨页面残留选中态，反而容易误删。

## R11. 权限"树形/列表"是纯前端视图

- **Decision**: 后端只存 `parent_id` 与 `sort`，一次返回该应用的扁平权限列表；前端用同一份数据渲染树或表格，`view` 仅作为 UI 状态（`localStorage` 记忆，不入库、不入接口）。
- **Rationale**: spec Q2 的结论是"同一份数据两种视图"；把视图状态放进数据库会引入无意义的应用配置项，且切换模式时还得处理层级数据（违反"改动最小"）。
- **Alternatives**: 在 `clients` 上加 `permission_view` 列 —— 需要迁移与"谁改的"审计；两种模式各建表 —— 数据重复。

## R12. `access_tokens` 的清理策略

- **Decision**: 在 `CreateAccessToken` / `CreateAccessAndRefreshTokens` 内，按**限频**执行 `DELETE FROM access_tokens WHERE expires_at < ? LIMIT 500`：仅当进程内记录的 `lastCleanup` 距今 > 1 分钟时才执行一次（单次最多 500 行）。失败只记 `slog.Warn`，不影响签发。
- **Rationale**: 项目当前没有后台清理任务（`auth_codes` 的 24 小时清理也未实现），为一个辅助表引入 fx 管理的定时器成本过高；`expires_at` 有索引，空集删除为常量级开销。限频是为了避免"长期未清理后的首次签发被大批删除拖慢"——单次签发最多付一次 `LIMIT 500` 的代价，且无需多轮即可在若干分钟内排空。
- **Alternatives**: ① 新增定时清理组件 —— 新的生命周期与可观测面，超出本特性范围；② 不清理 —— 表无界增长，最终拖慢 userinfo；③ 用 Redis TTL —— 见 R2 的一致性否决理由。

## R13. 并发编辑：`updatedAt` 乐观版本校验（不放宽也不加锁）

- **Decision**: 应用详情、角色、权限与两类授权的写请求接受可选的 `updatedAt`（毫秒，等于调用方读取到的 `updated_at`）；提交值与服务端当前值不一致 → `409 APP_RESOURCE_STALE`（message"已被他人修改，请刷新后重试"）。缺省不校验，以兼容 curl/脚本调用。校验与写入在同一事务内：先 `SELECT ... FOR UPDATE` 读当前 `updated_at`，比较后再写并刷新 `updated_at`。
- **Rationale**: spec Edge Cases 承诺"后保存者覆盖前者时页面提示已被他人修改"，但原设计的三类写接口都没有版本参数，服务端无从判断，承诺无法兑现（评审 E11）。`updated_at` 已存在于 6 张表中（`client_roles`/`client_permissions` 已是列；`clients` 既有列），无需新增字段与迁移。整体覆盖式写入本身是幂等的，版本校验只解决"覆盖别人的新改动"。
- **Alternatives**: ① `If-Match: <etag>` —— 语义更标准，但现有前端统一走 JSON 体，混用 header 会分裂错误处理；② 悲观锁（打开编辑面板即 `FOR UPDATE`）—— 跨 HTTP 请求持锁，连接池与面板停留时长不可控；③ 不做校验只提示 —— 当前状态，等于让管理员靠肉眼比对。

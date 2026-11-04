# Research: 应用权限点批量导入（011）

**Date**: 2026-11-03 | **Plan**: [plan.md](./plan.md) | **Spec**: [spec.md](./spec.md)

本文件针对 spec 未预设、实现前必须定稿的 10 个决策点逐条给出结论。所有"现状"事实均带路径与行号，可直接核对；结论与 spec 冲突时，冲突处已在条目内显式说明处置方式。

---

## R1 · 导入通道形态：单一 JSON body，不引入 multipart

**Decision**：两个端点（校验预览、确认导入）都只接受 `application/json`，body 即导入文档本身（`{version?, mode?, permissions[]}`）。前端"选择本地文件"时用浏览器原生 `File.text()` 读出文本，再作为同一个 JSON body 提交——两条输入方式在网络上完全同形。

**Rationale**：spec FR-001 要求"两种方式走同一条处理路径与同一套校验规则"。纯 JSON body 只有一条解析路径，文件方式退化为前端读取方式，服务端无从区分（也不需要区分）。同时导出（R9）必然是 JSON 结构，若导入用 multipart，两种形态会在同一功能里并存两套约定。项目侧也无 multipart 先例可循：唯一的"上传"是 `web/admin/src/components/ImageUpload.vue` 的 OSS 直传（不经后端请求体），后端 `internal/api` 无任何 `MultipartForm` 用法。

**Alternatives considered**：
- `multipart/form-data` + `c.FormFile`：需处理临时文件、文件名编码、大小双重校验，而"是否文件"只影响审计 `source` 字段——由一个前端布尔值就该解决的事，不值得引入解析分支。
- 让前端把文件内容 base64 塞进 JSON：body 体积放大 33%，与 512KB 上限直接冲突。

**来源（`paste`/`file`）的传递方式**：由 **query 参数 `?source=paste|file`** 携带（缺省与非法值一律按 `paste` 处理），**不写进导入文档**。理由有三：① 服务端无法从 JSON body 推断输入方式，而 FR-023 要求审计记录来源；② 导入文档必须保持"导出产物可原样回导"的纯净性（FR-019）——多一个 `source` 字段会让自家导出文件被自己的未知字段校验拒绝（FR-006 规定未知字段一律拒绝）；③ **query 不触发 CORS 预检**，而自定义头会——当前 admin/portal 与 API 同源（Vite 代理 / nginx 同源），但 007 已是四域拓扑，一旦前后端分域，自定义头会先发 `OPTIONS`，而管理面没有 CORS 中间件（宪法也禁止在 `/oauth2/*` 挂 CORS）。用 query 顺手消掉这个隐患，也免去"非法头取值怎么处理"的分歧：白名单外一律 `paste`。

**Alternatives considered（来源传递）**：
- 放进 body 顶层 → 与 FR-006 的严格白名单冲突（`version`/`mode`/`permissions` 之外一律拒绝），要么开白名单例外（文档不再纯净），要么导出时写死一个值（失去意义）。
- 自定义头 `X-Import-Source`（初版方案）→ 非法取值行为未定义，且分域部署会触发预检；已否。

---

## R2 · 512KB 体积上限的执行点：handler 内 `http.MaxBytesReader`

**Decision**：在导入 handler 读取 body 前执行 `http.MaxBytesReader(c.Writer, c.Request.Body, maxImportBodyBytes)`（`maxImportBodyBytes = 512 << 10`），读全量后若命中 `*http.MaxBytesError` → `413 APP_PERMISSION_IMPORT_TOO_LARGE`。仅在两个导入端点生效。

**为什么是 512KB 而不是 256KB**：1000 条的上限必须可达。最坏情况一条填满 `code`(64) + `name`(64) + `description`(255) ≈ 430 字节，加 JSON 结构开销后 1000 条约 **430KB**——256KB 会让"条数上限还没到，体积先撞墙"，且管理员按提示裁剪时会误以为要删条目，实际只需缩短描述。512KB 覆盖该最坏情况，同时仍远小于任何合理的滥用阈值。

**Rationale**：仓库当前**不存在**任何请求体体积控制（`*.go` 全库 0 处 `MaxBytesReader`/`LimitReader`/`ContentLength` 判空），因此任何"全局 body limit"都是新行为，会影响全部 `/api/v1/**` 端点（含既有 40+ 写端点）并需要额外回归。把上限收在导入 handler 内，影响面等于本特性自身。`http.MaxBytesReader` 是标准库，不触发 §2.0 的依赖引入流程。

**Alternatives considered**：
- 只检查 `c.Request.ContentLength`：chunked 请求没有该头（值为 -1），可被绕过；`MaxBytesReader` 在读取时计数，无法绕过。
- 全局中间件：与"协作纪律·不做顺手优化"冲突，且 `/oauth2/*` 在另一端口，收益为零。

**落地**：`MaxBytesReader` 的超限错误在 `json.Unmarshal` 之前被显式判定（先 `io.ReadAll` 再 `Unmarshal`，以便区分"体积超限"与"语法错误"）；`maxImportBodyBytes` 同时作为前端"选文件后立即检查 `file.size`"的依据（前端预检只是体验优化，服务端仍为唯一防线，符合 FR-027）。

---

## R3 · 三个上限以常量固化，不新增环境变量

**Decision**：`maxImportPermissions = 1000`、`maxImportBodyBytes = 512 << 10`、`maxImportIssues = 20` 写在 `internal/service/app_permission_import.go` 顶部 const 块；**层级上限直接复用既有的 `maxPermissionDepth = 100`**（`internal/service/app_auth.go:63`），不新增 `maxImportDepth`。**不新增任何环境变量**。

**Rationale**：全仓库所有业务上限都是 Go 常量，集中在 `internal/service/app_auth.go:55-64`（`maxMemberBatch=500`、`maxSelectAllIDs=5000`、`maxPermissionGrant=1000`、`maxPermissionDepth=100` 等），`internal/config/config.go` 无任何上限类变量，AGENTS §9 环境变量清单亦无此类条目。新增 env 需要同时改 `config.go` + `.env.example` + AGENTS §9 三处（`scripts/check-registry.sh` 第 4 项会三向比对），并落入"不预留配置项"的禁区。spec 中的"1000 条 / 512KB / 100 层"按**固定值**实现，不承诺可配置。

**Alternatives considered**：
- 走 env（`PERMISSION_IMPORT_MAX_ITEMS` 等）：让运维可以在不改代码的情况下放宽上限，看似灵活；但既有限额（成员批量 500、角色授权 1000）都没这么做，本特性单独开例会造成"同类事两套机制"。
- 走 `config` 但不进 env（代码内可注入）：`fx` 装配层会多一个无实际差异的依赖，且测试要构造它。

---

## R4 · 响应分工：预览"业务失败也是 200"，确认导入"只给码不给明细"

**Decision**：

| 场景 | 校验预览 `POST .../import/validate` | 确认导入 `POST .../import` |
|---|---|---|
| 内容合法 | `200`，`data.ok=true` + 三态计数 + 空 `issues` | `200`，`data` = 实际三态计数 |
| 内容非法（结构/字段/编码/重复/超深/超长/空/语法错） | `200`，`data.ok=false` + `issues[]`（≤20 条，含 `issueTotal`） | `400 APP_PERMISSION_IMPORT_INVALID`，`data: null` |
| 条数或体积超限 | `413 APP_PERMISSION_IMPORT_TOO_LARGE`（体积）/ `200 + issues`（条数，因可解析、需给"实际 N 条 > 上限 1000"的可定位提示） | `413 APP_PERMISSION_IMPORT_TOO_LARGE` |
| 应用不存在 | `404 NOT_FOUND` | `404 NOT_FOUND` |
| 应用停用 | admin：`403 FORBIDDEN`（带"应用已停用"文案）；门户：`403 FORBIDDEN`（中间件统一，不泄露） | 同左 |
| 并发冲突 | 无（只读） | `409 APP_RESOURCE_STALE` |

**Rationale**：这是 spec 两处要求的唯一自洽结合点。FR-005 要求校验失败"给出可定位的错误清单（第几条、路径、字段、原因）"，FR-029 又要求失败响应 `data` 为 null 且明细只走"校验预览的成功响应"。若预览对非法内容返回 400，明细无处可放（统一响应约束禁止在失败响应带 `data`）；因此把"内容非法"定义为**预览的业务结论而非 HTTP 失败**——预览端点只要成功完成"读取 + 判断"，就返回 200。与之对应，`data.ok` 是布尔结论，前端据它决定是否允许"确认导入"。确认导入不重复给明细，因为正常流程必经预览（FR-029 末句）。

**Alternatives considered**：
- 预览非法时返回 400 + `message` 里塞第一条错误：违反 FR-005（只报第一条）与统一响应约束。
- 在失败响应里破例允许 `data` 带 issues：需要改 `response.Fail` 的全局契约（`internal/api/response/response.go:49-59` 硬编码 `Data: nil`），影响面远大于本特性。
- 引入"预览票据"：见 plan §Complexity Tracking，已否。

**落地**：条数超限在预览中作为 `issues` 的一条（`code: "too_many"`），在确认导入中映射为 413——两者对管理员是同一件事，但 HTTP 语义不同（预览仍需返回清单）。体积超限在预览阶段无法解析，只能 413。

**预览响应的形状（含 FR-016 的明细要求）**：`{ok, added, updated, unchanged, moved, total, issues[], issueTotal, warnings[], warningTotal, movedItems[]}`。`movedItems` 最多 20 条，每条 `{code, fromParent, toParent}`（父级用 `code` 表达，根级为 `""`）——只给 `moved` 条数会让管理员在确认前无法分辨"改名"与"换爹"，而后者会改变整棵树的形状。

**解析防护**：必须用 `encoding/json`（内部有最大嵌套深度保护，超深会报语法错误而不是爆栈），**禁止自研递归下降解析器**；解析成功后立刻按 `maxPermissionDepth` 做深度检查给出 `too_deep`。

---

## R5 · 写入算法：事务内锁 `clients` 行 + 一次全量读 + 内存 diff + 分层 BFS 写

**Decision**：确认导入在 `store.Transaction` 内按固定顺序执行：

1. `SELECT ... FROM clients WHERE client_id = ? FOR UPDATE`（新 store 方法 `LockClientRow`）——把同一应用的导入串行化；
2. 一次读全量 `SELECT * FROM client_permissions WHERE client_id = ?`（新 `LockClientPermissions`，带 `FOR UPDATE`）；
3. 内存 diff：以 `code` 建索引，得到 `toInsert / toUpdate / unchanged / moved`；
4. 按输入树 BFS（父先子后）逐层写：`toInsert` 用 `CreateInBatches`（每批 100，ULID 主键），`toUpdate` 逐条 `Updates`（不走 `UpdateClientPermission` 的乐观锁——本事务已持锁）。

**Rationale**：既有单条链路无法承载批量。`CreatePermission` → `ensureParent`（`internal/service/app_auth.go:670-693`）**每个节点都要沿父链逐层 `GetClientPermission`**，最多 100 次单查；1000 条输入最坏会产生十万级查询，与 SC-005（5s p95）不可兼得。而"父级必须已存在"在导入语境下是**可推导的**：输入的每一层父级要么是本次输入里的节点（自己刚插入，ID 已知），要么本就在库里（本次一次全量读已拿到）。因此导入完全不需要 `ensureParent`。

`clients` 行锁是必要的而不是冗余的：只对 `client_permissions` 做范围 `FOR UPDATE` 时，若该应用**尚无任何权限点**（最常见的新应用导入场景），锁的是间隙而非记录，两个并发事务都得到空快照、都判"全部新增"，最终第二个事务撞唯一键 `uk_client_permissions_code` 回滚。锁 `clients` 的主键行（`model.Client.ClientID` 为 primaryKey，`internal/store/model/client.go:7-8`）是确定的记录锁，语义清晰。

同一坑在 002 已有记录：空表上的 `SELECT ... FOR UPDATE` 只取到 supremum 间隙锁，而间隙锁彼此兼容，两个事务都能通过"同名检查"，插入时互相冲突形成 1213 死锁（见 `specs/002-org-structure-crud/data-model.md` 的并发表与 `research.md` R5）。锁 `clients` 行同时消除了"新应用并发导入死锁"这条路径，不必再对 1213/1205 做重试——本特性的第一把锁是确定的主键记录锁，不存在间隙锁竞争。

**Alternatives considered**：
- 循环调用 `CreatePermission`：见上，查询放大；且每条都要 `ensureClient`（`GetClient` 一次）→ 1001 次。
- `INSERT ... ON DUPLICATE KEY UPDATE` 一条批量语句：无法区分"新增/更新/无变化"三态计数——`RowsAffected` 的 1/2/0 语义在 MySQL 里不可靠（010 已因这个坑放弃 `ON DUPLICATE KEY`，见 `specs/010-app-access-org-grant/plan.md` E3），而三态计数是本特性的核心输出。
- 用 `updated_at` 做并发乐观锁：导入没有单条的 `updatedAt` 入参（spec 也没给），逐条比对会把"别人改了一条"变成整批失败，与管理员的预期不符；行锁串行化更简单。

**并发冲突映射**：仍可能因外部路径（单条创建）在事务外插入同 `code` 而撞 1062，此时 `store.isDuplicate`（`internal/store/app_auth_store.go:89-95`）判定 → service 映射为 `ErrAppResourceStale` → `409 APP_RESOURCE_STALE`（既有 message"已被他人修改，请刷新后重试"覆盖 FR-018 的提示要求，不新增错误码）。

**并发单条编辑会被覆盖（已知行为，不是缺陷）**：`FOR UPDATE` 读保证"读到的是最新已提交状态"，但导入以文档为准（FR-016），因此另一个管理员在导入提交前改的名字/描述会被文档覆盖。`409 APP_RESOURCE_STALE` **只表示唯一键冲突**，不表示"有人改过"。实现者不得为此加逐条 `updated_at` 乐观锁——那会让"999 条正常 + 1 条被人改过"变成整批失败，与管理员的预期完全相反。

---

## R6 · 层级深度：导入直接复用既有 `maxPermissionDepth=100`，不另设上限

**Decision**：导入的深度上限就是既有的 `maxPermissionDepth = 100`（`internal/service/app_auth.go:63`），不新增 `maxImportDepth`；超限时返回 `too_deep` 并给出路径与实际深度。导出**不做深度裁剪**，原样输出。

**Rationale**：`maxPermissionDepth=100` 在既有代码里同时承担两个角色——父链遍历的防环护栏（`ensureParent` 的 `for depth := 0; depth < maxPermissionDepth`）与隐式的深度上限。既然单条创建允许建到 100 层，那么导入若能导出更深的树、却因"导入专用 10 层"而拒绝回导，就直接违背 spec FR-019 的闭环承诺（US4 的全部价值）。"权限字典应该只有三层"是**命名习惯**问题，应由 `prefix_hint` 这类不阻断的提示引导，不适合做成硬限制——用硬限制换来的是"合法的存量数据搬不走"。

**为什么最初写的 10 层被否**：初版把 spec 的"默认 10 层"当作硬要求，并与既有的 100 层并存；critique 指出这制造了"能导出、导不回"的闭环例外，且 10 层对权限字典没有产品意义。经 2026-11-04 决策改为对齐（spec FR-012 同步改写，US2-AC3 的验收数字由"12 层超 10"改为"101 层超 100"）。

**Alternatives considered**：
- 导入 10 层 + 导出超深拒绝：管理员会看到"请先整理层级"——但这个树是系统允许单条建出来的，等于让用户为工具的不一致买单。
- 导入 10 层 + 导出超深仍允许下载：就是本轮 critique 指出的"静默/事后失败"，已否。
- 把 `maxPermissionDepth` 改成 10：会让既有深树的编辑直接失败（`ensureParent` 返回 `ErrAppPermissionInvalidParent`），属未声明的行为收窄。

---

## R7 · 错误码：新增 2 个，复用 2 个

**Decision**：

| HTTP | code | 用途 | 来源 |
|---|---|---|---|
| 400 | `APP_PERMISSION_IMPORT_INVALID` | 结构/字段/编码规则/跨层重复/超深/超长/空/语法错误 | **新增** |
| 413 | `APP_PERMISSION_IMPORT_TOO_LARGE` | 请求体超 512KB；确认导入时条数超 1000 | **新增** |
| 409 | `APP_RESOURCE_STALE` | 并发冲突（唯一键撞车） | 复用（`internal/api/response/errors.go:39`） |
| 403 | `FORBIDDEN` | 门户非应用管理员 / 应用停用（中间件统一，不区分原因） | 复用（`internal/api/response/errors.go:21`） |
| 404 | `NOT_FOUND` | 应用不存在（admin 端；门户由中间件折叠为 403） | 复用（`internal/api/response/errors.go:22`） |

**Rationale**：spec FR-029 已定稿"两个稳定错误码"，拆分依据是**修法不同**（改内容 vs 裁剪）。命名沿用既有 `APP_PERMISSION_*` 前缀（`APP_PERMISSION_CODE_EXISTS`/`HAS_CHILDREN`/`INVALID_PARENT`）。413 而非 400：请求体超限是传输层语义，`APP_ORG_GRANT_LIMIT` 用 400 是因为它是业务配额（`errors.go:46`），而本特性的体积上限是 payload 层面。两者都必须先登记 AGENTS §5.3 再使用（宪法·技术与接口约束），`scripts/check-registry.sh` 第 2 项会把 `errors.go` 与 §5.3 逐条比对。

**Alternatives considered**：
- 全部用 `VALIDATION_FAILED`：`spec` 明确要求可分类（前端要据 code 决定"提示改内容"还是"提示裁剪"），通用码做不到。
- 拆三个（超条数 / 超体积 / 内容非法）：`TOO_LARGE` 的两种触发对管理员的动作完全相同（裁剪后重试），拆分只增加测试面。
- 并发冲突新造 `APP_PERMISSION_IMPORT_CONFLICT`：既有 `APP_RESOURCE_STALE` 的 message 已是"已被他人修改，请刷新后重试"，正好满足 FR-018，新造码会让前端多一个无差别分支。

**admin 端应用停用的处置（与既有单条 CRUD 不一致，需显式说明）**：既有 admin 端权限 CRUD 只调 `ensureClient`（`internal/service/app_auth.go:660-668`，仅判存在），不判 `status`；门户端由 `middleware.AppAdmin` → `AuthorizeAppAdmin`（`app_auth.go:612-631`）判 `Status != 1` → 403。spec FR-022 要求"应用停用/不存在时导入被拒绝"，因此**仅在导入路径**新增 `ensureClientActive`（存在性 + `status == 1`），admin 侧返回 `403 FORBIDDEN`（附文案"应用已停用，请先启用后再导入"），门户侧不额外改动（中间件已覆盖）。不回头改造既有单条 CRUD——那属于未声明范围，按"规范未覆盖选择改动最小方案"处理并在 PR 说明。

---

## R8 · 审计：1 个新事件类型，成功与失败同 Type、用 `result` 区分

**Decision**：新增 `model.EventAppPermissionsImported = "app_permissions_imported"`（`internal/store/model/audit_event.go`）。

- **成功**：**仅当 `added > 0 || updated > 0`** 写 1 条，`Fields{clientId, added, updated, unchanged, moved, source, total, addedIds, docSha256}`，`Result: "success"`；**全量无变化（`added=0 && updated=0`）不写**。
  - `addedIds`：本次新增权限点的 ID，**超过 20 项只记前 20 个 + `addedCount`**（沿用 AGENTS §6.7 对"删除角色/权限记被解绑 ID 快照"的截断规则，`snapshotIDs` 同款）；用途是"误导入后可据此批量清理"，是 v1 唯一的回滚兜底。
  - `docSha256`：导入文档的 SHA-256（十六进制）。不存原文（否则审计会被 1000 条 JSON 撑爆），但能让"这次导入的是不是这份文件"可验证、可复现。
- **失败**：仅"业务拒绝"写 1 条，`Fields{clientId, reason}`、`Result: "failure"`，`reason ∈ {too_large, stale, client_not_found, client_disabled}`；**内容非法（`APP_PERMISSION_IMPORT_INVALID`）与 401/403 不写**。
- **校验预览**：任何情况都不写（FR-002 / FR-025）。

**Rationale（三条都对着 spec 的硬约束）**：

1. **no-op 不写** —— spec **SC-003**（"不产生新的成功审计事件"）与 **US3 的 Independent Test**（"审计日志没有新增记录"）直接要求；不这么实现这两条验收必失败。既有 010 也是"`unchanged` 跳过不产生事件"（`internal/api/admin/org_grant.go:106-108`）。
2. **内容非法不写失败事件** —— 与 AGENTS §6.7 既有口径一致（`emitOrgGrantFailure` 的注释："输入格式错误与 401/403 不记"）。手滑贴坏 JSON 与校验预览同为"改内容再试"的无后果动作，逐次留痕会淹没审计页，与本特性 FR-002 排除预览的动机相同。
3. **聚合 1 条** —— 审计写入是**非阻塞丢弃队列**（`internal/service/audit.go:111-115`，队列满直接丢弃并 warn），逐条 Emit 1000 次既违反 FR-024，也很可能被丢弃一部分，得到"部分留痕"的更坏结果。故**导入事件的丢弃日志必须带 `event_type`**，让运维能 grep 出"这次导入是否丢了审计"；不为此改同步落库（成本高于收益，记入 plan 的 Operators Notes）。

`source` 由 query 参数 `?source=` 声明（非法值回退 `paste`），**仅供统计，不得作为安全依据**（可任意伪造）。失败 `reason` 只记分类枚举，不记原始 JSON 与逐条 issue，避免把用户内容（可能含内部命名、甚至误粘贴的密钥）写进审计。

**登记三处（缺一即 `make check` 失败）**：① `internal/store/model/audit_event.go` 常量；② AGENTS §6.7 的 `event_type` 枚举**同一行**（`scripts/check-registry.sh` 只解析含"新增需登记"的那一行）；③ AGENTS §6.7 的事件说明段落（写清 `reason` 取值、"no-op 不写"、"内容非法不写"与"预览不写"的规则）。

**Alternatives considered**：
- 两个 Type（`app_permissions_imported` / `app_permissions_import_failed`）：与 010 的既有惯例不一致，且失败原因已由 `result`+`reason` 表达。
- 成功时同时写每条的 `app_permission_created`：直接违反 FR-024。
- 无条件写成功事件（初版方案）：与 SC-003 / US3-IT 冲突，已被 critique 否决。

---

## R9 · 导出契约：统一响应包裹的 JSON 文档，不含内部标识与 `sort`

**Decision**：`GET /api/v1/admin/clients/:id/permissions/export`（及门户同构路径）返回 `200` + 统一响应体，`data` 为：

```json
{
  "version": 1, "mode": "merge",
  "importable": { "ok": true, "reasons": [] },
  "permissions": [ ... 嵌套 children ... ]
}
```

`permissions` 每项含 `code`、`name`、可选 `description`、可选 `children`；**不含** `id`、`parentId`、`sort`、`childCount`、`roleCount`、时间字段。根级与各层子级均按 `(sort, created_at)` 升序排列（与列表端点的 `ORDER BY p.sort ASC, p.created_at ASC` 同口径）。前端拿到 `data` 后 `JSON.stringify({version, mode, permissions}, null, 2)` → `Blob` → `URL.createObjectURL` 触发下载，文件名 `permissions-<clientId>-<yyyyMMdd>.json`。

**导出不得复用 `ListClientPermissions`（关键）**：该查询（`internal/store/app_auth_store.go:527-532`）带 `LIMIT 1000` 与两个计数子查询。复用它会让 >1000 条的应用**静默导出残缺文件**——这是本特性最恶劣的失败形态（管理员以为搬全了，另一环境少了 200 条且无人知晓），且直接违背 FR-019 的"同构"承诺。新增 `ListClientPermissionsForExport`：无 `LIMIT`、无计数子查询（顺带更快）。

**`importable` 判定（一个字段覆盖三个不对称）**：导出侧不设限制（备份与人工参考是正当用途），但导入侧有 1000 条 / 512KB / 100 层三道闸门，因此导出响应必须自证"这份文件能否回导"：

| `reasons` | 判定依据 |
|---|---|
| `empty` | `permissions` 为空（回导必被 `empty` 拒绝，故不能判 `ok=true`） |
| `too_many` | 权限点总数 > 1000 |
| `too_large` | **`{version, mode, permissions}` 以 `json.MarshalIndent(v, "", "  ")` 序列化后的长度 > 512KB**（该字节数必须与前端下载文件逐字节一致） |
| `too_deep` | 树深度 > `maxPermissionDepth`（100） |

**体积判定必须用 pretty 而不是 compact**：前端下载的是 `JSON.stringify(_, null, 2)`，3 层嵌套下缩进/换行开销约 1.2–1.4 倍。若服务端按 compact 判体积，会出现"`ok=true` 但实际文件 >512KB、回导被 413"——`importable` 的全部价值就此作废。因此判定与下载必须共用同一序列化方式，并由 `TestImportableMatchesDownloadBytes` 钉住（R10）。

`ok=false` 时前端在触发下载**之前**提示"该应用权限为空 / 权限点过多 / 体积过大 / 层级过深，导出文件可用于备份，但无法回导"；`too_many` 时附带"列表页最多展示 1000 条"（列表端点既有 `LIMIT 1000`，否则管理员会以为导出错了）。`importable` 与 `version/mode/permissions` 一样**只在响应里**，下载文件不含它（否则回导会被未知字段校验拒绝）。

**Rationale**：统一响应是硬约束（宪法·技术与接口约束：`/api/v1/**` 必须 `{code,message,data,traceId}`）；若导出直接吐文件流，就把 `/api/v1/**` 撕开一个例外，还会让前端的错误处理（`errorCode`/`errorMessage`）在导出失败时失效。前端自己生成下载是浏览器原生能力，仓库虽无现成工具（全库 0 处 `Blob`/`download`），但这是十几行代码，不值得为它引入依赖。

**不含 `sort` 的理由**：`sort` 是库内排序实现细节，导出若带上它，回导时会与"数组顺序"产生两个事实源；不带则"数组顺序即展示顺序"（spec FR-008 的语义），回导后按顺序重排 `0..n`，展示顺序完全一致——闭环成立且文档更干净。

**Alternatives considered**：
- 复用 `ListClientPermissions`（初版方案）：静默截断，已被 critique 否决。
- 超限直接拒绝导出：剥夺"备份/人工参考"的正当用途，且管理员无从得知到底哪一条超限。
- 直接返回 `application/json` 文件流（`Content-Disposition: attachment`）：破坏统一响应，且网关/前端拦截器需要为它开特例。
- 导出携带 `sort`：回导时若"数组顺序"与 `sort` 冲突（用户手工调换了顺序），产生歧义。
- 导出为 `{permissions: flat[]}` 扁平 + `parentCode`：违反 FR-007（层级只允许嵌套）。

---

## R10 · 前端复用与测试策略：两端各一份对话框 + 三层测试

**Decision**：

- **组件**：新增 `PermissionImportDialog.vue`，admin 一份、portal 复制一份（006 既定做法：`web/portal/src/views/app-admin/*` 是 admin 对应页的复制，仅 EP 注册方式与 API 前缀不同）。admin 端 EP 全局安装；portal 端必须逐个 import 组件并 `ElLoading.directive` 局部注册（对齐 `web/portal/src/views/app-admin/Permissions.vue:4-39`）。
- **文件工具**：仓库无任何现成的"读文件/触发下载"工具（全库 0 处 `Blob`/`FileReader`/`download`）。各自工程内新建 `src/utils/file.ts`（`readJsonFile(file)` / `downloadJson(name, obj)`），**不跨工程共享**（006 既定），但每个工程内只写一次，避免对话框里重复实现两端共 4 份。
- **前端只做三件事**：① `file.size > 512KB` 的即时提示；② `JSON.parse` 语法检查 + 空检查（语法错误的**位置**以服务端返回为准，前端只显示"不是合法的 JSON，请先本地校验"）；③ 导出响应 `importable.ok=false` 时在下载前提示"仅可用于备份，无法回导"。语义结论一律显示服务端 `issues`（FR-028）。
- **测试三层**：
  - 纯函数单测 `internal/service/app_permission_import_test.go`：覆盖 SC-002 的 8 类错误样例 + 未知字段 + `version`/`mode` + 大小写疑似重复（仅提示）+ 深度（含 101 层）+ 条数 + 空白 code + `children` 非数组等结构错；用表驱动，无需 DB。
  - 纯函数单测 `TestImportableMatchesDownloadBytes`：固定 30 条样例，断言 ① `importable` 判定所用字节数 **等于** `json.MarshalIndent({version,mode,permissions},"","  ")` 的长度；② 该长度 ≤512KB 时 `ok=true`、>512KB 时 `ok=false`；③ 空文档 `ok=false, reasons=["empty"]`。这条断言把"判定口径 == 用户拿到的文件"变成可回归的约束——没有它，实现按 compact 算、前端按 pretty 下载，单测全绿而线上错。
  - 集成测试 `internal/service/app_permission_import_integration_test.go`（`TEST_MYSQL_DSN`，沿用 `app_auth_integration_test.go` 的 `newAppAuthEnv` 夹具）：幂等重导（SC-003）、导入→导出→导入闭环（SC-004）、父级变更计数与 `movedItems`（US2-AC7）、未出现在输入中的子节点保持原父（边界）、两个 goroutine 并发导入（FR-018），以及**三条审计断言**：`TestImportWritesExactlyOneAuditEvent` / `TestImportPreviewWritesNoAudit` / `TestReimportNoopWritesNoAudit`。
  - handler 层 `internal/api/admin/app_permission_import_test.go`：坏 JSON → `400 APP_PERMISSION_IMPORT_INVALID` 且 service 未被调用；body 超 512KB → `413`；预览返回 `200 + issues`；导出对 >1000 条返回 `importable.ok=false`（可用夹具直接插 1001 行验证）。
  - 前端 `PermissionImportDialog.spec.ts`（两端各一份）：语法错误提示、三态计数与 `movedItems` 渲染、失败后输入保留（FR-030）、导出 `importable.ok=false` 的提示、下载 URL 生成（`URL.createObjectURL` 被调用一次）。

**Rationale**：项目测试惯例是"handler 层轻量（不起 DB）+ service 层真库集成（`TEST_MYSQL_DSN` 门控）+ store 层无单测"（`internal/store` 只有 `filestore_test.go`、`model/*_test.go`）。本特性把最易错的部分（解析与规则）做成纯函数，就能在不依赖 MySQL 的情况下覆盖 SC-002 的 20 份样例——这比在 handler 层拼 JSON 字符串更可靠。前端 vitest 必须两端各写：006 的 `Roles.spec.ts`/`Members.spec.ts` 就是成对存在的，两端断言同一组文案是 SC-007 的最低成本证据。

**Alternatives considered**：
- 抽一个共享组件包：跨两个独立 Vite 工程（各自 `package.json`/`tsconfig`），006 已明确否决，本特性不推翻。
- 只写集成测试：CI 无 MySQL 时会全部 skip，解析规则等于无覆盖。

---

## 遗留与交叉确认

- **无 NEEDS CLARIFICATION 遗留**：10 个决策点均已定稿。
- **2026-11-04 critique 后的两处收敛**（已落进本文件与 spec/plan）：
  1. **R6 改为深度对齐 100 层**：原"导入 10 层 / 既有 100 层并存"的处置会让 >10 层的历史树"能导出、导不回"，与 FR-019 的闭环承诺冲突；改为统一复用 `maxPermissionDepth`，spec FR-012 与 US2-AC3 同步改写（验收数字改为 101 层超 100）。
  2. **导出不再复用列表查询**：`ListClientPermissions` 的 `LIMIT 1000` 会让大应用导出被静默截断；改为 `ListClientPermissionsForExport`（无 `LIMIT`、无计数子查询）+ `importable{ok,reasons[]}` 显式判定。
- **仍在 critique 报告里但已定调的两点**（供实现期回看）：
  - admin 端导入增加"应用停用"校验（R7）与既有 admin 权限 CRUD 不一致——**有意为之**：导入是放大面动作，且 FR-022 是 MUST；不回头改造既有单条 CRUD。
  - 失败审计只记业务拒绝、内容非法不记（R8，spec FR-025 同步改写）——与既有"输入格式错误不记"口径一致。

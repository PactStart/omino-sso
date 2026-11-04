# Quickstart 验证剧本：应用权限点批量导入（011）

**Date**: 2026-11-03 | **Plan**: [plan.md](./plan.md) | **Contracts**: [admin](./contracts/admin-permission-import.md) · [portal](./contracts/portal-permission-import.md)

本剧本用于**实现完成后**端到端自证：每条断言都对应 spec 的一个验收场景或成功标准，失败即视为未完成。所有 `curl` 均针对 `:8081`（管理 API），统一响应里的 `data` 用 `jq '.data'` 取出。

---

## 0. 前置

```bash
docker compose up -d && docker compose ps        # mysql(3307) + redis(6380) 均 healthy
cp .env.example .env && make keys                # 首次
make dev                                         # 前台跑，:8080 协议层 / :8081 管理 API
```

前端（另开终端）：

```bash
cd web/admin  && npm install && npm run dev      # http://localhost:5174
cd web/portal && npm install && npm run dev      # http://localhost:5173
```

登录拿 cookie（管理员来自 `.env` 的 `ADMIN_BOOTSTRAP_*`，默认 `admin` / `Admin@12345`）：

```bash
curl -s -c /tmp/admin.txt -X POST http://localhost:8081/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"Admin@12345"}' | jq -r '.code'
# 期望：OK
```

建一个待测应用：

```bash
CID=$(curl -s -b /tmp/admin.txt -X POST http://localhost:8081/api/v1/admin/clients \
  -H 'Content-Type: application/json' \
  -d '{"clientName":"导入验证","redirectUris":["https://demo.example.com/cb"]}' \
  | jq -r '.data.clientId')
echo "$CID"
```

准备样例文档（3 层 5 条，覆盖 `sort` 省略与 `description`）：

```bash
cat > /tmp/perm-ok.json <<'JSON'
{
  "version": 1,
  "mode": "merge",
  "permissions": [
    { "code": "order", "name": "订单", "description": "订单域", "children": [
        { "code": "order.write", "name": "创建订单", "children": [
            { "code": "order.write.refund", "name": "退款" } ] },
        { "code": "order.read", "name": "查询订单" } ] },
    { "code": "user", "name": "用户" }
  ]
}
JSON
```

---

## 1. 主链路（US1 / US2 / SC-001）

### 1.1 预览不写库

```bash
curl -s -b /tmp/admin.txt -X POST \
  "http://localhost:8081/api/v1/admin/clients/$CID/permissions/import/validate?source=paste" \
  -H 'Content-Type: application/json' \
  --data-binary @/tmp/perm-ok.json | jq '.data'
```

**期望**：HTTP 200，`ok=true`，`added=5, updated=0, unchanged=0, moved=0, total=5`，`issues=[]`。

```bash
curl -s -b /tmp/admin.txt "http://localhost:8081/api/v1/admin/clients/$CID/permissions" | jq '.data.total'
# 期望：0（预览未写入）
```

### 1.2 确认导入

```bash
curl -s -b /tmp/admin.txt -X POST \
  "http://localhost:8081/api/v1/admin/clients/$CID/permissions/import?source=paste" \
  -H 'Content-Type: application/json' \
  --data-binary @/tmp/perm-ok.json | jq '.data'
```

**期望**：200，`added=5, updated=0, unchanged=0, moved=0, total=5, source="paste"`。审计 `detail` 另含 `addedIds`（5 个 ULID）+ `addedCount=5` + `docSha256`（64 位十六进制）。

```bash
curl -s -b /tmp/admin.txt "http://localhost:8081/api/v1/admin/clients/$CID/permissions" \
  | jq -r '.data.list[] | "\(.code)\t\(.parentId // "-")"' | sort
# 期望：5 行，order 与 user 的 parentId 为 "-"；order.write 的 parentId == order 的 id；
#       order.write.refund 的 parentId == order.write 的 id；同级顺序按书写顺序（sort 0,1,2…）
```

### 1.3 树形展示

浏览器打开 `http://localhost:5174/admin/clients/$CID`（权限 tab），确认树呈三层，展开顺序与 JSON 书写顺序一致；列表视图「排序」列为 0/1/2…（`sort` 省略时自动分配，FR-008）。

---

## 2. 幂等与更新（US3 / SC-003）

### 2.1 原样重导

```bash
curl -s -b /tmp/admin.txt -X POST \
  "http://localhost:8081/api/v1/admin/clients/$CID/permissions/import" \
  -H 'Content-Type: application/json' --data-binary @/tmp/perm-ok.json | jq '.data'
```

**期望**：`added=0, updated=0, unchanged=5`。权限点总数仍为 5，且**审计日志不新增** `app_permissions_imported` 记录（SC-003 / US3 独立测试：全 no-op 不写事件）：

```bash
curl -s -b /tmp/admin.txt \
  "http://localhost:8081/api/v1/admin/audit-logs?clientId=$CID&eventType=app_permissions_imported" | jq '.data.total'
# 期望：与重导前一致（本次 +0）
```

### 2.2 改一条名称

```bash
sed 's/"创建订单"/"新建订单"/' /tmp/perm-ok.json > /tmp/perm-edit.json
curl -s -b /tmp/admin.txt -X POST \
  "http://localhost:8081/api/v1/admin/clients/$CID/permissions/import" \
  -H 'Content-Type: application/json' --data-binary @/tmp/perm-edit.json | jq '.data'
# 期望：added=0, updated=1, unchanged=4
```

### 2.3 不删除

从 `/tmp/perm-ok.json` 删掉 `user` 节点后再导入，`user` 必须仍然存在（FR-015）。

---

## 3. 校验失败的可定位性（FR-005 / FR-029 / SC-002）

对每份坏样例，**预览必须 200 + `ok=false` + `issues` 指出位置**；**确认导入必须 400 + `APP_PERMISSION_IMPORT_INVALID` 且 `data` 为 null**。

| # | 样例 | 期望 `issues[].code` / 位置 |
|---|---|---|
| 1 | 空文件 | `empty` |
| 2 | `{"permissions": [` | `syntax_error`，`line` 非空 |
| 3 | 编码 `order write`（空格） | `invalid_code`，`path=permissions[0].code` |
| 4 | 顶层多写 `"id": "01H..."` | `unknown_field`，`path="id"` |
| 5 | `"version": 2` | `bad_version`（文案含"仅支持版本 1"） |
| 6 | `"mode": "replace"` | `bad_mode`（文案含"暂不支持全量对齐"） |
| 7 | 两条 `order.read`（不同父） | `dup_code`，**两个 `path` 都要出现** |
| 8 | 101 层嵌套（超过既有 100 层上限） | `too_deep`，指出超限节点路径与实际深度 |
| 9 | `name` 70 个汉字 | `too_long`，文案含最大 64 |
| 10 | 1001 条 | 预览：`too_many`（含实际 1001 与上限 1000）；确认导入：**413** `APP_PERMISSION_IMPORT_TOO_LARGE` |

```bash
# 例：未知字段
curl -s -b /tmp/admin.txt -X POST \
  "http://localhost:8081/api/v1/admin/clients/$CID/permissions/import/validate" \
  -H 'Content-Type: application/json' \
  -d '{"version":1,"mode":"merge","permissions":[{"code":"a","name":"A","id":"01H"}]}' | jq '.data'
# 期望：ok=false，issues[0].code="unknown_field"，issues[0].path="permissions[0].id"

# 同上内容走确认导入
curl -s -o /tmp/r.json -w '%{http_code}\n' -b /tmp/admin.txt -X POST \
  "http://localhost:8081/api/v1/admin/clients/$CID/permissions/import" \
  -H 'Content-Type: application/json' \
  -d '{"version":1,"mode":"merge","permissions":[{"code":"a","name":"A","id":"01H"}]}'
jq '{code,data}' /tmp/r.json
# 期望：400 / {"code":"APP_PERMISSION_IMPORT_INVALID","data":null}
```

**条数超限**：

```bash
python3 - <<'PY' > /tmp/perm-1001.json
import json
print(json.dumps({"permissions":[{"code":f"p{i}","name":f"P{i}"} for i in range(1001)]}))
PY
# 预览 → 200 + issues[].code=too_many
# 确认导入 → 413 APP_PERMISSION_IMPORT_TOO_LARGE
```

**体积超限（>512KB，无法解析 → 预览也是 413，且提示不声称知道条数）**：

```bash
python3 - <<'PY' > /tmp/perm-big.json
import json
print(json.dumps({"permissions":[{"code":f"p{i}","name":"x"*300} for i in range(2000)]}))
PY
wc -c /tmp/perm-big.json   # 期望 > 524288
curl -s -o /tmp/big.json -w '%{http_code}\n' -b /tmp/admin.txt -X POST \
  "http://localhost:8081/api/v1/admin/clients/$CID/permissions/import/validate" \
  -H 'Content-Type: application/json' --data-binary @/tmp/perm-big.json
# 期望：413，body.code = APP_PERMISSION_IMPORT_TOO_LARGE
```

**警告不阻断**（`warnings`，不影响 `ok`）：

- 子编码不以父编码为前缀（父 `order`、子 `pay.refund`）→ `prefix_hint`；
- 同文档出现 `order.read` 与 `Order.Read` → `case_hint`，且两者都成功导入。

---

## 4. 父级改写（US2-AC7/AC8）

1. 先导入 `/tmp/perm-ok.json`（已有 `order.read` 挂在 `order` 之下）。
2. 把 `order.read` 移到 `user` 的 `children` 下再导入：
   - 预览：`updated=1`、`moved=1`；
   - 确认后：`order.read.parentId == user.id`；
   - 该权限点若已被某角色引用，角色绑定不变（FR-017）：在应用详情 → 角色页确认引用数仍是 1。

3. 反向（子变父、父变子）不应成环：把 `order.write` 提为根、`order` 放进 `order.write.children`，导入成功后树为 `order.write → order`，无环。

---

## 5. 导出 → 回导闭环（US4 / SC-004）

```bash
# 应用 A 导出
curl -s -b /tmp/admin.txt "http://localhost:8081/api/v1/admin/clients/$CID/permissions/export" \
  | jq '.data' > /tmp/export-a.json
jq '{version,mode,importable,n:(.permissions|length)}' /tmp/export-a.json
# 期望：version=1, mode="merge", importable={ok:true,reasons:[]}, n=2（根级 2 个）

# 确认导出文档不含内部标识
grep -E '"(id|parentId|sort|childCount|roleCount|createdAt)"' /tmp/export-a.json && echo '❌ 含内部字段' || echo '✅ 干净'
```

**空应用导出**（下面建的应用 B 此时还没有权限点，正好复用）：

```bash
CID2=$(curl -s -b /tmp/admin.txt -X POST http://localhost:8081/api/v1/admin/clients \
  -H 'Content-Type: application/json' -d '{"clientName":"导入验证B","redirectUris":["https://b.example.com/cb"]}' \
  | jq -r '.data.clientId')
curl -s -b /tmp/admin.txt "http://localhost:8081/api/v1/admin/clients/$CID2/permissions/export" | jq '.data | {permissions, importable}'
# 期望：permissions=[] 且 importable={ok:false, reasons:["empty"]}（空文件导不回去，不能声称可回导）
```

把 `{version, mode, permissions}` 三个字段（去掉 `importable`）作为 body 导入到新应用 B：

```bash
jq '{version,mode,permissions}' /tmp/export-a.json > /tmp/export-a-importable.json
curl -s -b /tmp/admin.txt -X POST "http://localhost:8081/api/v1/admin/clients/$CID2/permissions/import" \
  -H 'Content-Type: application/json' --data-binary @/tmp/export-a-importable.json | jq '.data'
# 期望：added=5, updated=0，A 与 B 的 code 集合与父子结构一致（SC-004）
```

**导出不被列表上限截断（X1 核心断言）**：给应用 B 直插 1200 条权限点（或用集成夹具），再导出：

```bash
# 直插夹具数据（示例：走 mysql 容器）
docker compose exec -T mysql mysql -uroot -proot omino_sso -e "
INSERT INTO client_permissions (id, client_id, parent_id, code, name, sort, created_at, updated_at)
SELECT CONCAT('01H', LPAD(n, 23, '0')), '$CID2', NULL, CONCAT('bulk.', n), CONCAT('批量', n), n, UNIX_TIMESTAMP()*1000, UNIX_TIMESTAMP()*1000
FROM (SELECT 1 n UNION SELECT 2 UNION SELECT 3 /* … 共 1200 行，实际用脚本生成 */) t;"

curl -s -b /tmp/admin.txt "http://localhost:8081/api/v1/admin/clients/$CID2/permissions/export" | jq '.data | {n:(.permissions|length), importable}'
# 期望：n = 1205（1200 条直插 + 前面导入的 5 条；关键是要 ≠1000，证明未复用带 LIMIT 的列表查询）
#       importable={ok:false,reasons:["too_many"]}
```

前端此时点"导出"必须在下载前提示"该应用权限过多（列表页最多展示 1000 条），文件可用于备份但无法回导"，并允许继续下载。

**体积口径必须与下载文件一致（X1 关键断言）**：构造一份"compact 恰好 ~500KB、2 空格缩进后 >512KB"的文档（`name` 用 300 字符 × 1100 条），确认 `importable.reasons` 含 `too_large`；再把该判定结果与 `js` 侧 `JSON.stringify(data, null, 2)` 的字节数对比，二者必须一致：

```bash
jq '{version,mode,permissions}' /tmp/export-big.json > /tmp/dl.json
wc -c < /tmp/dl.json            # 前端下载内容的真实字节数
node -e 'const d=require("/tmp/dl.json");console.log(Buffer.byteLength(JSON.stringify(d,null,2)))'
# 期望：两者相等；且 >524288 时 importable.ok 必须为 false（不得靠 compact 序列化得出 ok=true）
```

对应单测 `TestImportableMatchesDownloadBytes`（纯函数，无需 DB）把这条口径固定下来。

**另两个不可回导的成因**：空应用（`permissions: []` → `reasons:["empty"]`，不能判 `ok=true`）与深度（>100 层 → `too_deep`）。四个原因可同时出现。

---

## 6. 审计（US6 / SC-008）

```bash
curl -s -b /tmp/admin.txt \
  "http://localhost:8081/api/v1/admin/audit-logs?clientId=$CID&eventType=app_permissions_imported&page=1&pageSize=20" \
  | jq '{total:.data.total, first:.data.list[0].detail, result:.data.list[0].result}'
```

**期望**：
- **有实际写入**的确认导入恰好 1 条（1000 条输入也是 1 条，FR-024）；
- **全量无变化**的重复导入 **0 条**（FR-023 / SC-003）；
- `detail` 含 `clientId/added/updated/unchanged/moved/source/total`；
- 连续 5 次**校验预览**（无论通过与否）不产生任何记录（FR-025）；
- **内容非法**（如未知字段、坏 JSON）导致的确认导入失败 **0 条**（FR-025）；
- **业务拒绝**失败 1 条：`result="failure"`、`detail.reason ∈ {too_large, stale, client_not_found, client_disabled}`。

对应集成测试（必须固定，不靠人肉 curl）：`TestImportWritesExactlyOneAuditEvent`、`TestReimportNoopWritesNoAudit`、`TestImportPreviewWritesNoAudit`。

---

## 7. 门户端与权限（US5 / SC-007）

1. 用 `admin` 把 `zhangsan` 加为应用 `$CID` 的成员，并在应用详情把其设为应用管理员（`PUT /admin/clients/:id/members/:uid/admin`，body `{"isAdmin":true}`）。
2. 浏览器登录门户（`http://localhost:5173`），进入「应用管理 → 权限」，对 `$CID` 执行同样的"校验 → 导入"：
   - 计数与 admin 端一致；
   - 权限点立即在 admin 端可见。
3. 用 `lisi`（非该应用管理员）直接调 `POST /api/v1/me/apps/$CID/permissions/import`：
   - **期望 403 `FORBIDDEN`**，且与"应用不存在"的响应**逐字节相同**（不泄露存在性）。
4. 用 `admin` 停用该应用（`PATCH /admin/clients/:id`，`{"status":0}`）后：
   - 门户端导入 → 403（通用文案）；
   - admin 端导入 → 403，`message` 为"应用已停用，请先启用后再导入"。

---

## 8. 前端行为（FR-028 / FR-030 / SC-002）

1. **示例下载（FR-001）**：对话框提供"下载示例"，下载到的文件可原样走"校验 → 导入"成功（证明示例本身合法），且含 3 层嵌套与 `sort` 写法；示例说明里须有"表格行 → 嵌套 JSON"的三行对照（`order` / `order.write` / `order.write.refund` 三行扁平编码即对应三层嵌套），让从 Excel 迁移的人不必自己推导层级。
2. 打开导入对话框，粘贴非法 JSON → 前端立即提示"内容不是合法的 JSON"（`JSON.parse` 失败），不发请求。
3. 粘贴合法但语义非法的内容 → 请求发出，**问题清单来自服务端**；列表展示 `path` + 原因，超过 20 条时显示"共 N 条"。
4. 校验通过后显示三态计数；若 `moved > 0`，**必须展示 `movedItems` 明细**（权限编码 / 原上级 / 新上级，至多 20 条 + "共 N 条"），让管理员在确认前看清"哪几条换爹"（FR-016）。
5. 校验通过后，"确认导入"按钮才可用；导入失败后编辑器内容**必须保留**（不清空）。
6. 选择 >512KB 文件 → 前端立即提示超限（`file.size`），不发请求。
7. 点"确认导入"后若返回计数 ≠ 预览计数 → 提示"数据在预览后有变化，已按最新状态写入"；遇 `413` 提示裁剪并给"重新校验"按钮；遇 `409` 提示"已被他人修改"并自动重跑一次预览。
8. 点"导出" → 浏览器下载 `permissions-<clientId>-<yyyyMMdd>.json`，内容可被"选择文件"重新导入；`importable.ok=false` 时下载前先提示"仅可用于备份，无法回导"。
9. 两端（`/admin/clients/:id` 权限 tab 与门户应用管理权限页）表现一致。

---

## 9. 门禁（提交前必须全绿）

```bash
make verify-fast        # lint + check + test（Go 单测 + 两端 vitest）
make docs               # 重新生成 openapi.json（新增 6 个 operation）
make build              # 含 docs-check（漂移比对）
TEST_MYSQL_DSN='root:root@tcp(127.0.0.1:3307)/omino_sso_test?parseTime=true' make test-integration
```

> ⚠️ `TEST_MYSQL_DSN` 必须指向**专用测试库**：集成测试脚手架会 `DROP DATABASE` 重建 schema（AGENTS §8 记录的 2026-10-22 事故）。指向开发库会清空管理员、组织与审计记录。

**必须同步更新的断言/基线**（漏一处则 `make build` 或 `make check` 红）：

| 位置 | 变更 |
|---|---|
| `internal/api/docs/docs_test.go` | operation 数 84 → 90 |
| `internal/api/docs/openapi.json` | `make docs` 产物，入库 |
| `specs/007-deployment/api-evolve/baseline/manifest.json` | `openapi.json` 新 sha256 |
| `internal/api/router_test.go` | 门户 3 条新路由已注册 |
| `AGENTS.md` | §3 目录树、§4.1（门户 3）、§4.6（admin 3）、§5.3（2 个错误码）、§6.7（1 个事件） |
| `scripts/check-registry.sh` 跑通 | 路由 / 错误码 / 事件 / 变量四向一致 |

#!/usr/bin/env bash
# 010-app-access-org-grant 的端到端断言（本地 test 形态）。
#
# 覆盖：
#   §1 组织授权的新增 / 幂等 / includeSubDepts 调整 / 部门删除保护 / 撤销影响人数
#   §2 员工侧「我的应用」可见性（含子部门 vs 不含子部门，全程不写成员表）—— FR-002、FR-010
#   §3 撤销后手动成员仍可见、角色绑定不变、impactUsers=0 —— FR-008
#   §4 审计按条留痕（批量 3 条 / 更新 from-to / 幂等无事件 / 失败 1 条）—— FR-023、FR-024
#
# 用法：make dev 起服务后 `bash scripts/e2e/org-grant.sh`。
# 退出码：0 全部通过；1 有断言失败；2 前置失败（API 不可达 / 管理员或专用账号登录失败）。
set -uo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$ROOT_DIR" || exit 2

# shellcheck source=lib/admin.sh
. scripts/e2e/lib/admin.sh

api=${E2E_API:-http://localhost:8081}
work=${E2E_WORK_DIR:-.tmp/e2e-examples}
cookie=${E2E_ADMIN_COOKIE:-$work/admin.cookie}
endpoint=${E2E_LOGIN_ENDPOINT:-/api/v1/auth/login}
user_pass=${E2E_EXAMPLE_PASS:-Examples@12345}
suffix=$(date +%s)
org_root="e2e-og-root-$suffix"
org_child="e2e-og-child-$suffix"
org_out="e2e-og-out-$suffix"
app_name="e2e-og-app-$suffix"

# 专用账号固定名字（每次只调整部门，避免在库里堆账号）。
u_sub="e2e-og-sub"
u_root="e2e-og-root"
u_out="e2e-og-out"
jar_sub="$work/og-sub.cookie"
jar_root="$work/og-root.cookie"
jar_out="$work/og-out.cookie"

pass=0
fail=0

ok() { pass=$((pass + 1)); printf '  [PASS] %s\n' "$*"; }
ko() { fail=$((fail + 1)); printf '  [FAIL] %s\n' "$*"; }

# jq_get <json> <filter>
jq_get() { printf '%s' "$1" | jq -r "$2" 2>/dev/null; }

req() { # req <method> <path> [body]
  local method=$1 path=$2 body=${3:-}
  if [[ -n "$body" ]]; then
    curl -sS --max-time 10 -b "$cookie" -X "$method" \
      -H 'Content-Type: application/json' -d "$body" "$api$path"
  else
    curl -sS --max-time 10 -b "$cookie" -X "$method" "$api$path"
  fi
}

req_as() { # req_as <jar> <path>
  curl -sS --max-time 10 -b "$1" "$api$2"
}

# user_upsert <username> <displayName> <deptId> —— 输出 userId（存在则只改部门）。
user_upsert() {
  local name=$1 display=$2 dept=$3 list id body
  list=$(req GET "/api/v1/admin/users?page=1&pageSize=20&keyword=$name")
  id=$(printf '%s' "$list" | jq -r --arg n "$name" '[.data.list[]? | select(.username == $n) | .id] | first // empty' 2>/dev/null)
  if [[ -n "$id" ]]; then
    req PATCH "/api/v1/admin/users/$id" "$(jq -nc --arg d "$dept" '{deptId: $d}')" >/dev/null
    printf '%s' "$id"
    return 0
  fi
  body=$(jq -nc --arg u "$name" --arg d "$display" --arg p "$user_pass" --arg dept "$dept" \
    '{username: $u, displayName: $d, password: $p, deptId: $dept}') || return 1
  id=$(req POST /api/v1/admin/users "$body" | jq -r '.data.id // empty' 2>/dev/null)
  [[ -n "$id" ]] || return 1
  printf '%s' "$id"
}

# user_login <username> <jar> —— 成功 0；登录被拒 2（凭据错了继续跑只会锁号）。
user_login() {
  local name=$1 jar=$2 code body
  body=$(jq -nc --arg u "$name" --arg p "$user_pass" '{username: $u, password: $p}') || return 2
  code=$(curl -sS --max-time 10 -c "$jar" -o /dev/null -w '%{http_code}' \
    -H 'Content-Type: application/json' -d "$body" "$api$endpoint" 2>/dev/null) || return 2
  [[ "$code" == "200" ]] || {
    admin::log "专用账号 $name 登录被拒（HTTP $code），脚本中止以免锁号"
    return 2
  }
  return 0
}

# my_apps_visible <jar> —— 该账号在「我的应用」里能否看到本脚本创建的应用。
# 注意 /me/apps 的 data 是数组（不是分页对象）。
my_apps_visible() {
  req_as "$1" "/api/v1/me/apps" \
    | jq -r --arg id "$app_id" '[.data[]? | select(.clientId == $id)] | length > 0' 2>/dev/null
}

# ---- 审计查询（只统计本脚本创建的应用，避免历史数据干扰）----

audit_count() { # audit_count <eventType> <result>
  req GET "/api/v1/admin/audit-logs?page=1&pageSize=100&eventType=$1&clientId=$app_id" \
    | jq -r --arg et "$1" --arg rs "$2" \
      '[.data.list[]? | select(.eventType == $et and .result == $rs)] | length' 2>/dev/null
}

audit_detail() { # audit_detail <eventType> [result] —— 输出该类型最新一条的 detail JSON
  req GET "/api/v1/admin/audit-logs?page=1&pageSize=100&eventType=$1&clientId=$app_id" \
    | jq -r --arg et "$1" --arg rs "${2:-success}" \
      '[.data.list[]? | select(.eventType == $et and .result == $rs)] | first | (.detail // "{}")' 2>/dev/null
}

# audit_wait <eventType> <result> <want> —— 审计异步落库，轮询到等于 want；超时输出实测值并返回 1。
audit_wait() {
  local et=$1 rs=$2 want=$3 i=0 now=""
  while ((i < 10)); do
    now=$(audit_count "$et" "$rs")
    [[ "$now" == "$want" ]] && { printf '%s' "$now"; return 0; }
    sleep 1
    i=$((i + 1))
  done
  printf '%s' "$now"
  return 1
}

mkdir -p "$work"

echo "[org-grant] 管理员登录"
admin::login || exit 2

echo "[org-grant] 准备组织与应用"
root_json=$(req POST /api/v1/admin/orgs "{\"name\":\"$org_root\",\"parentId\":null,\"sort\":1}")
root_id=$(jq_get "$root_json" '.data.id')
[[ -n $root_id && $root_id != null ]] || { echo "创建根部门失败: $root_json"; exit 2; }

child_json=$(req POST /api/v1/admin/orgs "{\"name\":\"$org_child\",\"parentId\":\"$root_id\",\"sort\":1}")
child_id=$(jq_get "$child_json" '.data.id')
[[ -n $child_id && $child_id != null ]] || { echo "创建子部门失败: $child_json"; exit 2; }

out_json=$(req POST /api/v1/admin/orgs "{\"name\":\"$org_out\",\"parentId\":null,\"sort\":2}")
out_id=$(jq_get "$out_json" '.data.id')
[[ -n $out_id && $out_id != null ]] || { echo "创建树外部门失败: $out_json"; exit 2; }

app_json=$(req POST /api/v1/admin/clients \
  "{\"clientName\":\"$app_name\",\"homeUrl\":\"https://e2e.example.com\",\"redirectUris\":[\"https://e2e.example.com/cb\"]}")
app_id=$(jq_get "$app_json" '.data.clientId')
[[ -n $app_id && $app_id != null ]] || { echo "创建应用失败: $app_json"; exit 2; }

uid_sub=$(user_upsert "$u_sub" "组织授权-子部门" "$child_id") || { echo "创建账号 $u_sub 失败"; exit 2; }
uid_root=$(user_upsert "$u_root" "组织授权-本级" "$root_id") || { echo "创建账号 $u_root 失败"; exit 2; }
uid_out=$(user_upsert "$u_out" "组织授权-树外" "$out_id") || { echo "创建账号 $u_out 失败"; exit 2; }

echo "[org-grant] 专用账号登录"
user_login "$u_sub" "$jar_sub" || exit 2
user_login "$u_root" "$jar_root" || exit 2
user_login "$u_out" "$jar_out" || exit 2

base="/api/v1/admin/clients/$app_id/org-grants"

echo "[org-grant] §1 空列表"
list=$(req GET "$base")
[[ $(jq_get "$list" '.data.total') == "0" ]] && ok "初始无组织授权" || ko "初始应有 0 条授权: $list"

echo "[org-grant] §1 新增（含子部门）"
r=$(req POST "$base" "{\"grants\":[{\"deptId\":\"$root_id\",\"includeSubDepts\":true}]}")
[[ $(jq_get "$r" '.data.added') == "1" ]] && ok "新增 1 条" || ko "新增计数异常: $r"
r=$(req GET "$base")
[[ $(jq_get "$r" '.data.list[0].includeSubDepts') == "true" ]] && ok "落库 includeSubDepts=true" || ko "includeSubDepts 未落库: $r"
[[ -n $(jq_get "$r" '.data.list[0].coveredUsers') ]] && ok "返回在职启用账号数" || ko "缺 coveredUsers: $r"
[[ -n $(jq_get "$r" '.data.list[0].uniqueUsers') ]] && ok "返回撤销影响人数" || ko "缺 uniqueUsers: $r"
[[ $(jq_get "$r" '.data.list[0].deptPath') != "null" ]] && ok "返回部门层级路径" || ko "缺 deptPath: $r"

echo "[org-grant] §2 员工侧可见性（含子部门：子树内可见、树外不可见）"
[[ $(my_apps_visible "$jar_sub") == "true" ]] && ok "子部门账号可见（无需任何成员操作）" || ko "子部门账号应可见该应用"
[[ $(my_apps_visible "$jar_root") == "true" ]] && ok "本级账号可见" || ko "本级账号应可见该应用"
[[ $(my_apps_visible "$jar_out") == "false" ]] && ok "树外账号不可见" || ko "树外账号不应可见该应用"

echo "[org-grant] §1 幂等"
r=$(req POST "$base" "{\"grants\":[{\"deptId\":\"$root_id\",\"includeSubDepts\":true}]}")
[[ $(jq_get "$r" '.data.unchanged') == "1" && $(jq_get "$r" '.data.added') == "0" ]] \
  && ok "重复提交幂等（unchanged=1）" || ko "幂等失效: $r"

echo "[org-grant] §1 调整 includeSubDepts"
r=$(req POST "$base" "{\"grants\":[{\"deptId\":\"$root_id\",\"includeSubDepts\":false}]}")
[[ $(jq_get "$r" '.data.updated') == "1" ]] && ok "调整为不含子部门（updated=1）" || ko "调整未生效: $r"
[[ $(my_apps_visible "$jar_sub") == "false" ]] && ok "不含子部门：子部门账号不可见" || ko "子部门账号不应再可见"
[[ $(my_apps_visible "$jar_root") == "true" ]] && ok "不含子部门：本级账号仍可见" || ko "本级账号应仍可见"

echo "[org-grant] §1 部门删除保护"
del=$(req DELETE "/api/v1/admin/orgs/$root_id")
[[ $(jq_get "$del" '.code') == "ORG_HAS_GRANTS" ]] && ok "被授权引用的部门禁止删除（409）" || ko "删除保护未生效: $del"

echo "[org-grant] §3 手动成员 + 角色绑定（撤销后必须保留）"
r=$(req POST "/api/v1/admin/clients/$app_id/members" "{\"userIds\":[\"$uid_sub\"]}")
[[ $(jq_get "$r" '.code') == "OK" ]] && ok "手动加入子部门账号" || ko "手动加入成员失败: $r"

role_name="ogRole$suffix"
role_id=$(req POST "/api/v1/admin/clients/$app_id/roles" "{\"name\":\"$role_name\"}" | jq -r '.data.id // empty' 2>/dev/null)
[[ -n "$role_id" ]] && ok "创建角色 $role_name" || ko "创建角色失败"

if [[ -n "$role_id" ]]; then
  req PUT "/api/v1/admin/clients/$app_id/roles/$role_id/members" "{\"userIds\":[\"$uid_sub\"]}" >/dev/null
  r=$(req GET "/api/v1/admin/clients/$app_id/members?page=1&pageSize=20")
  rc=$(printf '%s' "$r" | jq -r --arg u "$uid_sub" \
    '[.data.list[]? | select(.username == $u) | .roleCount] | first // empty' 2>/dev/null)
  [[ ${rc:-0} == "1" ]] && ok "角色绑定数 = 1（撤销前基线）" || ko "角色绑定数应为 1，实际 ${rc:-空}: $r"
fi

echo "[org-grant] §3 授权→撤销（手动成员不失去可见性）"
r=$(req POST "$base" "{\"grants\":[{\"deptId\":\"$root_id\",\"includeSubDepts\":true}]}")
[[ $(jq_get "$r" '.data.added') == "1" ]] && ok "重新授权根部门（含子部门）" || ko "重新授权失败: $r"
r=$(req POST "$base/remove" "{\"orgIds\":[\"$root_id\"]}")
[[ $(jq_get "$r" '.data.removed') == "1" ]] && ok "撤销 1 条" || ko "撤销失败: $r"
[[ $(jq_get "$r" '.data.impactUsers') == "0" ]] && ok "impactUsers=0（该账号靠手动成员仍可见）" || ko "impactUsers 应为 0: $r"

r=$(req GET "/api/v1/admin/clients/$app_id/members?page=1&pageSize=20")
still=$(printf '%s' "$r" | jq -r --arg u "$u_sub" \
  '[.data.list[]? | select(.username == $u)] | length' 2>/dev/null)
[[ ${still:-0} == "1" ]] && ok "撤销后仍在成员列表" || ko "撤销后手动成员不应消失: $r"
rc=$(printf '%s' "$r" | jq -r --arg u "$u_sub" \
  '[.data.list[]? | select(.username == $u) | .roleCount] | first // empty' 2>/dev/null)
[[ ${rc:-0} == "1" ]] && ok "撤销后角色绑定数不变（=1）" || ko "角色绑定数被撤销动作改变: ${rc:-空}"
[[ $(my_apps_visible "$jar_sub") == "true" ]] && ok "撤销组织授权后手动成员仍可见该应用" || ko "手动成员被误判为不可见"

r=$(req GET "$base")
[[ $(jq_get "$r" '.data.total') == "0" ]] && ok "撤销后列表为空" || ko "撤销后仍有记录: $r"

echo "[org-grant] §4 审计按条留痕"
g0=$(audit_count app_org_granted success)
u0=$(audit_count app_org_grant_updated success)
f0=$(audit_count app_org_granted failure)
rv0=$(audit_count app_org_grant_revoked success)

r=$(req POST "$base" "{\"grants\":[{\"deptId\":\"$root_id\",\"includeSubDepts\":false},{\"deptId\":\"$child_id\",\"includeSubDepts\":true},{\"deptId\":\"$out_id\",\"includeSubDepts\":false}]}")
[[ $(jq_get "$r" '.data.added') == "3" ]] && ok "批量新增 3 条" || ko "批量新增计数异常: $r"
n=$(audit_wait app_org_granted success $((g0 + 3))) && ok "app_org_granted 恰增 3 条（$n）" || ko "app_org_granted 应增 3 条，实际 $n（基线 $g0）"

detail=$(audit_detail app_org_granted success)
for field in clientId deptName deptPath; do
  v=$(printf '%s' "$detail" | jq -r --arg f "$field" '.[$f] // ""' 2>/dev/null)
  [[ -n "$v" ]] && ok "granted 审计 detail.$field 非空" || ko "granted 审计 detail.$field 为空: $detail"
done
last_uid=$(req GET "/api/v1/admin/audit-logs?page=1&pageSize=1&eventType=app_org_granted&clientId=$app_id" \
  | jq -r '[.data.list[]? | select(.result == "success")] | first | (.userId // "")' 2>/dev/null)
[[ -n "$last_uid" ]] && ok "审计记录操作人 userId" || ko "审计缺 userId"

r=$(req POST "$base" "{\"grants\":[{\"deptId\":\"$root_id\",\"includeSubDepts\":true}]}")
[[ $(jq_get "$r" '.data.updated') == "1" ]] && ok "翻转 includeSubDepts（updated=1）" || ko "翻转未生效: $r"
n=$(audit_wait app_org_grant_updated success $((u0 + 1))) && ok "app_org_grant_updated 恰增 1 条（$n）" || ko "updated 审计条数异常：$n（基线 $u0）"
detail=$(audit_detail app_org_grant_updated success)
from=$(printf '%s' "$detail" | jq -r '.from // "null"' 2>/dev/null)
to=$(printf '%s' "$detail" | jq -r '.to // "null"' 2>/dev/null)
[[ "$from" == "false" && "$to" == "true" ]] && ok "updated 审计 detail.from/to = false→true" || ko "from/to 不正确: $detail"

echo "[org-grant] §4 幂等提交不产生事件"
r=$(req POST "$base" "{\"grants\":[{\"deptId\":\"$root_id\",\"includeSubDepts\":true},{\"deptId\":\"$child_id\",\"includeSubDepts\":true},{\"deptId\":\"$out_id\",\"includeSubDepts\":false}]}")
[[ $(jq_get "$r" '.data.unchanged') == "3" ]] && ok "全部命中 unchanged" || ko "幂等判定异常: $r"
g_before=$(audit_count app_org_granted success)
u_before=$(audit_count app_org_grant_updated success)
sleep 2
g_after=$(audit_count app_org_granted success)
u_after=$(audit_count app_org_grant_updated success)
[[ "$g_after" == "$g_before" && "$u_after" == "$u_before" ]] \
  && ok "幂等提交没有新增审计事件（granted=$g_after updated=$u_after）" \
  || ko "幂等提交产生了审计事件：granted $g_before→$g_after, updated $u_before→$u_after"

echo "[org-grant] §4 失败也留痕（部门不存在）"
ghost="01J9ZZZZZZZZZZZZZZZZZZZZZZ"
r=$(req POST "$base" "{\"grants\":[{\"deptId\":\"$ghost\",\"includeSubDepts\":true}]}")
[[ $(jq_get "$r" '.code') == "VALIDATION_FAILED" ]] && ok "不存在的部门返回 400" || ko "部门不存在应返回 400: $r"
n=$(audit_wait app_org_granted failure $((f0 + 1))) && ok "失败审计恰增 1 条（$n）" || ko "失败审计条数异常：$n（基线 $f0）"
detail=$(audit_detail app_org_granted failure)
reason=$(printf '%s' "$detail" | jq -r '.reason // ""' 2>/dev/null)
[[ "$reason" == "dept_not_found" ]] && ok "失败审计 detail.reason=dept_not_found" || ko "失败原因不正确: $detail"
cid=$(printf '%s' "$detail" | jq -r '.clientId // ""' 2>/dev/null)
[[ "$cid" == "$app_id" ]] && ok "失败审计 detail.clientId 正确" || ko "失败审计 clientId 不正确: $detail"

echo "[org-grant] §4 撤销按条留痕"
r=$(req POST "$base/remove" "{\"orgIds\":[\"$root_id\",\"$out_id\"]}")
[[ $(jq_get "$r" '.data.removed') == "2" ]] && ok "撤销 2 条" || ko "撤销计数异常: $r"
n=$(audit_wait app_org_grant_revoked success $((rv0 + 2))) && ok "app_org_grant_revoked 恰增 2 条（$n）" || ko "撤销审计条数异常：$n（基线 $rv0）"
detail=$(audit_detail app_org_grant_revoked success)
for field in clientId orgId deptName; do
  v=$(printf '%s' "$detail" | jq -r --arg f "$field" '.[$f] // ""' 2>/dev/null)
  [[ -n "$v" ]] && ok "revoked 审计 detail.$field 非空" || ko "revoked 审计 detail.$field 为空: $detail"
done

echo "[org-grant] §1 清理"
req POST "$base/remove" "{\"orgIds\":[\"$child_id\"]}" >/dev/null
r=$(req GET "$base")
[[ $(jq_get "$r" '.data.total') == "0" ]] && ok "清空后列表为空" || ko "仍有残留授权: $r"

del=$(req DELETE "/api/v1/admin/orgs/$child_id")
[[ $(jq_get "$del" '.code') == "OK" ]] && ok "撤销后可删除子部门" || ko "子部门删除失败: $del"
del=$(req DELETE "/api/v1/admin/orgs/$root_id")
[[ $(jq_get "$del" '.code') == "OK" ]] && ok "撤销后可删除根部门" || ko "根部门删除失败: $del"
del=$(req DELETE "/api/v1/admin/orgs/$out_id")
[[ $(jq_get "$del" '.code') == "OK" ]] && ok "可删除树外部门" || ko "树外部门删除失败: $del"

printf '\n[org-grant] 结果：PASS=%d FAIL=%d\n' "$pass" "$fail"
[[ $fail -eq 0 ]] || exit 1

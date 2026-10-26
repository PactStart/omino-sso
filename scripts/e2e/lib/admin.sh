#!/usr/bin/env bash
# 示例端到端脚本的管理员侧辅助（specs/008-integration-examples/tasks.md T004–T005）。
#
# 约定：
#   - 客户端按 clientName 逐字比对复用，缺失才创建；命中则 PATCH redirectUris；
#   - clientSecret 优先复用 .tmp/e2e-examples/clients.env，缺失才轮换（轮换会让手工实例的旧密钥失效）；
#   - 凭据文件 umask 077 + chmod 600；
#   - 专用账号「直接创建、已存在视为成功」，随后登录一次确认，失败即中止且不重试（失败计数会累加）。
#
# 本文件只被 source，不单独执行。函数返回码：0 成功；1 可重试/可诊断失败；2 前置失败（调用方应 exit 2）。

set -uo pipefail

ad_base=${E2E_API:-http://localhost:8081}
ad_work=${E2E_WORK_DIR:-.tmp/e2e-examples}
ad_cookie="$ad_work/admin.cookie"
ad_timeout=${E2E_CURL_TIMEOUT:-10}
ad_clients_env="$ad_work/clients.env"

admin::log() {
  printf '[admin] %s\n' "$*" >&2
}

admin::warn() {
  printf '[admin][warn] %s\n' "$*" >&2
}

# admin::login —— 管理员登录，cookie 写入 $ad_cookie。失败返回 2（凭据问题不是断言失败）。
admin::login() {
  local user=${E2E_ADMIN_USER:-admin}
  local pass=${E2E_ADMIN_PASS:-Admin@12345}
  mkdir -p "$ad_work"

  local body code
  body=$(jq -nc --arg u "$user" --arg p "$pass" '{username:$u,password:$p}') || return 2
  code=$(curl -sS --max-time "$ad_timeout" -c "$ad_cookie" -o "$ad_work/admin-login.json" \
    -w '%{http_code}' -H 'Content-Type: application/json' -d "$body" \
    "$ad_base/api/v1/auth/login" 2>/dev/null) || {
    admin::log "管理员登录请求失败（API 不可达：$ad_base）"
    return 2
  }
  if [[ "$code" != "200" ]]; then
    admin::log "管理员登录被拒（HTTP $code）；检查 E2E_ADMIN_USER/E2E_ADMIN_PASS"
    return 2
  fi
  return 0
}

# admin::client_id_by_name <clientName> —— 逐字比对，找不到输出空行。
admin::client_id_by_name() {
  local name=$1 json
  json=$(curl -sG --max-time "$ad_timeout" -b "$ad_cookie" --data-urlencode "keyword=$name" \
    "$ad_base/api/v1/admin/clients" 2>/dev/null) || return 1
  printf '%s' "$json" | jq -r --arg n "$name" \
    '[.data.list[]? | select(.clientName == $n) | .clientId] | first // empty' 2>/dev/null
}

# admin::create_client <clientName> <redirectUri> —— 输出 "<clientId> <clientSecret>"。
admin::create_client() {
  local name=$1 redirect=$2 body code
  body=$(jq -nc --arg n "$name" --arg r "$redirect" '{
    clientName: $n,
    redirectUris: [$r],
    grantTypes: ["authorization_code", "refresh_token"],
    scopes: ["openid", "profile", "email", "offline_access"],
    isPublic: false
  }') || return 1

  code=$(curl -sS --max-time "$ad_timeout" -b "$ad_cookie" -o "$ad_work/client-create.json" -w '%{http_code}' \
    -H 'Content-Type: application/json' -d "$body" "$ad_base/api/v1/admin/clients" 2>/dev/null) || return 1
  if [[ "$code" != "200" && "$code" != "201" ]]; then
    admin::log "创建客户端「$name」失败（HTTP $code）"
    return 1
  fi
  jq -r '.data | "\(.clientId) \(.clientSecret)"' "$ad_work/client-create.json"
}

# admin::patch_redirect <clientId> <redirectUri>
admin::patch_redirect() {
  local id=$1 redirect=$2 body code
  body=$(jq -nc --arg r "$redirect" '{redirectUris: [$r]}') || return 1
  code=$(curl -sS --max-time "$ad_timeout" -X PATCH -b "$ad_cookie" -o "$ad_work/client-patch.json" \
    -w '%{http_code}' -H 'Content-Type: application/json' -d "$body" \
    "$ad_base/api/v1/admin/clients/$id" 2>/dev/null) || return 1
  if [[ "$code" != "200" ]]; then
    admin::log "更新客户端 $id 的回调失败（HTTP $code）"
    return 1
  fi
  return 0
}

# admin::rotate_secret <clientId> —— 输出新 secret。
admin::rotate_secret() {
  local id=$1 code
  code=$(curl -sS --max-time "$ad_timeout" -X POST -b "$ad_cookie" -o "$ad_work/client-secret.json" \
    -w '%{http_code}' -H 'Content-Type: application/json' -d '{}' \
    "$ad_base/api/v1/admin/clients/$id/secret" 2>/dev/null) || return 1
  if [[ "$code" != "200" && "$code" != "201" ]]; then
    admin::log "轮换客户端 $id 的密钥失败（HTTP $code）"
    return 1
  fi
  jq -r '.data.clientSecret' "$ad_work/client-secret.json"
}

# admin::cache_get <KEY> <FIELD> —— 从 clients.env 读 KEY_FIELD（不存在则空）。
admin::cache_get() {
  local key=$1 field=$2
  [[ -f "$ad_clients_env" ]] || return 0
  sed -n "s/^${key}_${field}=//p" "$ad_clients_env" | head -1
}

# admin::cache_put <KEY> <FIELD> <VALUE> —— 就地更新一行，不丢其他示例的条目。
admin::cache_put() {
  local key=$1 field=$2 value=$3 tmp
  mkdir -p "$ad_work"
  tmp="$ad_clients_env.tmp"
  (
    umask 077
    if [[ -f "$ad_clients_env" ]]; then
      grep -v "^${key}_${field}=" "$ad_clients_env" >"$tmp" || :
    else
      : >"$tmp"
    fi
    printf '%s_%s=%s\n' "$key" "$field" "$value" >>"$tmp"
    mv "$tmp" "$ad_clients_env"
    chmod 600 "$ad_clients_env"
  )
}

# admin::ensure_client <KEY> <clientName> <redirectUri>
# 成功后输出 "<clientId> <clientSecret>"；KEY 用于 clients.env 的变量前缀（GO/JAVA/PY）。
admin::ensure_client() {
  local key=$1 name=$2 redirect=$3
  local cached_id cached_secret id secret

  cached_id=$(admin::cache_get "$key" CLIENT_ID)
  cached_secret=$(admin::cache_get "$key" CLIENT_SECRET)

  if [[ -n "$cached_id" ]] && [[ "$(admin::client_id_by_name "$name")" == "$cached_id" ]]; then
    # 客户端仍在：复用缓存里的密钥，保证手工起的示例不至于因轮换而失效。
    if [[ -n "$cached_secret" ]]; then
      admin::patch_redirect "$cached_id" "$redirect" >/dev/null 2>&1 || true
      admin::cache_put "$key" REDIRECT "$redirect"
      printf '%s %s' "$cached_id" "$cached_secret"
      return 0
    fi
    secret=$(admin::rotate_secret "$cached_id") || return 1
    admin::patch_redirect "$cached_id" "$redirect" >/dev/null 2>&1 || true
    admin::cache_put "$key" CLIENT_SECRET "$secret"
    admin::cache_put "$key" REDIRECT "$redirect"
    printf '%s %s' "$cached_id" "$secret"
    return 0
  fi

  id=$(admin::client_id_by_name "$name")
  if [[ -n "$id" ]]; then
    admin::patch_redirect "$id" "$redirect" || return 1
    secret=$(admin::rotate_secret "$id") || return 1
  else
    local created
    created=$(admin::create_client "$name" "$redirect") || return 1
    id=${created%% *}
    secret=${created#* }
    [[ -n "$id" && -n "$secret" ]] || { admin::log "创建客户端「$name」的响应缺少 clientId/clientSecret"; return 1; }
  fi

  admin::cache_put "$key" CLIENT_ID "$id"
  admin::cache_put "$key" CLIENT_SECRET "$secret"
  admin::cache_put "$key" REDIRECT "$redirect"
  printf '%s %s' "$id" "$secret"
}

# admin::dept_id —— 取组织树第一个部门（创建用户必须有 deptId，且长度固定 26）。
admin::dept_id() {
  local json id
  json=$(curl -s --max-time "$ad_timeout" -b "$ad_cookie" "$ad_base/api/v1/admin/orgs/tree" 2>/dev/null) || return 1
  id=$(printf '%s' "$json" | jq -r '[.data[]? | .. | .id? // empty] | first // empty' 2>/dev/null)
  [[ -n "$id" ]] || return 1
  printf '%s' "$id"
}

# admin::ensure_user <username> <password> <displayName>
# 「已存在」视为成功（同 research D15：直接创建，冲突即存在）。
admin::ensure_user() {
  local user=$1 pass=$2 display=$3 dept body code
  dept=$(admin::dept_id) || { admin::log "取组织树失败，无法确定 deptId"; return 1; }

  body=$(jq -nc --arg u "$user" --arg p "$pass" --arg d "$display" --arg dept "$dept" '{
    username: $u, displayName: $d, password: $p, deptId: $dept
  }') || return 1

  code=$(curl -sS --max-time "$ad_timeout" -b "$ad_cookie" -o "$ad_work/user-create.json" -w '%{http_code}' \
    -H 'Content-Type: application/json' -d "$body" "$ad_base/api/v1/admin/users" 2>/dev/null) || return 1

  if [[ "$code" == "200" || "$code" == "201" ]]; then
    return 0
  fi
  if jq -e '.message? // "" | test("已存在|重复|exist"; "i")' "$ad_work/user-create.json" >/dev/null 2>&1 \
    || jq -e '.code? // "" | test("EXIST"; "i")' "$ad_work/user-create.json" >/dev/null 2>&1; then
    return 0
  fi
  admin::log "创建专用账号 $user 失败（HTTP $code）"
  return 1
}

# admin::verify_login <username> <password> —— 用独立 cookie jar 登录一次确认密码可用。
# 失败返回 2：凭据错了继续跑只会把账号锁死。
admin::verify_login() {
  local user=$1 pass=$2 body code
  body=$(jq -nc --arg u "$user" --arg p "$pass" '{username:$u,password:$p}') || return 2
  code=$(curl -sS --max-time "$ad_timeout" -c "$ad_work/user.cookie" -o "$ad_work/user-login.json" \
    -w '%{http_code}' -H 'Content-Type: application/json' -d "$body" \
    "$ad_base/api/v1/auth/login" 2>/dev/null) || {
    admin::log "专用账号登录请求失败（API 不可达）"
    return 2
  }
  if [[ "$code" != "200" ]]; then
    admin::log "专用账号 $user 登录被拒（HTTP $code）。为免锁号，脚本就此中止、不重试；请校正 E2E_EXAMPLE_PASS"
    return 2
  fi
  return 0
}

# ---- 审计查询（009 T018：断言「凭既有会话完成授权」写进了审计，且能在管理端查到）----

# admin::audit_count <eventType> [clientId] —— 该条件的审计条数（分页 total）；失败输出空。
admin::audit_count() {
  local et=$1 cid=${2:-} url
  url="$ad_base/api/v1/admin/audit-logs?page=1&pageSize=1&eventType=$et"
  [[ -n "$cid" ]] && url="$url&clientId=$cid"
  curl -s --max-time "$ad_timeout" -b "$ad_cookie" "$url" 2>/dev/null \
    | jq -r '.data.total // empty' 2>/dev/null
}

# admin::audit_latest <eventType> [clientId] —— 最新一条输出 "<clientId>\t<detail 原样 JSON 字符串>"。
admin::audit_latest() {
  local et=$1 cid=${2:-} url
  url="$ad_base/api/v1/admin/audit-logs?page=1&pageSize=1&eventType=$et"
  [[ -n "$cid" ]] && url="$url&clientId=$cid"
  curl -s --max-time "$ad_timeout" -b "$ad_cookie" "$url" 2>/dev/null \
    | jq -r '.data.list[0] | "\(.clientId // "")\t\(.detail // "")"' 2>/dev/null
}

# admin::audit_wait_increment <eventType> <clientId> <before> [秒]
# 审计是异步落库的（Auditor 走 channel），所以这里轮询到条数变成 before+1，
# 输出最终条数；超时也输出实测值（调用方据此判 FAIL 并打实际值）。
admin::audit_wait_increment() {
  local et=$1 cid=$2 before=$3 limit=${4:-5} i=0 now=""
  while (( i < limit )); do
    now=$(admin::audit_count "$et" "$cid")
    [[ -n "$now" && "$now" != "$before" ]] && { printf '%s' "$now"; return 0; }
    sleep 1
    i=$((i + 1))
  done
  printf '%s' "$now"
  return 1
}

# admin::audit_wait_total <eventType> <clientId> <期望总数> [秒]
# 轮询到条数等于期望值；超时输出实测值并返回 1（审计消费者是异步的，批量写会滞后）。
admin::audit_wait_total() {
  local et=$1 cid=$2 want=$3 limit=${4:-10} i=0 now=""
  while (( i < limit )); do
    now=$(admin::audit_count "$et" "$cid")
    [[ "$now" == "$want" ]] && { printf '%s' "$now"; return 0; }
    sleep 1
    i=$((i + 1))
  done
  printf '%s' "$now"
  return 1
}

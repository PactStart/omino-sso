#!/usr/bin/env bash
#
# scripts/e2e/run.sh —— 本地 test 形态端到端验证（跨子域 SSO / cookie / 探针 / 可信代理）
#
# 为什么需要它：单测能证明"属性拼对了"，证明不了"四个子域下登录态真的通、真的失效"。
# 本脚本用 *.lvh.me + 自签证书 + nginx 容器复刻 test/prod 的域名拓扑，
# 另起一套后端实例（:9080/:9081，REDIS_DB=1）跑断言，全程不碰 :8080/:8081 的 dev 实例。
#
# 覆盖：① 探针暴露面（sso 域只放 /healthz，/readyz、/api/v1 404）
#       ② cookie 属性（Domain/Secure/HttpOnly/SameSite）
#       ③ 跨子域（api 域签发 → portal 域生效）
#       ④ 登出：清 cookie 带同一 Domain、各域 401
#       ⑤ 改密：清 cookie、旧 sid 401、新密码可登录
#       ⑥ 多跳 XFF：审计 IP 取真实客户端，不取网关
#       ⑦ 验证码重置：保留当前会话、吊销其他设备、不清 cookie（D10 反例）
#       ⑧ OIDC 授权码 + PKCE：未登录授权 → 302 门户登录页 → api 域登录 → 回跳完成授权 → 换 token
#          → sso 登出 → portal 401；含 loop guard（带 portal=1 无会话 → 200 内置页不回头）与
#          「内置页表单 POST 仍能完成授权」的回归断言（009）
#
# 不覆盖：真实 LB / 云 DNS / 网络隔离（api 域内网化）/ 浏览器人工点验。
#
# 前置：docker（需 nginx:1.27-alpine）、curl、openssl、python3；先跑 `make build` 与 `make build-web`。
# 数据写入：会临时改演示账号密码与 portal 客户端回调；退出时 trap 自动还原（幂等）。
# 安全阀：.env 里 DB_HOST 必须是本机，APP_ENV 必须是 prod 以外的值；否则拒绝执行。
#         （确实要对远端库跑时显式设 E2E_ALLOW_REMOTE=1 —— 它会写真实用户密码，慎用。）
#
# 用法：
#   make e2e
#   bash scripts/e2e/run.sh
#   E2E_USER=zhangsan E2E_PASS=Demo@12345 bash scripts/e2e/run.sh
#
# 可覆盖变量：E2E_IDP_ADDR(:9080) E2E_API_ADDR(:9081) E2E_TLS_PORT(8443)
#            E2E_TRUSTED_PROXIES E2E_USER E2E_PASS E2E_ADMIN_USER E2E_ADMIN_PASS
#            E2E_REDIS_CONTAINER(omino-sso-redis) E2E_NGINX_IMAGE(nginx:1.27-alpine)
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
WORK="$ROOT/.tmp/e2e"
CONF_DIR="$ROOT/scripts/e2e/nginx"
BIN="$ROOT/bin/sso"

IDP_ADDR="${E2E_IDP_ADDR:-:9080}"
API_ADDR="${E2E_API_ADDR:-:9081}"
IDP_PORT="${IDP_ADDR##*:}"
API_PORT="${API_ADDR##*:}"
HOST_PORT="${E2E_TLS_PORT:-8443}"
TRUSTED="${E2E_TRUSTED_PROXIES:-127.0.0.1/32,172.17.0.0/16,192.168.65.0/24}"
NGINX_NAME="${E2E_NGINX_NAME:-omino-e2e-nginx}"
NGINX_IMAGE="${E2E_NGINX_IMAGE:-nginx:1.27-alpine}"
REDIS_CONTAINER="${E2E_REDIS_CONTAINER:-omino-sso-redis}"

USER_NAME="${E2E_USER:-zhangsan}"
USER_PASS="${E2E_PASS:-Demo@12345}"
TMP_PASS="${E2E_TMP_PASS:-E2eTemp@2026}"
ADMIN_USER="${E2E_ADMIN_USER:-admin}"
ADMIN_PASS="${E2E_ADMIN_PASS:-Admin@12345}"

# 域名固定 .lvh.me：nginx 配置里的 server_name 是字面量，改域要同步改配置（刻意不做环境变量替换）。
DOMAIN=lvh.me
PT="https://portal.$DOMAIN:$HOST_PORT"
AD="https://admin.$DOMAIN:$HOST_PORT"
SSO="https://sso.$DOMAIN:$HOST_PORT"
API="https://api.$DOMAIN:$HOST_PORT"
# dev 的注册回调（seed 基线）；测试回调用完必须回到它，否则 dev 门户登录会失效。
DEV_REDIRECTS='["http://localhost:5173/callback"]'
E2E_REDIRECT="https://portal.$DOMAIN:$HOST_PORT/callback"

UA='omino-e2e/1.0'
PASS=0
FAIL=0
BACKEND_PID=""
AJAR="$WORK/jar-admin.txt"

# ---------------- 输出与断言 ----------------
section() { printf '\n== %s\n' "$*"; }
ok()  { printf '  [PASS] %s\n' "$*"; PASS=$((PASS + 1)); }
bad() { printf '  [FAIL] %s\n' "$*"; FAIL=$((FAIL + 1)); }
eq()  { if [ "$2" = "$3" ]; then ok "$1"; else bad "$1（got=$2 want=$3）"; fi; }
has() { case "$2" in *"$3"*) ok "$1" ;; *) bad "$1（未包含 $3，实际：$2）" ;; esac; }
hasnot() { case "$2" in *"$3"*) bad "$1（不应包含 $3，实际：$2）" ;; *) ok "$1" ;; esac; }
die() { printf '[e2e] %s\n' "$*" >&2; exit 1; }

# ---------------- 环境检查 ----------------
require_cmd() { command -v "$1" >/dev/null 2>&1 || die "缺少命令 $1"; }
port_busy() {
  if command -v lsof >/dev/null 2>&1; then
    lsof -nP -iTCP:"$1" -sTCP:LISTEN >/dev/null 2>&1
  else
    ss -ltn 2>/dev/null | grep -q ":$1 "
  fi
}

preflight() {
  require_cmd docker
  require_cmd curl
  require_cmd openssl
  require_cmd python3
  docker info --format '{{.ServerVersion}}' >/dev/null 2>&1 || die "docker 守护进程不可用"
  [ -x "$BIN" ] || die "缺少 $BIN，先执行 make build"
  [ -f "$ROOT/web/portal/dist/index.html" ] || die "缺少 web/portal/dist，先执行 make build-web"
  [ -f "$ROOT/web/admin/dist/index.html" ] || die "缺少 web/admin/dist，先执行 make build-web"

  # 数据安全阀：本脚本会改用户密码，绝不能对着非本地库或 prod 环境跑。
  if [ -f "$ROOT/.env" ]; then
    local app_env db_host
    app_env=$(sed -n 's/^APP_ENV=//p' "$ROOT/.env" | tail -1 | tr -d '"\r')
    if [ "$app_env" = "prod" ]; then
      die ".env 的 APP_ENV=prod，拒绝执行（本脚本会写用户密码）"
    fi
    db_host=$(sed -n 's/^DB_HOST=//p' "$ROOT/.env" | tail -1 | tr -d '"\r')
    case "$db_host" in
      '' | 127.0.0.1 | localhost | ::1) ;;
      *)
        if [ "${E2E_ALLOW_REMOTE:-0}" != "1" ]; then
          die "DB_HOST=$db_host 不是本机；确认要写远端库请设 E2E_ALLOW_REMOTE=1"
        fi
        ;;
    esac
  fi

  for p in "$IDP_PORT" "$API_PORT" "$HOST_PORT"; do
    if port_busy "$p"; then
      die "端口 $p 已被占用（本脚本不会杀别人的进程，请先停掉或换 E2E_*_ADDR/E2E_TLS_PORT）"
    fi
  done
}

# ---------------- 起停 ----------------
gen_cert() {
  mkdir -p "$WORK/tls"
  if [ -f "$WORK/tls/lvh.me.crt" ] && [ -f "$WORK/tls/lvh.me.key" ]; then
    return 0
  fi
  openssl req -x509 -newkey rsa:2048 -nodes -days 30 \
    -keyout "$WORK/tls/lvh.me.key" -out "$WORK/tls/lvh.me.crt" \
    -subj "/CN=$DOMAIN" -addext "subjectAltName=DNS:$DOMAIN,DNS:*.$DOMAIN" >/dev/null 2>&1 \
    || die "生成自签证书失败（需要 OpenSSL 1.1.1+ 支持 -addext）"
}

start_backend() {
  # 与 dev 隔离：独立端口 + REDIS_DB=1；通知走 mock（验证码固定 888888，不发真实短信/邮件）。
  # PORTAL_LOGIN_URL：未登录授权落到门户登录页（009）；APP_ENV=test 下必须 https。
  env HTTP_IDP_ADDR="$IDP_ADDR" HTTP_API_ADDR="$API_ADDR" \
    APP_ENV=test COOKIE_DOMAIN=".$DOMAIN" COOKIE_SECURE=true \
    OIDC_ISSUER="$SSO" REDIS_DB=1 TRUSTED_PROXIES="$TRUSTED" \
    PORTAL_LOGIN_URL="$PT/login" \
    MAIL_PROVIDER=mock SMS_PROVIDER=mock \
    nohup "$BIN" >"$WORK/sso.log" 2>&1 &
  BACKEND_PID=$!
  local i
  for i in $(seq 1 40); do
    if [ "$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$API_PORT/readyz" || true)" = "200" ]; then
      return 0
    fi
    sleep 0.25
  done
  tail -20 "$WORK/sso.log" >&2 || true
  die "test 后端未在 10s 内就绪（日志：$WORK/sso.log）"
}

start_nginx() {
  docker rm -f "$NGINX_NAME" >/dev/null 2>&1 || true
  local extra=()
  # Linux 原生 docker 需要显式映射宿主机；Docker Desktop 已内置 host.docker.internal。
  case "$(docker info --format '{{.OperatingSystem}}' 2>/dev/null || true)" in
    *"Docker Desktop"*) ;;
    *) extra=(--add-host=host.docker.internal:host-gateway) ;;
  esac
  docker run -d --name "$NGINX_NAME" \
    -p "127.0.0.1:$HOST_PORT:443" \
    ${extra[@]+"${extra[@]}"} \
    -v "$CONF_DIR:/etc/nginx/conf.d:ro" \
    -v "$WORK/tls:/etc/nginx/tls:ro" \
    -v "$ROOT/web/portal/dist:/srv/portal:ro" \
    -v "$ROOT/web/admin/dist:/srv/admin:ro" \
    "$NGINX_IMAGE" >/dev/null
  local i
  for i in $(seq 1 40); do
    if [ "$(curl -sk -o /dev/null -w '%{http_code}' "$SSO/healthz" || true)" = "200" ]; then
      return 0
    fi
    sleep 0.25
  done
  docker logs "$NGINX_NAME" 2>&1 | tail -20 >&2 || true
  die "nginx 未在 10s 内就绪"
}

stop_all() {
  docker rm -f "$NGINX_NAME" >/dev/null 2>&1 || true
  if [ -n "$BACKEND_PID" ] && kill -0 "$BACKEND_PID" 2>/dev/null; then
    kill "$BACKEND_PID" 2>/dev/null || true
    wait "$BACKEND_PID" 2>/dev/null || true
  fi
}

# ---------------- 数据还原（trap 兜底，幂等） ----------------
login_code() { # $1=jar $2=user $3=pass
  curl -sk -o /dev/null -w '%{http_code}' -c "$1" -A "$UA" -H 'Content-Type: application/json' \
    -d "{\"username\":\"$2\",\"password\":\"$3\"}" "$AD/api/v1/auth/login" || echo 000
}

restore_state() {
  # 1) portal 客户端回调回到 dev 基线（不依赖当前值反推，否则残留会被"还原"成残留）
  if [ -s "$AJAR" ]; then
    curl -sk -o /dev/null -b "$AJAR" -A "$UA" -X PATCH -H 'Content-Type: application/json' \
      -d "{\"redirectUris\":$DEV_REDIRECTS}" "$AD/api/v1/admin/clients/portal" || true
  fi
  # 2) 演示密码若停在测试值则改回；已还原则直接返回（最多 2 次登录，不会触发 5 次锁定）
  local jar="$WORK/jar-restore.txt"
  rm -f "$jar"
  if [ "$(login_code "$jar" "$USER_NAME" "$USER_PASS")" = "200" ]; then
    return 0
  fi
  if [ "$(login_code "$jar" "$USER_NAME" "$TMP_PASS")" != "200" ]; then
    return 0
  fi
  curl -sk -o /dev/null -b "$jar" -A "$UA" -H 'Content-Type: application/json' \
    -d "{\"oldPassword\":\"$TMP_PASS\",\"newPassword\":\"$USER_PASS\"}" "$AD/api/v1/me/password" || true
}

on_exit() {
  local code=$?
  set +e
  if [ "$FAIL" -gt 0 ] || [ "$code" -ne 0 ]; then
    restore_state
    printf '\n[e2e] 失败诊断（后端日志尾部）：\n'
    tail -15 "$WORK/sso.log" 2>/dev/null || true
  fi
  stop_all
  printf '\n[e2e] 日志：%s（后端 %s / nginx 已停）\n' "$WORK/sso.log" "$BIN"
  exit "$code"
}

# 起始状态自愈：上一次中断可能把测试密码留在库里、把测试回调留在 client 上，
# 那会让后续每次运行"从第 1 节就开始错"，且错误信息指向下游。开工前先拉回基线。
ensure_baseline() {
  section "准备：账号与客户端回调基线"
  local jar="$WORK/jar-baseline.txt" uris
  rm -f "$jar"
  if [ "$(login_code "$jar" "$USER_NAME" "$USER_PASS")" = "200" ]; then
    ok "演示账号可登录（$USER_NAME）"
  elif [ "$(login_code "$jar" "$USER_NAME" "$TMP_PASS")" = "200" ]; then
    curl -sk -o /dev/null -b "$jar" -A "$UA" -H 'Content-Type: application/json' \
      -d "{\"oldPassword\":\"$TMP_PASS\",\"newPassword\":\"$USER_PASS\"}" \
      "$AD/api/v1/me/password" || true
    if [ "$(login_code "$jar" "$USER_NAME" "$USER_PASS")" = "200" ]; then
      ok "上次运行残留的测试密码已拉回演示值"
    else
      bad "残留测试密码无法拉回演示值"
    fi
  else
    bad "演示账号登录失败：核对 E2E_USER / E2E_PASS 与 .env 种子值"
  fi

  rm -f "$AJAR"
  if [ "$(login_code "$AJAR" "$ADMIN_USER" "$ADMIN_PASS")" != "200" ]; then
    bad "管理员登录失败：核对 E2E_ADMIN_USER / E2E_ADMIN_PASS（默认取 .env 的 ADMIN_BOOTSTRAP_*）"
    return 0
  fi
  uris=$(curl -sk -b "$AJAR" -A "$UA" "$AD/api/v1/admin/clients/portal" \
    | python3 -c 'import sys,json;print(json.dumps(json.load(sys.stdin)["data"]["redirectUris"]))' 2>/dev/null || true)
  case "$uris" in
    *"$DOMAIN"*)
      curl -sk -o /dev/null -b "$AJAR" -A "$UA" -X PATCH -H 'Content-Type: application/json' \
        -d "{\"redirectUris\":$DEV_REDIRECTS}" "$AD/api/v1/admin/clients/portal" || true
      ok "上次运行残留的测试回调已移除"
      ;;
    *) ok "客户端回调为 dev 基线" ;;
  esac
}

# ---------------- 业务断言 ----------------
http_code() { curl -sk -o /dev/null -w '%{http_code}' -A "$UA" "$@" || echo 000; }

clear_resend_limit() {
  # 手工重跑时 60s 重发限制会让验证码步骤失败；只清 test 实例专用 Redis DB1 的 key。
  command -v docker >/dev/null 2>&1 || return 0
  docker ps --format '{{.Names}}' 2>/dev/null | grep -qx "$REDIS_CONTAINER" || return 0
  docker exec "$REDIS_CONTAINER" redis-cli -n 1 --scan \
    --pattern 'verify:resend:find_password:email:*' 2>/dev/null \
    | while IFS= read -r k; do
        [ -n "$k" ] && docker exec "$REDIS_CONTAINER" redis-cli -n 1 del "$k" >/dev/null 2>&1
      done
  return 0
}

check_exposure() {
  section "0. 暴露面与静态站点"
  eq "sso 域 /healthz 200" "$(http_code "$SSO/healthz")" "200"
  eq "sso 域 /readyz 404（公网不可见）" "$(http_code "$SSO/readyz")" "404"
  eq "sso 域 /api/v1/me 404（协议域不暴露管理 API）" "$(http_code "$SSO/api/v1/me")" "404"
  eq "portal 静态页 200" "$(http_code "$PT/")" "200"
  # admin 路由挂根路径：/users 必须由 SPA fallback 命中 index.html（与 dev 5174 行为一致）
  eq "admin 静态页 200" "$(http_code "$AD/")" "200"
  eq "admin /users 200（根路径路由，非 /admin/users）" "$(http_code "$AD/users")" "200"
  eq "后端 :$API_PORT 直连 /readyz 200（内网可达）" \
    "$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$API_PORT/readyz" || echo 000)" "200"
}

check_login_cookie() {
  section "1. 登录下发共享 cookie"
  local jar="$WORK/jar-user.txt" sc code
  rm -f "$jar"
  code=$(curl -sk -D "$WORK/h-login.txt" -o /dev/null -w '%{http_code}' -c "$jar" -A "$UA" \
    -H 'Content-Type: application/json' \
    -d "{\"username\":\"$USER_NAME\",\"password\":\"$USER_PASS\"}" "$AD/api/v1/auth/login" || printf '000')
  eq "登录 200" "$code" "200"
  sc=$(grep -i '^set-cookie: sid=' "$WORK/h-login.txt" | tr -d '\r' || true)
  printf '    %s\n' "$sc"
  # Go 按 RFC 6265 §4.1.2.3 省略 Domain 的前导点：配置 .lvh.me → 输出 lvh.me（语义等价）。
  has "cookie 带 Domain=$DOMAIN" "$sc" "Domain=$DOMAIN"
  has "cookie 带 HttpOnly" "$sc" "HttpOnly"
  has "cookie 带 Secure" "$sc" "Secure"
  has "cookie 带 SameSite=Lax" "$sc" "SameSite=Lax"
}

check_cross_subdomain() {
  section "2. 跨子域：api 域签发 → portal 域生效"
  local jar="$WORK/jar-user.txt"
  eq "portal 域 /api/v1/me 200" "$(http_code -b "$jar" "$PT/api/v1/me")" "200"
  eq "api 域 /api/v1/me 200" "$(http_code -b "$jar" "$API/api/v1/me")" "200"
}

check_logout() {
  section "3. 登出：清 cookie 必须带同一 Domain"
  local jar="$WORK/jar-user.txt" sc
  sc=$(curl -sk -D - -o /dev/null -b "$jar" -c "$jar" -A "$UA" -X POST "$AD/api/v1/auth/logout" \
    | grep -i '^set-cookie: sid=' | tr -d '\r' || true)
  printf '    %s\n' "$sc"
  has "清除带 Domain=$DOMAIN" "$sc" "Domain=$DOMAIN"
  has "Max-Age=0（立即失效）" "$sc" "Max-Age=0"
  eq "登出后 portal 域 401" "$(http_code -b "$jar" "$PT/api/v1/me")" "401"
  eq "登出后 admin 域 401" "$(http_code -b "$jar" "$AD/api/v1/me")" "401"
}

check_change_password() {
  section "4. 改密：清 cookie + 全会话失效"
  local jar="$WORK/jar-user.txt" sc code
  rm -f "$jar"
  login_code "$jar" "$USER_NAME" "$USER_PASS" >/dev/null
  code=$(curl -sk -D "$WORK/h-pwd.txt" -o /dev/null -b "$jar" -c "$jar" -A "$UA" \
    -H 'Content-Type: application/json' \
    -d "{\"oldPassword\":\"$USER_PASS\",\"newPassword\":\"$TMP_PASS\"}" \
    "$AD/api/v1/me/password" -w '%{http_code}' || printf '000')
  eq "改密 200" "$code" "200"
  sc=$(grep -i '^set-cookie: sid=' "$WORK/h-pwd.txt" | tr -d '\r' || true)
  printf '    %s\n' "$sc"
  has "改密清 cookie 带 Domain=$DOMAIN" "$sc" "Domain=$DOMAIN"
  eq "改密后旧 sid 在 portal 域 401" "$(http_code -b "$jar" "$PT/api/v1/me")" "401"
  eq "新密码可登录" "$(login_code "$jar" "$USER_NAME" "$TMP_PASS")" "200"
  code=$(curl -sk -o /dev/null -b "$jar" -c "$jar" -A "$UA" -H 'Content-Type: application/json' \
    -d "{\"oldPassword\":\"$TMP_PASS\",\"newPassword\":\"$USER_PASS\"}" \
    "$AD/api/v1/me/password" -w '%{http_code}' || printf '000')
  eq "密码已还原为演示值" "$code" "200"
}

check_trusted_proxy() {
  section "5. 多跳 XFF：审计 IP 取真实客户端而非网关"
  local jar="$WORK/jar-user.txt" ip
  rm -f "$jar" "$AJAR"
  # 注入一个"外层 LB 写入的真实客户端 IP"，nginx 会在其后追加自己看到的来源。
  curl -sk -o /dev/null -c "$jar" -A "$UA" -H 'X-Forwarded-For: 198.51.100.7' \
    -H 'Content-Type: application/json' \
    -d "{\"username\":\"$USER_NAME\",\"password\":\"$USER_PASS\"}" "$AD/api/v1/auth/login" || true
  login_code "$AJAR" "$ADMIN_USER" "$ADMIN_PASS" >/dev/null
  # 取该用户最新一条 login_success 的 ip。注意：未注入 XFF 的请求落成网关地址是正确的
  # （网关就是直连来源）；这条用例只断言"可伪造的左端值不会被采信" —— 注入值必须胜出。
  ip=$(curl -sk -b "$AJAR" -A "$UA" "$AD/api/v1/admin/audit-logs?eventType=login_success&pageSize=20" \
    | python3 -c 'import sys,json
items=json.load(sys.stdin)["data"]["list"]
print(next((i["ip"] for i in items if i.get("username")==sys.argv[1]), ""))' "$USER_NAME" 2>/dev/null || true)
  printf '    %s 最新 login_success 的 ip: %s\n' "$USER_NAME" "$ip"
  eq "审计 IP 取注入的真实客户端 198.51.100.7" "$ip" "198.51.100.7"
}

check_reset_by_code() {
  section "6. 验证码重置（D10 反例）：保留当前会话、吊销其他设备、不清 cookie"
  local a="$WORK/jar-a.txt" b="$WORK/jar-b.txt" code
  rm -f "$a" "$b"
  clear_resend_limit
  login_code "$a" "$USER_NAME" "$USER_PASS" >/dev/null
  login_code "$b" "$USER_NAME" "$USER_PASS" >/dev/null
  code=$(curl -sk -o /dev/null -w '%{http_code}' -b "$a" -A "$UA" -H 'Content-Type: application/json' \
    -d '{"scene":"find_password","channel":"email"}' "$AD/api/v1/auth/code")
  if [ "$code" != "200" ]; then
    bad "已登录发码 200（实际 $code；429 多为 60s 重发限制且本机无 $REDIS_CONTAINER 可清）"
    return 0
  fi
  ok "已登录发码 200（免图形码）"
  code=$(curl -sk -D "$WORK/h-reset.txt" -o /dev/null -b "$a" -c "$a" -A "$UA" \
    -H 'Content-Type: application/json' \
    -d "{\"channel\":\"email\",\"code\":\"888888\",\"newPassword\":\"$TMP_PASS\"}" \
    "$AD/api/v1/me/password/reset" -w '%{http_code}' || printf '000')
  eq "验证码重置 200（mock 验证码 888888）" "$code" "200"
  if grep -qi '^set-cookie: sid=' "$WORK/h-reset.txt"; then
    bad "重置响应不得清 cookie（与改密是两条不同契约）"
  else
    ok "重置响应未清 cookie"
  fi
  eq "重置后当前设备仍 200" "$(http_code -b "$a" "$PT/api/v1/me")" "200"
  eq "重置后其他设备 401" "$(http_code -b "$b" "$PT/api/v1/me")" "401"
  code=$(curl -sk -o /dev/null -b "$a" -A "$UA" -H 'Content-Type: application/json' \
    -d "{\"oldPassword\":\"$TMP_PASS\",\"newPassword\":\"$USER_PASS\"}" \
    "$AD/api/v1/me/password" -w '%{http_code}' || printf '000')
  eq "密码已还原为演示值" "$code" "200"
}

# ---------------- 009：门户跳转相关小工具 ----------------
ms() { python3 -c 'import time;print(int(time.time()*1000))'; }

# loc_of —— 从 curl -D - 的头块里取首个 Location（去 CRLF）
loc_of() { tr -d '\r' | awk 'tolower($1)=="location:"{print $2; exit}'; }

# url_query <url> <名> —— 查询参数解码**一次**（next 是被编码的完整地址，app 是中文应用名）
url_query() {
  python3 - "$1" "$2" <<'PY'
import sys, urllib.parse

p = urllib.parse.urlsplit(sys.argv[1])
print(urllib.parse.parse_qs(p.query).get(sys.argv[2], [""])[0])
PY
}

# url_base <url> —— scheme://host/path（比对落点用，忽略查询串）
url_base() {
  python3 - "$1" <<'PY'
import sys, urllib.parse

p = urllib.parse.urlsplit(sys.argv[1])
print(f"{p.scheme}://{p.netloc}{p.path}")
PY
}

# authorize_url <state> <code_challenge> —— portal 客户端的 S256 授权请求
authorize_url() {
  printf '%s/oauth2/authorize?client_id=portal&response_type=code&scope=openid%%20profile%%20email&redirect_uri=%s&state=%s&code_challenge=%s&code_challenge_method=S256' \
    "$SSO" \
    "$(python3 -c 'import urllib.parse,sys;print(urllib.parse.quote(sys.argv[1],safe=""))' "$E2E_REDIRECT")" \
    "$1" "$2"
}

check_oidc_flow() {
  section "7. 未登录授权落门户 → api 域登录 → 回跳完成授权（含 loop guard）"
  local sj="$WORK/jar-sso.txt" verifier challenge loc loc2 next app code ocode tok now t0 hops uris
  local jar_empty="$WORK/jar-empty.txt" jar_form="$WORK/jar-form.txt" ncode nid floc payload

  curl -sk -o /dev/null -b "$AJAR" -A "$UA" -X PATCH -H 'Content-Type: application/json' \
    -d "{\"redirectUris\":[\"http://localhost:5173/callback\",\"$E2E_REDIRECT\"]}" \
    "$AD/api/v1/admin/clients/portal" || true
  verifier=$(python3 -c 'import secrets;print(secrets.token_urlsafe(48)[:64])')
  challenge=$(python3 -c "import hashlib,base64,sys;print(base64.urlsafe_b64encode(hashlib.sha256(sys.argv[1].encode()).digest()).rstrip(b'=').decode())" "$verifier")

  # ① 空 jar 授权：authorize 的第一跳固定是协议库的 /login?id=（不是落点），第二跳才由
  #    本次改造决定——必须落到门户登录页，且 next 指回 sso 域登录入口。
  rm -f "$jar_empty"
  loc=$(curl -sk -o /dev/null -D - -c "$jar_empty" -A "$UA" "$(authorize_url e2e "$challenge")" | loc_of || true)
  has "authorize 先回 sso 登录入口" "$loc" "/login?id="
  loc2=$(curl -sk -o /dev/null -D - -b "$jar_empty" -A "$UA" "$loc" | loc_of || true)
  eq "未登录授权 302 到门户登录页" "$(url_base "$loc2")" "$PT/login"
  next=$(url_query "$loc2" next)
  app=$(url_query "$loc2" app)
  has "next 指回 sso 域登录入口" "$next" "$SSO/login?id="
  has "next 带 portal 标记" "$next" "&portal=1"
  if [ -n "$app" ]; then ok "门户登录页带应用名（app=$app）"; else bad "门户登录页缺少 app 参数"; fi
  eq "门户登录页可达" "$(http_code "$PT/login")" "200"

  # ② 真实用户路径：api 域登录建立共享会话 → 回到 next → sso 域两跳 → portal 回调。
  #    计时口径（contracts/e2e-a18-a19.md §3）：起点是登录接口 200，终点是指向 SP 回调的 302。
  sj="$WORK/jar-sso.txt"
  rm -f "$sj"
  # 请求体先落到变量：`-d "{\"a\":1}"` 写在命令替换里会被 bash 当成花括号展开，
  # 拆成两个词发给服务端（实测得到 VALIDATION_FAILED），必须先 printf 出来再引用。
  payload=$(printf '{"username":"%s","password":"%s"}' "$USER_NAME" "$USER_PASS")
  eq "api 域登录 200" "$(curl -sk -o "$WORK/api-login.json" -w '%{http_code}' -c "$sj" -A "$UA" \
      -H 'Content-Type: application/json' -d "$payload" \
      "$API/api/v1/auth/login" || echo 000)" "200"
  if grep -qi "$DOMAIN" "$sj"; then
    ok "api 域登录下发共享 cookie（Domain=$DOMAIN）"
  else
    # 只打印 jar 的「域|名字」结构，绝不打 cookie 值（sid 等同凭据）。
    bad "api 域登录未下发共享 cookie（jar: $(awk -F'\t' 'NF>=7{printf "%s|%s ", $1, $6}' "$sj" 2>/dev/null | tr -d '\r')；mfaRequired=$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1])).get("data",{}).get("mfaRequired"))' "$WORK/api-login.json" 2>/dev/null || echo '?'))"
  fi

  t0=$(ms)
  loc2=$(curl -sk -o /dev/null -D - -b "$sj" -c "$sj" -A "$UA" "$next" | loc_of || true)
  hops=1
  case "$loc2" in
    *"authorize/callback"*)
      loc2=$(curl -sk -o /dev/null -D - -b "$sj" -c "$sj" -A "$UA" "$SSO$loc2" | loc_of || true)
      hops=2
      ;;
  esac
  now=$(ms)
  has "回跳后落到 portal 回调（带 code）" "$loc2" "callback?code="
  eq "门户登录 → SP 回调跳转数 ≤2" "$([ "$hops" -le 2 ] && echo 1 || echo 0)" "1"
  eq "门户登录 200 → SP 回调 302 ≤3s" "$([ $((now - t0)) -le 3000 ] && echo 1 || echo 0)" "1"
  printf '    [timing] 门户登录 200 → SP 回调 302：%sms / %s 跳\n' "$((now - t0))" "$hops"
  eq "协议域登录后 portal 域 /me 200（SSO 生效）" "$(http_code -b "$sj" "$PT/api/v1/me")" "200"

  ocode=$(printf '%s' "$loc2" | sed -n 's/.*[?&]code=\([^&]*\).*/\1/p')
  code=$(curl -sk -o "$WORK/token.json" -w '%{http_code}' -A "$UA" -X POST \
    -d 'grant_type=authorization_code' --data-urlencode "code=$ocode" \
    --data-urlencode "redirect_uri=$E2E_REDIRECT" -d 'client_id=portal' \
    --data-urlencode "code_verifier=$verifier" "$SSO/oauth2/token" || printf '000')
  eq "token 端点 200" "$code" "200"
  tok=$(python3 -c 'import json;print(json.load(open("'"$WORK"'/token.json")).get("access_token",""))' 2>/dev/null || true)
  if [ -n "$tok" ]; then ok "返回 access_token"; else bad "未返回 access_token"; fi

  # ③ loop guard：另起一个仍有效的请求，带 portal=1 但无会话 → 200 内置页，不再 302 回门户。
  #    复用已完成的 next 会命中「请求已过期」，所以这里重新授权一次拿新的 id。
  rm -f "$jar_empty"
  loc=$(curl -sk -o /dev/null -D - -c "$jar_empty" -A "$UA" "$(authorize_url e2e-lg "$challenge")" | loc_of || true)
  loc2=$(curl -sk -o /dev/null -D - -b "$jar_empty" -A "$UA" "$loc" | loc_of || true)
  next=$(url_query "$loc2" next)
  rm -f "$jar_form"
  ncode=$(curl -sk -o "$WORK/loopguard.html" -w '%{http_code}' -c "$jar_form" -b "$jar_form" -A "$UA" "$next" || echo 000)
  eq "loop guard：带 portal=1 无会话 → 200（不 302 回门户）" "$ncode" "200"
  has "loop guard 页面含内置表单" "$(cat "$WORK/loopguard.html" 2>/dev/null || true)" "<form"
  has "loop guard 页面含降级说明" "$(cat "$WORK/loopguard.html" 2>/dev/null || true)" "未能在跳转中延续"

  # ④ 保留一条表单 POST 登录断言：内置页确实能完成授权，而不只是「看起来像表单」。
  nid=$(printf '%s' "$next" | sed -n 's/.*[?&]id=\([^&]*\).*/\1/p')
  floc=$(curl -sk -o /dev/null -D - -c "$jar_form" -b "$jar_form" -A "$UA" -X POST \
    --data-urlencode "id=$nid" --data-urlencode "username=$USER_NAME" --data-urlencode "password=$USER_PASS" \
    "$SSO/login" | loc_of || true)
  case "$floc" in
    *"authorize/callback"*)
      floc=$(curl -sk -o /dev/null -D - -b "$jar_form" -c "$jar_form" -A "$UA" "$SSO$floc" | loc_of || true)
      ;;
  esac
  has "内置页表单 POST 仍能完成授权" "$floc" "callback?code="

  # ⑤ 登出：sso 域清 cookie 带同一 Domain，portal 域随即 401
  loc=$(curl -sk -D - -o /dev/null -b "$sj" -A "$UA" "$SSO/logout" | grep -i '^set-cookie: sid=' | tr -d '\r' || true)
  printf '    %s\n' "$loc"
  has "sso 登出清 cookie 带 Domain=$DOMAIN" "$loc" "Domain=$DOMAIN"
  eq "sso 登出后 portal 域 401" "$(http_code -b "$sj" "$PT/api/v1/me")" "401"

  curl -sk -o /dev/null -b "$AJAR" -A "$UA" -X PATCH -H 'Content-Type: application/json' \
    -d "{\"redirectUris\":$DEV_REDIRECTS}" "$AD/api/v1/admin/clients/portal" || true
  uris=$(curl -sk -b "$AJAR" -A "$UA" "$AD/api/v1/admin/clients/portal" \
    | python3 -c 'import sys,json;print(json.dumps(json.load(sys.stdin)["data"]["redirectUris"]))' 2>/dev/null || true)
  eq "portal 回调地址已还原为 dev 基线" "$uris" "$DEV_REDIRECTS"
}
# ---------------- 主流程 ----------------
main() {
  mkdir -p "$WORK"
  preflight
  gen_cert
  trap on_exit EXIT INT TERM

  printf '[e2e] 域名 %s → 127.0.0.1:%s；后端 %s / %s（REDIS_DB=1）；nginx=%s\n' \
    "$DOMAIN" "$HOST_PORT" "$IDP_ADDR" "$API_ADDR" "$NGINX_IMAGE"
  start_backend
  start_nginx

  ensure_baseline
  check_exposure
  check_login_cookie
  check_cross_subdomain
  check_logout
  check_change_password
  check_trusted_proxy
  check_reset_by_code
  check_oidc_flow

  # 正常收尾也要还原（on_exit 只在失败时兜底调用）
  restore_state

  printf '\n[e2e] 结果：PASS=%d FAIL=%d\n' "$PASS" "$FAIL"
  [ "$FAIL" -eq 0 ]
}

main "$@"

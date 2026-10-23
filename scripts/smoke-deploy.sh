#!/usr/bin/env bash
# 部署冒烟（只读，无副作用）——对应 specs/007-deployment/tasks.md T045 与 quickstart §3。
#
# 用法：
#   DOMAIN=example.com ./scripts/smoke-deploy.sh
#   DOMAIN=example-test.com INSECURE=1 ./scripts/smoke-deploy.sh     # 自签证书环境
#
# 断言（全部只读，不写库、不登录、不改配置）：
#   1. sso 域 /healthz 200（存活探针公网唯一放行项）
#   2. sso 域 /readyz 404（FR-015：依赖探针不得暴露到公网）
#   3. 公网访问 api 域连不通（SC-007；**必须在公网机器执行**，内网跳板机跑会假红）
#   4. api 域 /docs/ 在容器内打 :8081 也是 404（prod 由后端自行 404）
#   5. portal/admin 深链刷新 200 且 text/html + Cache-Control: no-store
#   6. 指纹资源 Cache-Control 含 immutable
#   7. portal 同源反代 /api/v1/me 未带 cookie 返回 401（证明反代链路通了，而不是把前端当后端）
#   8. portal/admin 域 /readyz 404（FR-015：静态站点的 SPA fallback 会把任意路径兜成 200 的
#      index.html，监控会误判"依赖已就绪"；靠 nginx 的 `location = /readyz { return 404; }`）
#
# 可选环境变量：
#   DOMAIN       主域（必填，默认 example.com）
#   SCHEME       默认 https
#   DEEP_LINK    admin 深链（默认 /clients/smoke/members，SPA fallback 必须 200）
#   CURL_TIMEOUT 单次请求超时秒（默认 10）
#   INSECURE=1   跳过证书校验（自签环境）
#   SKIP_INTERNAL=1  跳过第 4 条容器内检查（无 docker / 无编排文件时）
#
# 退出码：0 全部通过（含 skip）；1 存在失败；2 前置条件不满足。
set -uo pipefail

DOMAIN="${DOMAIN:-example.com}"
SCHEME="${SCHEME:-https}"
DEEP_LINK="${DEEP_LINK:-/clients/smoke/members}"
CURL_TIMEOUT="${CURL_TIMEOUT:-10}"
[[ -n "${DOMAIN:-}" ]] || { echo "smoke-deploy: 请指定 DOMAIN=<主域>" >&2; exit 2; }
command -v curl >/dev/null || { echo "smoke-deploy: 缺少 curl" >&2; exit 2; }

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT" || exit 2

PORTAL="${SCHEME}://portal.${DOMAIN}"
ADMIN="${SCHEME}://admin.${DOMAIN}"
SSO="${SCHEME}://sso.${DOMAIN}"
API="${API_SCHEME:-http}://api.${DOMAIN}"

CURL_OPTS=(-sS --max-time "$CURL_TIMEOUT")
[[ "${INSECURE:-0}" == "1" ]] && CURL_OPTS+=(-k)

fails=0
pass() { printf '  [ok]   %s\n' "$1"; }
fail() { printf '  [fail] %s（%s）\n' "$1" "${2:-}"; fails=$((fails + 1)); }
skip() { printf '  [skip] %s（%s）\n' "$1" "${2:-}"; }

# status <url> —— 打印 HTTP 状态码；连不通时打印 000（curl 非零退出）
status() { curl "${CURL_OPTS[@]}" -o /dev/null -w '%{http_code}' "$1" 2>/dev/null || echo 000; }
# headers <url> —— 打印响应头
headers() { curl "${CURL_OPTS[@]}" -D - -o /dev/null "$1" 2>/dev/null; }

expect_status() { # expect_status <说明> <url> <期望码>
	local code
	code="$(status "$2")"
	if [[ "$code" == "$3" ]]; then pass "$1 → $code"; else fail "$1" "期望 $3，实际 $code（$2）"; fi
}

expect_header() { # expect_header <说明> <url> <头部正则>
	local h
	h="$(headers "$2")"
	if printf '%s' "$h" | grep -qiE "$3"; then pass "$1"; else fail "$1" "响应头无 $3（$2）"; fi
}

printf '== 探针（sso 域）==\n'
expect_status "sso /healthz" "$SSO/healthz" 200
expect_status "sso /readyz 公网不可见" "$SSO/readyz" 404

printf '== 暴露面（api 域）==\n'
if curl "${CURL_OPTS[@]}" -o /dev/null "$API/api/v1/me" 2>/dev/null; then
	fail "api 域公网可达" "安全事故：DNS/安全组未做内网限制（见 contracts/nginx-routing.md §3）"
else
	pass "api 域公网连不通（$API）"
fi

if [[ "${SKIP_INTERNAL:-0}" == "1" ]]; then
	skip "容器内 /docs/ 404" "SKIP_INTERNAL=1"
elif ! command -v docker >/dev/null; then
	skip "容器内 /docs/ 404" "本机无 docker"
elif [[ ! -f deploy/compose.prod.yml ]]; then
	skip "容器内 /docs/ 404" "缺少 deploy/compose.prod.yml"
else
	# prod 由后端按 APP_ENV 自行 404，这里验证"不依赖 nginx 兜底也能 404"
	code="$(docker compose -f deploy/compose.prod.yml exec -T sso \
		wget -q -S -O /dev/null "http://127.0.0.1:8081/docs/" 2>&1 | grep -oE 'HTTP/1\.[0-9] [0-9]{3}' | head -1 | awk '{print $2}')"
	if [[ "$code" == "404" ]]; then pass "容器内 :8081/docs/ → 404"; else fail "容器内 :8081/docs/" "期望 404，实际 ${code:-无响应}"; fi
fi

printf '== 就绪探针不得外泄（portal / admin 域）==\n'
# 静态站点的 SPA fallback 会把任意路径兜成 200 的 index.html；少了 nginx 的
# `location = /readyz { return 404; }`，监控就会把"能返回 HTML"当成"依赖已就绪"。
expect_status "portal /readyz 公网不可见" "$PORTAL/readyz" 404
expect_status "admin /readyz 公网不可见" "$ADMIN/readyz" 404

printf '== 深链与缓存（admin 域）==\n'
expect_status "admin 深链 $DEEP_LINK" "$ADMIN$DEEP_LINK" 200
expect_header "深链响应为 HTML 且不缓存" "$ADMIN$DEEP_LINK" '^content-type:[[:space:]]*text/html'
expect_header "深链 Cache-Control: no-store" "$ADMIN$DEEP_LINK" '^cache-control:[[:space:]]*no-store'
expect_header "index.html 不缓存" "$ADMIN/index.html" '^cache-control:[[:space:]]*no-store'

asset="$(curl "${CURL_OPTS[@]}" "$ADMIN/" 2>/dev/null | grep -oE 'assets/[^"]+\.js' | head -1)"
if [[ -z "$asset" ]]; then
	fail "指纹资源" "首页未引用 assets/*.js（页面异常或产物未部署）"
else
	expect_header "指纹资源 immutable（/$asset）" "$ADMIN/$asset" '^cache-control:.*immutable'
fi

printf '== 同源反代（portal 域）==\n'
expect_status "portal /api/v1/me 未带 cookie" "$PORTAL/api/v1/me" 401

printf '== 收敛 ==\n'
if [[ "$fails" -eq 0 ]]; then
	echo "  全部通过"
	exit 0
fi
echo "  $fails 项失败"
exit 1

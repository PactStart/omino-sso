#!/usr/bin/env bash
# 串环境检查（只读）——对应 specs/007-deployment/tasks.md T037。
#
# 前端产物是按环境构建的（同一 commit + 不同 VITE_*），最容易出的错是
# "测试镜像里带着生产域名"或反之：页面能开，点「去后台」却跳到另一套环境。
# 本脚本直接读**镜像内**产物（镜像才是最终事实，宿主机 dist 可能过期）。
#
# 用法（两个镜像 + 两个主域）：
#   PORTAL_IMAGE=registry.example.com/omino-portal:2026.10.23-1 \
#   ADMIN_IMAGE=registry.example.com/omino-admin:2026.10.23-1 \
#   EXPECT_DOMAIN=example.com FOREIGN_DOMAIN=example-test.com \
#   ./scripts/check-build-env.sh
#
# 断言（对 admin.<主域> / portal.<主域> 这两个**入口地址**做断言，而不是裸主域）：
#   1. 本环境入口必须出现：portal 镜像含 `admin.<EXPECT>`（「去后台」）；
#      admin 镜像含 `portal.<EXPECT>`（「返回门户」/会话失效回跳）。
#      0 次说明构建参数没传进去，产物退化成本地缺省值。
#   2. 另一环境入口必须 0 次：`admin.<FOREIGN>`、`portal.<FOREIGN>`。
#      注意只查裸主域会误报——源码里有 `//app.example.com/cb` 这类文档示例串，
#      它和"跳错环境"无关，所以判定必须带 `admin.` / `portal.` 前缀。
#   3. 可选：EXPECT_SITE_NAME 至少出现 1 次，FOREIGN_SITE_NAME 出现 0 次。
#
# 未提供 PORTAL_IMAGE / ADMIN_IMAGE 时打印 skip 并以 0 退出，
# 使 `make check` 在无镜像、无 docker 的开发机上仍可执行。
#
# 退出码：0 通过（含 skip）；1 断言失败；2 前置条件不满足（docker 不可用、镜像不存在）。
set -uo pipefail

EXPECT_DOMAIN="${EXPECT_DOMAIN:-}"
FOREIGN_DOMAIN="${FOREIGN_DOMAIN:-}"
EXPECT_SITE_NAME="${EXPECT_SITE_NAME:-}"
FOREIGN_SITE_NAME="${FOREIGN_SITE_NAME:-}"
PORTAL_IMAGE="${PORTAL_IMAGE:-}"
ADMIN_IMAGE="${ADMIN_IMAGE:-}"

if [[ -z "${PORTAL_IMAGE}" && -z "${ADMIN_IMAGE}" ]]; then
	echo "check-build-env: skip（未提供 PORTAL_IMAGE/ADMIN_IMAGE）"
	exit 0
fi
[[ -n "${EXPECT_DOMAIN}" && -n "${FOREIGN_DOMAIN}" ]] || {
	echo "check-build-env: 需要 EXPECT_DOMAIN 与 FOREIGN_DOMAIN（本环境 / 另一环境主域）" >&2
	exit 2
}
command -v docker >/dev/null || { echo "check-build-env: 缺少 docker，无法读取镜像内产物" >&2; exit 2; }

fails=0
fail() { printf '  [fail] %s\n' "$1"; fails=$((fails + 1)); }
ok() { printf '  [ok]   %s\n' "$1"; }

# count_in_image <镜像> <字面串> —— 统计产物里出现次数
count_in_image() {
	local image="$1" needle="$2"
	docker run --rm --entrypoint sh "$image" -c \
		"grep -rF '$needle' /usr/share/nginx/html/assets/ 2>/dev/null | wc -l" 2>/dev/null || echo 0
}

expect_present() { # expect_present <镜像> <角色> <字面串>
	local n
	n="$(count_in_image "$1" "$3")"
	if [[ "${n:-0}" -gt 0 ]]; then
		ok "$2：$3 出现 $n 处"
	else
		fail "$2：产物内不含 $3（构建参数未注入？）"
	fi
}

expect_absent() { # expect_absent <镜像> <角色> <字面串>
	local n
	n="$(count_in_image "$1" "$3")"
	if [[ "${n:-0}" -eq 0 ]]; then
		ok "$2：不含另一环境入口 $3"
	else
		fail "$2：出现 $3 共 $n 处（串环境）"
	fi
}

check_image() { # check_image <镜像> <角色> <本环境必含项...>
	local image="$1" role="$2"
	shift 2
	if ! docker image inspect "$image" >/dev/null 2>&1; then
		echo "check-build-env: 镜像不存在：$image" >&2
		exit 2
	fi
	local p
	for p in "$@"; do
		expect_present "$image" "$role" "$p"
	done
	expect_absent "$image" "$role" "admin.$FOREIGN_DOMAIN"
	# 009：统一身份登录域也进产物（VITE_SSO_BASE_URL），串环境会让未登录 SSO 跳到另一套 IdP
	expect_absent "$image" "$role" "sso.$FOREIGN_DOMAIN"
	expect_absent "$image" "$role" "portal.$FOREIGN_DOMAIN"

	if [[ -n "${EXPECT_SITE_NAME}" ]]; then
		expect_present "$image" "$role" "$EXPECT_SITE_NAME"
	fi
	if [[ -n "${FOREIGN_SITE_NAME}" ]]; then
		expect_absent "$image" "$role" "$FOREIGN_SITE_NAME"
	fi
}

# 各镜像真正会落进产物的入口（2026-10-24 实测）：
#   portal 只用 VITE_ADMIN_HOME（「去后台」），故以 admin.<主域> 为锚点；
#   admin 只用 VITE_PORTAL_HOME / VITE_PORTAL_LOGIN_URL（返回门户、会话失效回跳），故以 portal.<主域> 为锚点。
[[ -n "${PORTAL_IMAGE}" ]] && check_image "$PORTAL_IMAGE" portal "admin.$EXPECT_DOMAIN" "sso.$EXPECT_DOMAIN"
[[ -n "${ADMIN_IMAGE}" ]] && check_image "$ADMIN_IMAGE" admin "portal.$EXPECT_DOMAIN"

if [[ "$fails" -eq 0 ]]; then
	echo "check-build-env: 通过（$EXPECT_DOMAIN ↔ $FOREIGN_DOMAIN 未串环境）"
	exit 0
fi
echo "check-build-env: $fails 项失败"
exit 1

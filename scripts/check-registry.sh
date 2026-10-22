#!/usr/bin/env bash
# 注册表一致性门禁（2026-10-22 复盘新增）。
#
# 背景：005 的 6 轮 converge 里有 12/27 条是「代码与 AGENTS 登记表不同步」，
# 属于确定性检查，不该靠人工审计发现。本脚本把这类检查前移到提交前：
#
#   1. 路由        internal/api/router.go        ⊆ AGENTS.md §4.1 接口总览
#   2. 错误码      internal/api/response/errors.go ⊆ AGENTS.md §5.3 错误码表
#   3. 审计事件    internal/store/model/audit_event.go = AGENTS.md §6.7 枚举
#   4. 环境变量    internal/config/config.go
#                  ↔ .env.example
#                  ↔ AGENTS.md §9 环境变量清单
#
# 只做「代码 → 登记表」与「.env.example ↔ config」两个方向的比对，
# 不做语义校验（HTTP 状态码、默认值是否一致由 code review 负责）。
#
# 退出码：0 一致；1 存在漂移；2 前置条件不满足（文件缺失等）。
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT" || exit 2

AGENTS="AGENTS.md"
ROUTER="internal/api/router.go"
ERRORS="internal/api/response/errors.go"
EVENTS="internal/store/model/audit_event.go"
CONFIG="internal/config/config.go"
ENV_EXAMPLE=".env.example"

for f in "$AGENTS" "$ROUTER" "$ERRORS" "$EVENTS" "$CONFIG" "$ENV_EXAMPLE"; do
	[[ -f "$f" ]] || { echo "check-registry: 缺少文件 $f" >&2; exit 2; }
done

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

fail=0
report() { # report <类别> <说明>
	printf '  [fail] %s %s\n' "$1" "$2"
	fail=$((fail + 1))
}

# ---------------------------------------------------------------------------
# 通用：从 AGENTS.md 中截取某个章节的正文（到下一个同级或更高级标题为止）
#   section <标题正则>
# ---------------------------------------------------------------------------
section() {
	perl -ne '
		BEGIN { $pat = shift @ARGV; }
		if (!$started && /$pat/) { $started = 1; next }
		if ($started) {
			if (/^#{1,4} /) { exit }
			print
		}
	' "$1" "$AGENTS"
}

# ---------------------------------------------------------------------------
# 1. 路由：解析 Group 嵌套拼出完整路径，再逐条比对 §4.1
# ---------------------------------------------------------------------------
perl -0777 -ne '
	my @lines = split /\n/, $_;
	my %prefix = ("r" => "");
	for my $l (@lines) {
		$prefix{$1} = ($prefix{$2} // "") . $3 if $l =~ /^\s*(\w+)\s*:=\s*(\w+)\.Group\("([^"]*)"\)/;
	}
	for my $l (@lines) {
		next unless $l =~ /^\s*(\w+)\.(GET|POST|PUT|DELETE|PATCH)\("([^"]+)"/;
		printf "%s %s%s\n", $2, ($prefix{$1} // ""), $3;
	}
' "$ROUTER" | sort -u >"$TMP/routes.code"

# 路由登记分散在两处：§4.1 是「方法 | 路径」两列总览，§4.6 是「`METHOD /path`」单元格。
# 只比对 /api/v1 前缀：/oauth2/* 与 /.well-known/* 由 internal/oidc 的 http.Server 直接挂载。
{
	section '^### 4\.1 ' |
		perl -ne 'print "$1 $2\n" if /^\|\s*(GET|POST|PUT|DELETE|PATCH)\s*\|\s*`?([^`|]+?)`?\s*\|/'
	# §4.6 的单元格允许合并方法写法，如 `GET/PATCH/DELETE /api/v1/...`
	section '^### 4\.6 ' |
		perl -ne '
			next unless /^\|\s*`([^`]+)`/;
			my ($methods, $path) = split /\s+/, $1, 2;
			next unless defined $path && $path =~ m{^/};
			for my $m (split m{/}, $methods) {
				print "$m $path\n" if $m =~ /^(GET|POST|PUT|PATCH|DELETE)$/;
			}'
} | awk '$2 ~ /^\/api\/v1(\/|$)/' | sort -u >"$TMP/routes.doc"

awk '$2 ~ /^\/api\/v1(\/|$)/' "$TMP/routes.code" | sort -u >"$TMP/routes.code.api"
mv "$TMP/routes.code.api" "$TMP/routes.code"

routes_code=$(wc -l <"$TMP/routes.code" | tr -d ' ')
routes_doc=$(wc -l <"$TMP/routes.doc" | tr -d ' ')

while read -r method path; do
	[[ -n "$method" ]] || continue
	grep -qxF "$method $path" "$TMP/routes.doc" ||
		report "路由" "$method $path 未在 AGENTS.md §4.1 登记"
done <"$TMP/routes.code"

while read -r method path; do
	[[ -n "$method" ]] || continue
	grep -qxF "$method $path" "$TMP/routes.code" ||
		report "路由" "AGENTS.md §4.1 的 $method $path 在 router.go 中不存在"
done <"$TMP/routes.doc"

# ---------------------------------------------------------------------------
# 2. 错误码：errors.go 的 &Error{..., "CODE", ...} 必须出现在 §5.3
# ---------------------------------------------------------------------------
perl -0777 -ne 'print "$1\n" while /&Error\{[^}]*?"([A-Z][A-Z0-9_]+)"/gs' "$ERRORS" |
	sort -u >"$TMP/codes.code"

section '^### 5\.3 ' |
	perl -ne 'print "$1\n" if /^\|\s*\d{3}\s*\|\s*`([A-Z][A-Z0-9_]+)`/' |
	sort -u >"$TMP/codes.doc"

codes_code=$(wc -l <"$TMP/codes.code" | tr -d ' ')
codes_doc=$(wc -l <"$TMP/codes.doc" | tr -d ' ')

while read -r code; do
	[[ -n "$code" ]] || continue
	grep -qxF "$code" "$TMP/codes.doc" ||
		report "错误码" "$code 未在 AGENTS.md §5.3 登记"
done <"$TMP/codes.code"

while read -r code; do
	[[ -n "$code" ]] || continue
	grep -qxF "$code" "$TMP/codes.code" ||
		report "错误码" "AGENTS.md §5.3 的 $code 在 errors.go 中不存在"
done <"$TMP/codes.doc"

# ---------------------------------------------------------------------------
# 3. 审计事件：audit_event.go 的常量值必须出现在 §6.7 的枚举行
# ---------------------------------------------------------------------------
perl -0777 -ne 'print "$1\n" while /Event\w+\s*=\s*"([a-z][a-z0-9_]+)"/g' "$EVENTS" |
	sort -u >"$TMP/events.code"

# 只取「`event_type` 枚举（新增需登记）：…」这一行，避免把列名（id/ip/…）误当事件。
section '^### 6\.7 ' |
	perl -ne '
		next unless /新增需登记/;
		s/^.*?：//;                       # 丢掉「`event_type` 枚举（新增需登记）：」前缀
		print "$1\n" while /`([a-z][a-z0-9_]+)`/g;' |
	sort -u >"$TMP/events.doc"

events_code=$(wc -l <"$TMP/events.code" | tr -d ' ')
events_doc=$(wc -l <"$TMP/events.doc" | tr -d ' ')

while read -r ev; do
	[[ -n "$ev" ]] || continue
	grep -qxF "$ev" "$TMP/events.doc" ||
		report "审计事件" "$ev 未在 AGENTS.md §6.7 枚举中登记"
done <"$TMP/events.code"

while read -r ev; do
	[[ -n "$ev" ]] || continue
	grep -qxF "$ev" "$TMP/events.code" ||
		report "审计事件" "AGENTS.md §6.7 的 $ev 在 audit_event.go 中不存在"
done <"$TMP/events.doc"

# ---------------------------------------------------------------------------
# 4. 环境变量：config.go 读取键 ↔ .env.example ↔ §9 三向一致
# ---------------------------------------------------------------------------
perl -0777 -ne 'print "$1\n" while /\benv(?:Int|Bool|Duration)?\("([A-Z][A-Z0-9_]+)"/g' "$CONFIG" |
	sort -u >"$TMP/env.code"

perl -ne 'print "$1\n" if /^([A-Z][A-Z0-9_]*)=/' "$ENV_EXAMPLE" |
	sort -u >"$TMP/env.example"

section '^## 9\. ' |
	perl -ne 'print "$1\n" if /^\|\s*`([A-Z][A-Z0-9_]+)`/' |
	sort -u >"$TMP/env.doc"

env_code=$(wc -l <"$TMP/env.code" | tr -d ' ')
env_example=$(wc -l <"$TMP/env.example" | tr -d ' ')
env_doc=$(wc -l <"$TMP/env.doc" | tr -d ' ')

while read -r k; do
	[[ -n "$k" ]] || continue
	grep -qxF "$k" "$TMP/env.example" ||
		report "环境变量" "$k 被 config.go 读取但未写入 .env.example"
	grep -qxF "$k" "$TMP/env.doc" ||
		report "环境变量" "$k 被 config.go 读取但未在 AGENTS.md §9 登记"
done <"$TMP/env.code"

while read -r k; do
	[[ -n "$k" ]] || continue
	grep -qxF "$k" "$TMP/env.code" ||
		report "环境变量" ".env.example 的 $k 未被 config.go 任何 env*() 读取（疑似废弃）"
done <"$TMP/env.example"

while read -r k; do
	[[ -n "$k" ]] || continue
	grep -qxF "$k" "$TMP/env.code" ||
		report "环境变量" "AGENTS.md §9 的 $k 未被 config.go 读取（疑似废弃）"
done <"$TMP/env.doc"

# ---------------------------------------------------------------------------
# 汇总
# ---------------------------------------------------------------------------
printf 'check-registry: 路由 %s/%s 条、错误码 %s/%s 个、审计事件 %s/%s 个、环境变量 %s(代码)/%s(example)/%s(AGENTS)\n' \
	"$routes_code" "$routes_doc" "$codes_code" "$codes_doc" "$events_code" "$events_doc" "$env_code" "$env_example" "$env_doc"

if [[ "$fail" -gt 0 ]]; then
	printf 'check-registry: %d 处漂移\n' "$fail"
	exit 1
fi
echo "check-registry: 注册表一致"

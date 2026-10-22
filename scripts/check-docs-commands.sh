#!/usr/bin/env bash
# 文档命令可执行性门禁（2026-10-22 复盘新增）。
#
# 背景：005 的 quickstart 教用户执行 `make migrate`，而 Makefile 只有
# `migrate-up` / `migrate-down`，照文档操作直接 `No rule to make target`。
# 这类「文档写了不存在的命令」属于确定性检查，交给脚本而非人工审计。
#
# 规则：扫描 AGENTS.md / README.md / docs/**.md / specs/**.md 中的
#   ① 行内代码 `make <target>`
#   ② 代码块中以 `$ make <target>` 或 `make <target>` 开头的行
# 对每个 target 执行 `make -n <target>`，目标不存在即失败。
#
# 例外：行内出现 `docs-commands:ignore` 的行整体跳过（用于「描述历史缺陷」的文字，
# 例如复盘记录里必须原样引用已废弃的 make 目标）。
#
# 退出码：0 全部可执行；1 存在未知目标；2 前置条件不满足。
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT" || exit 2

[[ -f Makefile ]] || { echo "check-docs-commands: 缺少 Makefile" >&2; exit 2; }

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# 收集待扫描文档（只扫仓库内的过程文档，不扫 node_modules / dist）
: >"$TMP/files"
[[ -f AGENTS.md ]] && echo "AGENTS.md" >>"$TMP/files"
[[ -f README.md ]] && echo "README.md" >>"$TMP/files"
for d in docs specs; do
	[[ -d "$d" ]] || continue
	find "$d" -type f -name '*.md' \
		-not -path '*/node_modules/*' -not -path '*/dist/*' >>"$TMP/files"
done

if [[ ! -s "$TMP/files" ]]; then
	echo "check-docs-commands: 未找到待扫描文档"
	exit 0
fi

# 提取 make 目标：<文件>:<行号>:<target>
: >"$TMP/refs"
while IFS= read -r f; do
	perl -ne '
		next if /docs-commands:ignore/;
		while (/`make\s+([A-Za-z][A-Za-z0-9_-]*)`/g) { print "$ARGV:$.:$1\n" }
		print "$ARGV:$.:$1\n" if /^\s*(?:\$\s+)?make\s+([A-Za-z][A-Za-z0-9_-]*)\b/;
	' "$f" >>"$TMP/refs"
done <"$TMP/files"

if [[ ! -s "$TMP/refs" ]]; then
	echo "check-docs-commands: 文档中未引用任何 make 目标"
	exit 0
fi

# 逐目标校验（同一目标只跑一次）
awk -F: '{ print $3 }' "$TMP/refs" | sort -u >"$TMP/targets"
checked=0
fail=0
while IFS= read -r target; do
	[[ -n "$target" ]] || continue
	checked=$((checked + 1))
	if ! make -n "$target" >/dev/null 2>&1; then
		fail=$((fail + 1))
		echo "  [fail] make $target 不存在（Makefile 无此目标）"
		awk -F: -v t="$target" '$3 == t { printf "         被引用: %s:%s\n", $1, $2 }' "$TMP/refs"
	fi
done <"$TMP/targets"

printf 'check-docs-commands: 校验 %d 个目标、%d 处引用\n' "$checked" "$(wc -l <"$TMP/refs" | tr -d ' ')"
if [[ "$fail" -gt 0 ]]; then
	printf 'check-docs-commands: %d 个目标不可执行\n' "$fail"
	exit 1
fi
echo "check-docs-commands: 文档命令全部可执行"

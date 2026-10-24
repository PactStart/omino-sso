#!/usr/bin/env bash
# 制品引用门禁（2026-10-24 复盘新增）。
#
# 背景：007 的 4 轮 converge 里有 3–4 条 finding 是「任务声称写了某个测试/脚本，
# 文件根本不存在」——T051 声称 `internal/api/me/profile_test.go` 已存在（应锁住会话失效），
# 实际直到 Phase 10 的 T065 才真正落盘。这类漂移是**确定性可检查项**，
# 不该靠事后人工审计（更不该靠第 N+1 轮 converge）发现，因此前移到 `make check`：
#
#   1. AGENTS.md §3 目录树：树里出现的每个文件/目录必须真实存在
#      （被 .gitignore 排除的条目如 configs/dev-signing-key.pem、.tmp/ 跳过）
#   2. specs/*/tasks.md 的 `DoD:` 行：行内引用的仓库路径必须存在
#      —— 新任务格式（AGENTS §7.4）要求每条任务的完成证据是一条可执行的 DoD，
#      DoD 里点名的文件就是"这项任务到底交付了什么"的硬锚点
#   3. specs/*/tasks.md 正文里的**路径限定**引用（`*_test.go` / `scripts/*`）必须存在
#      历史遗留的失效引用登记在 LEGACY_ALLOW；登记项一旦真实存在，脚本会提示删除，
#      避免这块清单变成永久的谎言
#
# 退出码：0 一致；1 存在失效引用；2 前置条件不满足（缺文件）。
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT" || exit 2

AGENTS="${1:-AGENTS.md}"   # 可选参数：便于单独验证解析器（默认 AGENTS.md）
[[ -f "$AGENTS" ]] || { echo "check-artifact-refs: 缺少 $AGENTS" >&2; exit 2; }

# 历史遗留的失效引用：<tasks.md 路径>|<被引用路径>|<原因>
# 只允许"已交付且不再改动"的特性登记；新特性一律改成让文件存在，而不是往这里加行。
LEGACY_ALLOW=(
	'specs/006-app-admin-role/tasks.md|internal/api/me/app_admin_guard_test.go|006 落地时该用例并入 internal/api/me/app_admin_test.go，历史任务书不再改动'
)

fail=0
checked=0
report() { printf '  [fail] %s %s\n' "$1" "$2"; fail=$((fail + 1)); }
skip() { printf '  [skip] %s %s\n' "$1" "$2"; }

allow_hit() { # allow_hit <tasks.md> <被引用路径>
	local f="$1" p="$2" entry
	for entry in "${LEGACY_ALLOW[@]}"; do
		[[ "${entry%%|*}" == "$f" ]] || continue
		[[ "${entry#*|}" == "$p|"* ]] && return 0
	done
	return 1
}

ignored() { # ignored <路径>：被 .gitignore 排除且尚未生成（如开发私钥、.tmp 中间产物）
	git rev-parse --git-dir >/dev/null 2>&1 || return 1
	# 目录型规则（`.tmp/`）只匹配"目录"，而不存在的路径会被 git 当成普通文件，
	# 于是干净检出（CI）上 `git check-ignore -- .tmp` 判不中、误报成"树里少了一个目录"。
	# 开发机因为跑过 make docs 恰好存在 `.tmp/`，这个盲区一直藏着 —— 所以两种形态都探。
	git check-ignore -q --no-index -- "$1" 2>/dev/null && return 0
	git check-ignore -q --no-index -- "${1%/}/" 2>/dev/null
}

# ---------------------------------------------------------------------------
# 1. AGENTS.md §3 目录树
#    树用 box-drawing 字符绘制，每层 4 字符宽；这里按前缀回推完整路径。
#    跳过：glob 条目（含 `*`）、以及被 gitignore 排除且不存在的条目。
# ---------------------------------------------------------------------------
tree_paths() {
	perl -ne '
		BEGIN { $in = 0; $fence = 0; @stack = (); }
		# 只扫 §3 章节
		if (!$in) { $in = 1 if /^##\s*3\./; next }
		exit if /^##\s*4\./;
		if (/^```/) { $fence = !$fence; next }
		next unless $fence;
		chomp;
		my $l = $_;
		next if $l =~ /^\s*$/;
		my $d = 0;
		while ($l =~ s/^(?:│   |    )//) { $d++ }
		next unless $l =~ s/^(?:├── |└── )//;
		$l =~ s/\s+#.*$//;
		$l =~ s/\s+$//;
		next if $l eq "" || $l =~ /\*/;    # glob 与空条目
		$stack[$d] = $l;
		$#stack = $d;
		print join("", @stack), "\n";
	' "$AGENTS"
}

while IFS= read -r p; do
	[[ -n "$p" ]] || continue
	checked=$((checked + 1))
	probe="${p%/}"
	[[ -e "$probe" ]] && continue
	if ignored "$probe"; then
		skip "AGENTS §3" "$probe（gitignored，未生成属正常）"
		continue
	fi
	report "AGENTS §3" "目录树条目不存在：$probe"
done < <(tree_paths)

# ---------------------------------------------------------------------------
# 2/3. tasks.md：DoD 行（硬要求）与正文里路径限定的测试/脚本引用
# ---------------------------------------------------------------------------
REF_RE='(\./)?(internal|web|scripts|deploy|specs|cmd|migrations|configs|tools)/[A-Za-z0-9_./+-]+'
QUALIFIED_TEST_RE='([A-Za-z0-9_.-]+/)+[A-Za-z0-9_.-]+_test\.go|scripts/[A-Za-z0-9_./-]+'

shopt -s nullglob
for f in specs/*/tasks.md; do
	# 2) DoD 行里的仓库路径
	while IFS= read -r ref; do
		[[ -n "$ref" ]] || continue
		probe="${ref#./}"
		probe="${probe%/}"
		# 去掉行尾可能的标点/引号
		probe="${probe%%[，。）)\]】]*}"
		[[ -e "$probe" ]] && continue
		ignored "$probe" && continue
		report "DoD" "$f: 引用的路径不存在：$probe"
	done < <(grep -E 'DoD:' "$f" 2>/dev/null | grep -oE "$REF_RE" | sort -u)

	# 3) 正文里路径限定的测试/脚本引用（裸文件名有歧义，不判）
	while IFS= read -r ref; do
		[[ -n "$ref" ]] || continue
		[[ -e "$ref" ]] && continue
		allow_hit "$f" "$ref" && continue
		report "tasks.md" "$f: 引用的文件不存在：$ref"
	done < <(grep -oE "$QUALIFIED_TEST_RE" "$f" 2>/dev/null | sort -u)
done
shopt -u nullglob

# 失效的豁免项：登记的路径已经存在 → 提示删除（不阻塞门禁，避免清理动作反过来卡提交）
for entry in "${LEGACY_ALLOW[@]}"; do
	f="${entry%%|*}"
	rest="${entry#*|}"
	p="${rest%%|*}"
	[[ -e "$p" ]] && skip "LEGACY_ALLOW" "$f 的豁免项已失效（$p 已存在），请从 check-artifact-refs.sh 删除该行"
done

if [[ "$fail" -gt 0 ]]; then
	echo "check-artifact-refs: $fail 处失效引用（任务书/目录树声称的路径不存在）" >&2
	exit 1
fi
echo "check-artifact-refs: AGENTS §3 目录树与 tasks.md 引用一致（校验 $checked 个路径）"
exit 0

#!/usr/bin/env bash
# 干净检出复跑（2026-10-22 复盘新增）。
#
# 背景：005 交付时 `make lint` 在开发机全绿、在 CI 全红——因为本地有
# web/*/node_modules 与 configs/dev-signing-key.pem 等未入库文件，而 CI 是干净检出。
# 本脚本用临时 git worktree 复刻「只有 tracked 文件」的环境（比 clone 快，
# 且不会污染当前工作区），在其中跑完整的交付门禁。
#
# 用法：make ci-local（首次会 npm ci，约 1–2 分钟）
# 退出码：0 通过；非 0 表示在干净环境下门禁失败。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

TMP="$(mktemp -d)"
WORKTREE="$TMP/repo"
cleanup() {
	cd "$ROOT" 2>/dev/null || true # 先把 cwd 移出待删目录，否则 bash 会报 getcwd 失败
	git -C "$ROOT" worktree remove --force "$WORKTREE" >/dev/null 2>&1 || true
	rm -rf "$TMP"
}
trap cleanup EXIT

git -C "$ROOT" worktree add --detach "$WORKTREE" HEAD >/dev/null

# worktree 取的是 HEAD 快照：未提交改动不在其中（这正是「干净检出」的含义）。
if [[ -n "$(git -C "$ROOT" status --porcelain)" ]]; then
	printf 'ci-local: 注意——工作区有未提交改动，本次只验证 HEAD 快照\n'
fi

# CI 没有这些变量：集成用例在干净环境应走 t.Skip，而不是连本地库。
unset TEST_MYSQL_DSN TEST_REDIS_ADDR

cd "$WORKTREE"
printf 'ci-local: 干净工作树 %s（HEAD=%s）\n' "$WORKTREE" "$(git rev-parse --short HEAD)"
printf 'ci-local: 未跟踪/未入库文件不会出现在此工作树中（与 CI 一致）\n'

make build
make test
make lint
make check

echo "ci-local: 干净检出下全部门禁通过"

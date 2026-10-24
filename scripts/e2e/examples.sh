#!/usr/bin/env bash
# 三语言接入示例的本地端到端验证（特性 008-integration-examples）。
#
# 当前状态：**骨架占位**。断言清单与行为约定见
#   specs/008-integration-examples/contracts/e2e-examples-cli.md
# 实现落在 specs/008-integration-examples/tasks.md 的 T019–T024。
#
# 占位阶段刻意以退出码 2 拒绝执行：门禁要求"任务书点名的脚本必须真实存在"，
# 但存在不等于可用——宁可明确报"未实现"，也不让 exit 0 被误读成"验证已通过"。
set -uo pipefail

cat >&2 <<'MSG'
examples.sh: 尚未实现（占位骨架）。
  断言清单：specs/008-integration-examples/contracts/e2e-examples-cli.md
  实现任务：specs/008-integration-examples/tasks.md T019–T024
MSG
exit 2

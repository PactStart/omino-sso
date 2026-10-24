#!/usr/bin/env bash
# 示例端到端脚本的管理员侧辅助：客户端注册复用、专用示例账号的创建与登录。
#
# 当前状态：**骨架占位**。实现在 specs/008-integration-examples/tasks.md T004–T005。
# 约定（届时补全）：
#   - 客户端按 clientName 逐字比对复用，缺失才创建；
#   - clientSecret 优先复用 .tmp/e2e-examples/clients.env，缺失才轮换；
#   - 写文件用 umask 077 + chmod 600；
#   - 专用账号"直接创建、已存在视为成功"，登录失败即中止不重试。
set -uo pipefail

admin_lib::not_implemented() {
	echo "admin.sh: 尚未实现（占位骨架），见 specs/008-integration-examples/tasks.md T004–T005" >&2
	return 2
}

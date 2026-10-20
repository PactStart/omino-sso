export const IS_DEV = import.meta.env.DEV

/**
 * admin 是独立构建产物（AGENTS.md §8.3：portal 5173 / admin 5174）。
 * 开发态跨端口 -> 绝对地址；生产同域 -> PRD §2 中的 /admin/*。
 * 两种情况都用整页跳转，避免 SPA 路由到不存在的路由。
 */
export const ADMIN_HOME = IS_DEV ? 'http://localhost:5174/admin/users' : '/admin/users'

export const SITE_NAME = '统一身份认证'

export const IS_DEV = import.meta.env.DEV

/** admin 与 portal 是两个独立构建产物（AGENTS.md §8.3：5174 / 5173） */
export const PORTAL_HOME = IS_DEV ? 'http://localhost:5173/account/profile' : '/account/profile'

/**
 * 会话失效后的登录地址。
 * 开发态跨端口，next 参数在 5173 上无意义，故只在同域（生产）透传。
 */
export function portalLoginUrl(next?: string): string {
  const base = IS_DEV ? 'http://localhost:5173/login' : '/login'
  if (!IS_DEV && next) return `${base}?next=${encodeURIComponent(next)}`
  return base
}

export const SITE_NAME = '统一身份认证'

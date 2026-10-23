export const IS_DEV = import.meta.env.DEV

/**
 * 运行期配置来自构建期环境变量（Vite 只暴露 VITE_ 前缀）：
 *   - VITE_API_BASE_URL       API 基地址（同源默认 /api/v1；跨域直连写绝对地址）
 *   - VITE_PORTAL_HOME        门户账号中心地址
 *   - VITE_PORTAL_LOGIN_URL   未登录时的门户登录页地址
 *   - VITE_SITE_NAME          站点名
 */
const env = import.meta.env

export const API_BASE_URL: string = env.VITE_API_BASE_URL || '/api/v1'

/**
 * admin 与 portal 是两份独立产物、各自独立入口：
 * admin 的路由一律挂根路径（/users、/clients…），dev(5174) 与 test/prod(admin.<主域>) 行为一致。
 * 跨站回跳（门户↔后台）靠下表的环境变量注入绝对地址。
 */
export const PORTAL_HOME: string =
  env.VITE_PORTAL_HOME || (IS_DEV ? 'http://localhost:5173/account/profile' : '/account/profile')

/**
 * 会话失效后的登录地址。
 *
 * `next` 一律转成**绝对地址**（当前 admin 域的 origin + 路径）：四域下门户与后台不同域，
 * 相对路径会被门户当成它自己的路由（`/users` 在门户上不存在），登录后回不到原页面。
 * 门户侧用 `utils/redirect.ts` 的 origin 白名单校验，非法目标会被丢弃而不是跳转。
 */
export function portalLoginUrl(next?: string): string {
  const base = env.VITE_PORTAL_LOGIN_URL || (IS_DEV ? 'http://localhost:5173/login' : '/login')
  // 无 next = 主动登出：带上 loggedOut=1，门户登录页据此提示"已退出"（FR-023）。
  // 有 next = 会话失效回跳：只关心回到原路径，不提示登出。
  if (!next) return `${base}?loggedOut=1`
  const target = new URL(next, window.location.origin).toString()
  return `${base}?next=${encodeURIComponent(target)}`
}

export const SITE_NAME: string = env.VITE_SITE_NAME || '统一身份认证'

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

/** admin 与 portal 同域部署：生产同在根路径下（portal 在 /，admin 在 /admin/） */
export const PORTAL_HOME: string =
  env.VITE_PORTAL_HOME || (IS_DEV ? 'http://localhost:5173/account/profile' : '/account/profile')

/**
 * 会话失效后的登录地址。
 * 开发态跨端口，next 参数在 5173 上无意义，故只在同域（生产）透传。
 */
export function portalLoginUrl(next?: string): string {
  const base = env.VITE_PORTAL_LOGIN_URL || (IS_DEV ? 'http://localhost:5173/login' : '/login')
  if (!IS_DEV && next) return `${base}?next=${encodeURIComponent(next)}`
  return base
}

export const SITE_NAME: string = env.VITE_SITE_NAME || '统一身份认证'

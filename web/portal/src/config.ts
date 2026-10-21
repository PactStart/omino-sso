export const IS_DEV = import.meta.env.DEV

/**
 * 运行期配置全部来自构建期环境变量（Vite 只暴露 VITE_ 前缀），
 * 便于同一份代码在 dev/test/prod 用不同部署拓扑构建：
 *   - VITE_API_BASE_URL   API 基地址；同源反代时用相对路径（默认 /api/v1），
 *                         跨域直连时写绝对地址（如 https://sso.example.com/api/v1）
 *   - VITE_ADMIN_HOME     管理后台入口；缺省按开发/生产推导
 *   - VITE_SITE_NAME      站点名
 */
const env = import.meta.env

export const API_BASE_URL: string = env.VITE_API_BASE_URL || '/api/v1'

/**
 * admin 是独立构建产物：dev 下是 5174 端口，生产与 portal 同域、挂在 /admin/ 子路径
 * （nginx 单入口方案，见 web/nginx.conf.template）。
 * 两种情况都用整页跳转，避免 SPA 路由到不存在的路由。
 */
export const ADMIN_HOME: string =
  env.VITE_ADMIN_HOME || (IS_DEV ? 'http://localhost:5174/admin/users' : '/admin/users')

export const SITE_NAME: string = env.VITE_SITE_NAME || '统一身份认证'

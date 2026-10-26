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
 * admin 是独立构建产物、独立入口，路由挂在**域名根**（FR-004/FR-005：入口为 `admin.<主域>/`，
 * 由它自己 `/` → `/users` 重定向），三套环境一致：
 *   - dev：vite 5174，`http://localhost:5174/`
 *   - test/prod：`https://admin.<主域>/`，由构建期 `VITE_ADMIN_HOME` 注入
 * 未注入时退化为同域根路径 `/`，仅在一体化演示（同域反代）下成立。
 * 两种情况都用整页跳转，避免 SPA 路由到不存在的路由。
 */
export const ADMIN_HOME: string =
  env.VITE_ADMIN_HOME || (IS_DEV ? 'http://localhost:5174/' : '/')

export const SITE_NAME: string = env.VITE_SITE_NAME || '统一身份认证'

/**
 * 统一身份（IdP）登录域，形如 `https://sso.example.com`（009-portal-login-redirect）。
 *
 * 两个用途：
 *   1. 回跳白名单：未登录 SSO 时 IdP 把浏览器送到本页（`?next=<sso>/login?id=…&portal=1`），
 *      登录成功后必须能整页跳回该地址；不在白名单会被 `resolveNext` 丢弃，
 *      表现为"登录成功却停在门户首页"。
 *   2. 文案判定：`next` 指向 IdP 时显示「登录后返回 <应用名>」，而不是 `/login` 这类技术路径。
 *
 * dev 缺省 `http://localhost:8080`；test/prod 由构建参数 `VITE_SSO_BASE_URL` 注入，
 * 且 `web/Dockerfile` 的 portal target 对其做构建期强制（缺失即构建失败）。
 */
export const SSO_BASE_URL: string =
  env.VITE_SSO_BASE_URL || (IS_DEV ? 'http://localhost:8080' : '')

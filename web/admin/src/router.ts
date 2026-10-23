import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import { portalLoginUrl } from '@/config'
import { useSessionStore } from '@/stores/session'
import Forbidden from '@/views/Forbidden.vue'
import NotFound from '@/views/NotFound.vue'

// 路由一律挂在根路径：admin 是独立构建产物、独立域名（admin.<主域>/users），
// dev 的 5174 与 test/prod 的 nginx 静态站点行为一致，地址栏不得出现 /admin 冗余段（007 FR / SC-002）。
const routes: RouteRecordRaw[] = [
  { path: '/', redirect: '/users' },
  {
    path: '/users',
    name: 'users',
    component: () => import('@/views/Users.vue'),
    meta: { title: '用户管理', requiresAdmin: true },
  },
  {
    path: '/clients',
    name: 'clients',
    component: () => import('@/views/Clients.vue'),
    meta: { title: '应用管理', requiresAdmin: true },
  },
  {
    // 应用子页面：左侧五菜单固定，右侧内容区由子路由渲染（FR-001 / FR-002）
    path: '/clients/:clientId',
    component: () => import('@/views/client/ClientDetail.vue'),
    meta: { title: '应用详情', requiresAdmin: true },
    children: [
      {
        path: '',
        name: 'client-detail',
        redirect: (to) => ({
          name: 'client-overview',
          params: { clientId: String(to.params.clientId) },
        }),
      },
      {
        path: 'overview',
        name: 'client-overview',
        component: () => import('@/views/client/ClientOverview.vue'),
        meta: { title: '应用详情', requiresAdmin: true },
      },
      {
        path: 'members',
        name: 'client-members',
        component: () => import('@/views/client/ClientMembers.vue'),
        meta: { title: '应用成员', requiresAdmin: true },
      },
      {
        path: 'roles',
        name: 'client-roles',
        component: () => import('@/views/client/ClientRoles.vue'),
        meta: { title: '应用角色', requiresAdmin: true },
      },
      {
        path: 'permissions',
        name: 'client-permissions',
        component: () => import('@/views/client/ClientPermissions.vue'),
        meta: { title: '应用权限', requiresAdmin: true },
      },
      {
        path: 'grants',
        name: 'client-grants',
        component: () => import('@/views/client/ClientGrants.vue'),
        meta: { title: '授权管理', requiresAdmin: true },
      },
      {
        // 非法 tab 一律回到「应用详情」，不落到 404（T015）
        path: ':tab',
        name: 'client-tab-fallback',
        redirect: (to) => ({
          name: 'client-overview',
          params: { clientId: String(to.params.clientId) },
        }),
      },
    ],
  },
  {
    path: '/audit',
    name: 'audit',
    component: () => import('@/views/Audit.vue'),
    meta: { title: '审计日志', requiresAdmin: true },
  },
  {
    path: '/files',
    name: 'files',
    component: () => import('@/views/Files.vue'),
    meta: { title: '文件管理', requiresAdmin: true },
  },
  {
    path: '/send-records',
    name: 'send-records',
    component: () => import('@/views/SendRecords.vue'),
    meta: { title: '发送记录', requiresAdmin: true },
  },
  {
    path: '/offline',
    name: 'offline',
    component: () => import('@/views/Offline.vue'),
    meta: { public: true, title: '服务不可用' },
  },
  { path: '/403', name: 'forbidden', component: Forbidden, meta: { public: true } },
  { path: '/:pathMatch(.*)*', name: 'not-found', component: NotFound, meta: { public: true } },
]

export const router = createRouter({
  history: createWebHistory('/'),
  routes,
})

router.beforeEach(async (to) => {
  if (to.meta.public) return true

  const session = useSessionStore()
  const me = await session.fetchMe({ silent: true })

  // 服务不可用 ≠ 未登录：留在后台展示重试页，避免把运维误导到登录页
  if (!me && session.probeFailure === 'unavailable') {
    return { path: '/offline', query: { next: to.fullPath }, replace: true }
  }

  // admin 产物内没有登录页：未登录直接去 portal 登录（生产同域，可带 next 回跳）
  if (!me) {
    window.location.replace(portalLoginUrl(to.fullPath))
    return false
  }
  if (to.meta.requiresAdmin && !me.roles.includes('admin')) {
    return { path: '/403' }
  }
  return true
})

router.afterEach((to) => {
  const title = to.meta.title as string | undefined
  document.title = title ? `${title} - 管理后台` : '管理后台 - 统一身份认证'
})

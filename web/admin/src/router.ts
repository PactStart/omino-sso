import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import { portalLoginUrl } from '@/config'
import { useSessionStore } from '@/stores/session'
import Forbidden from '@/views/Forbidden.vue'
import NotFound from '@/views/NotFound.vue'

const routes: RouteRecordRaw[] = [
  { path: '/', redirect: '/admin/users' },
  { path: '/admin', redirect: '/admin/users' },
  {
    path: '/admin/users',
    name: 'users',
    component: () => import('@/views/Users.vue'),
    meta: { title: '用户管理', requiresAdmin: true },
  },
  {
    path: '/admin/clients',
    name: 'clients',
    component: () => import('@/views/Clients.vue'),
    meta: { title: '应用管理', requiresAdmin: true },
  },
  {
    path: '/admin/audit',
    name: 'audit',
    component: () => import('@/views/Audit.vue'),
    meta: { title: '审计日志', requiresAdmin: true },
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

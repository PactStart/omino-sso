import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import { ADMIN_HOME } from '@/config'
import { useSessionStore } from '@/stores/session'
import type { Me } from '@/api/types'
import Home from '@/views/Home.vue'
import Forbidden from '@/views/Forbidden.vue'
import NotFound from '@/views/NotFound.vue'

const routes: RouteRecordRaw[] = [
  { path: '/', name: 'home', component: Home, meta: { requiresAuth: true } },
  {
    path: '/login',
    name: 'login',
    component: () => import('@/views/Login.vue'),
    meta: { public: true },
  },
  { path: '/account', redirect: '/account/profile' },
  {
    path: '/account/profile',
    name: 'profile',
    component: () => import('@/views/Account.vue'),
    meta: { requiresAuth: true, title: '个人信息' },
  },
  {
    path: '/account/apps',
    name: 'apps',
    component: () => import('@/views/Apps.vue'),
    meta: { requiresAuth: true, title: '我的应用' },
  },
  // 门户应用管理（006）：独立地址、可收藏；子页共用外层壳（摘要 + tab）。
  // handleForbidden: 'local' 让 403 由页面内提示处理，而不是整页跳 /403。
  {
    path: '/account/apps/:clientId/manage',
    component: () => import('@/views/app-admin/AppAdminLayout.vue'),
    meta: { requiresAuth: true, handleForbidden: 'local', title: '应用管理' },
    children: [
      {
        path: '',
        redirect: (to) =>
          `/account/apps/${encodeURIComponent(String(to.params.clientId ?? ''))}/manage/overview`,
      },
      {
        // 应用信息：参考 admin 端详情，但门户只开放名称/描述/Logo/回调地址（006 口径调整）。
        path: 'overview',
        name: 'app-manage-overview',
        component: () => import('@/views/app-admin/Overview.vue'),
        meta: { title: '应用信息' },
      },
      {
        path: 'members',
        name: 'app-manage-members',
        component: () => import('@/views/app-admin/Members.vue'),
        meta: { title: '应用成员' },
      },
      {
        path: 'roles',
        name: 'app-manage-roles',
        component: () => import('@/views/app-admin/Roles.vue'),
        meta: { title: '应用角色' },
      },
      {
        path: 'permissions',
        name: 'app-manage-permissions',
        component: () => import('@/views/app-admin/Permissions.vue'),
        meta: { title: '应用权限' },
      },
      {
        path: 'grants',
        name: 'app-manage-grants',
        component: () => import('@/views/app-admin/Grants.vue'),
        meta: { title: '授权管理' },
      },
    ],
  },
  {
    path: '/account/sessions',
    name: 'sessions',
    component: () => import('@/views/Sessions.vue'),
    meta: { requiresAuth: true, title: '登录设备' },
  },
  {
    path: '/account/password',
    name: 'password',
    component: () => import('@/views/Password.vue'),
    meta: { requiresAuth: true, title: '修改密码' },
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

/** admin 属于另一个构建产物，必须整页跳转 */
export function goAdminHome(): void {
  window.location.assign(ADMIN_HOME)
}

function landingFor(me: Me): 'admin' | '/account/profile' {
  return me.roles.includes('admin') ? 'admin' : '/account/profile'
}

router.beforeEach(async (to) => {
  const session = useSessionStore()

  if (to.meta.public) {
    // 403/404 不调接口（PRD §4 X1/X2）
    if (to.name !== 'login') return true
    // 已登录访问登录页 -> 直接落地（PRD §4 P1）
    const me = await session.fetchMe({ silent: true })
    if (!me) return true
    const next = typeof to.query.next === 'string' ? to.query.next : ''
    if (next.startsWith('/') && !next.startsWith('/login')) {
      return { path: next, replace: true }
    }
    const target = landingFor(me)
    if (target === 'admin') {
      goAdminHome()
      return false
    }
    return { path: target, replace: true }
  }

  const me = await session.fetchMe({ silent: true })
  if (!me) {
    // 服务不可用 ≠ 未登录：不要误导用户去重新登录
    if (session.probeFailure === 'unavailable') {
      return { path: '/offline', query: { next: to.fullPath }, replace: true }
    }
    return { path: '/login', query: { next: to.fullPath } }
  }
  if (to.meta.requiresAdmin && !me.roles.includes('admin')) {
    return { path: '/403' }
  }
  if (to.path === '/') {
    const target = landingFor(me)
    if (target === 'admin') {
      goAdminHome()
      return false
    }
    return { path: target, replace: true }
  }
  return true
})

router.afterEach((to) => {
  const title = to.meta.title as string | undefined
  document.title = title ? `${title} - 统一身份认证` : '统一身份认证'
})

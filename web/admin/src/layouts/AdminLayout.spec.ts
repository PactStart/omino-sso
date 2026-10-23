import { mount } from '@vue/test-utils'
import ElementPlus, { ElMenuItem } from 'element-plus'
import type { ComponentCustomProperties } from 'vue'
import type { Router } from 'vue-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'

/**
 * 一级菜单的 index 就是路由地址（el-menu 开了 router 模式）。
 * 007 起 admin 是独立构建产物、独立域名（admin.<主域>/users），
 * 因此 index 一律挂根路径，不得出现 `/admin` 冗余段（FR-004 / SC-002）。
 */
const MENU_INDEX = ['/users', '/clients', '/audit', '/files', '/send-records']

const pushMock = vi.fn<(path: string) => Promise<void>>(() => Promise.resolve())
const signOutMock = vi.fn(() => Promise.resolve())

const routeState = { path: '/clients/demo-app/members' }

vi.mock('vue-router', () => ({
  useRoute: () => routeState,
}))

vi.mock('@/stores/session', () => ({
  useSessionStore: () => ({ user: null, signOut: signOutMock }),
}))

import AdminLayout from './AdminLayout.vue'

function mountLayout() {
  return mount(AdminLayout, {
    global: {
      plugins: [ElementPlus],
      // el-menu 的 router 模式取 appContext.globalProperties.$router（不是 useRouter），
      // 这里注入假 router 即可断言跳转目标，无需挂真实路由表。
      config: {
        globalProperties: { $router: { push: pushMock } as unknown as Router } as ComponentCustomProperties,
      },
    },
    slots: { default: '<div class="page-body" />' },
  })
}

function menuIndexes(wrapper: ReturnType<typeof mountLayout>): string[] {
  return wrapper.findAllComponents(ElMenuItem).map((item) => String(item.props('index')))
}

describe('管理后台一级菜单（007 FR-004）', () => {
  beforeEach(() => {
    pushMock.mockClear()
    signOutMock.mockClear()
  })

  it('五项菜单 index 为根路径，顺序与文案固定', () => {
    const wrapper = mountLayout()
    const items = wrapper.findAllComponents(ElMenuItem)

    expect(menuIndexes(wrapper)).toEqual(MENU_INDEX)
    expect(items.map((item) => item.text())).toEqual([
      '用户管理',
      '应用管理',
      '审计日志',
      '文件管理',
      '发送记录',
    ])
  })

  it('菜单 index 不含 /admin 前缀（独立域名下地址栏不得出现冗余段）', () => {
    const wrapper = mountLayout()

    for (const index of menuIndexes(wrapper)) {
      expect(index.startsWith('/admin')).toBe(false)
      expect(index).toMatch(/^\/[a-z-]+$/)
    }
  })

  it('菜单开启 router 模式，点击按 index 跳转且目标同样是根路径', async () => {
    const wrapper = mountLayout()
    expect(wrapper.findComponent({ name: 'ElMenu' }).props('router')).toBe(true)

    await wrapper.findAllComponents(ElMenuItem)[3]!.trigger('click')

    expect(pushMock).toHaveBeenCalledWith('/files')
    expect(String(pushMock.mock.calls[0]![0])).not.toContain('/admin')
  })

  it('应用子页面（/clients/:id/*）仍高亮「应用管理」一级菜单', () => {
    const wrapper = mountLayout()

    expect(wrapper.findComponent({ name: 'ElMenu' }).props('defaultActive')).toBe('/clients')
  })
})

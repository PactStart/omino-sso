import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'

/**
 * 服务不可用页：会话恢复后回跳原路径，没有 next 时落到后台首页 `/users`。
 * `/users` 是 admin 根路径下的首个落地页，不能带 `/admin` 冗余段（007 FR-004）。
 */
const replaceMock = vi.fn<(path: string) => Promise<void>>(() => Promise.resolve())
const routeState: { query: Record<string, string> } = { query: {} }

const sessionState: { me: { userId: string } | null; probeFailure: string | null } = {
  me: null,
  probeFailure: null,
}

vi.mock('vue-router', () => ({
  useRoute: () => routeState,
  useRouter: () => ({ replace: replaceMock }),
}))

vi.mock('@/stores/session', () => ({
  useSessionStore: () => ({
    get probeFailure() {
      return sessionState.probeFailure
    },
    fetchMe: () => Promise.resolve(sessionState.me),
  }),
}))

import Offline from './Offline.vue'

function findButton(wrapper: ReturnType<typeof mountOffline>, label: string) {
  const button = wrapper.findAll('button').find((b) => b.text() === label)
  if (!button) throw new Error(`未找到按钮：${label}`)
  return button
}

function mountOffline() {
  return mount(Offline, { global: { plugins: [ElementPlus] } })
}

describe('服务不可用页（007 FR-004）', () => {
  beforeEach(() => {
    replaceMock.mockClear()
    routeState.query = {}
    sessionState.me = { userId: 'u1' }
    sessionState.probeFailure = null
  })

  it('重试成功且无 next 时回跳 /users（根路径，不含 /admin）', async () => {
    const wrapper = mountOffline()

    await findButton(wrapper, '重试').trigger('click')
    await flushPromises()

    expect(replaceMock).toHaveBeenCalledWith('/users')
    expect(String(replaceMock.mock.calls[0]![0])).not.toContain('/admin')
  })

  it('带合法 next 时回跳原路径，而不是固定 /users', async () => {
    routeState.query = { next: '/clients/demo-app/roles' }
    const wrapper = mountOffline()

    await findButton(wrapper, '重试').trigger('click')
    await flushPromises()

    expect(replaceMock).toHaveBeenCalledWith('/clients/demo-app/roles')
  })

  it('仍是服务不可用时只提示重试，不发生跳转', async () => {
    sessionState.me = null
    sessionState.probeFailure = 'unavailable'
    const wrapper = mountOffline()

    await findButton(wrapper, '重试').trigger('click')
    await flushPromises()

    expect(replaceMock).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('仍然无法连接')
  })
})

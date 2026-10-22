import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { AppItem } from '@/api/types'

const getMyAppsMock = vi.fn()
const pushMock = vi.fn()

vi.mock('@/api', () => ({ getMyApps: () => getMyAppsMock() }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push: pushMock }) }))

import Apps from './Apps.vue'

const baseApp: AppItem = {
  clientId: 'demo-app',
  name: '验收应用',
  description: '内部验收用',
  logoUrl: '',
  homeUrl: 'https://demo.example.com',
  lastAccessAt: null,
  isAppAdmin: false,
}

function mountApps() {
  return mount(Apps, {
    global: { stubs: { PortalLayout: { template: '<div><slot /></div>' } } },
  })
}

function findButton(wrapper: VueWrapper, label: string) {
  const btn = wrapper.findAll('button').find((b) => b.text() === label)
  if (!btn) throw new Error(`未找到按钮：${label}`)
  return btn
}

function buttonLabels(wrapper: VueWrapper): string[] {
  return wrapper.findAll('button').map((b) => b.text())
}

describe('我的应用 · 应用管理入口（006）', () => {
  beforeEach(() => {
    getMyAppsMock.mockReset()
    pushMock.mockReset()
  })

  it('应用管理员看到「管理」，且位于「打开」左侧（FR-004）', async () => {
    getMyAppsMock.mockResolvedValue([{ ...baseApp, isAppAdmin: true }])
    const wrapper = mountApps()
    await flushPromises()

    const labels = buttonLabels(wrapper)
    expect(labels).toContain('管理')
    expect(labels.indexOf('管理')).toBeLessThan(labels.indexOf('打开'))
  })

  it('普通成员只有「打开」，没有「管理」（FR-004）', async () => {
    getMyAppsMock.mockResolvedValue([{ ...baseApp, isAppAdmin: false }])
    const wrapper = mountApps()
    await flushPromises()

    const labels = buttonLabels(wrapper)
    expect(labels).toContain('打开')
    expect(labels).not.toContain('管理')
  })

  it('仅对自己是管理员的那个应用展示「管理」（US2-AC3）', async () => {
    getMyAppsMock.mockResolvedValue([
      { ...baseApp, clientId: 'app-x', name: '应用 X', isAppAdmin: true },
      { ...baseApp, clientId: 'app-y', name: '应用 Y', isAppAdmin: false },
    ])
    const wrapper = mountApps()
    await flushPromises()

    const cards = wrapper.findAll('article')
    expect(cards).toHaveLength(2)
    expect(cards[0]!.text()).toContain('管理')
    expect(cards[1]!.text()).not.toContain('管理')
  })

  it('点击「管理」进入该应用的管理页（独立地址）', async () => {
    getMyAppsMock.mockResolvedValue([{ ...baseApp, isAppAdmin: true }])
    const wrapper = mountApps()
    await flushPromises()

    await findButton(wrapper, '管理').trigger('click')
    expect(pushMock).toHaveBeenCalledWith('/account/apps/demo-app/manage')
  })
})

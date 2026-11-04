import { flushPromises, mount } from '@vue/test-utils'
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import type { PermissionExportDocument } from '@/api/types'

/**
 * 011：门户权限页的入口与导出行为（T050 / FR-020、SC-007）。
 *
 * 门户这一页是 admin 端的复制实现（006 既定），逻辑同构但不共享代码，
 * 因此必须独立锁住「下载内容不含 importable」「不可回导先提示后下载」两条语义，
 * 否则两端会在无人察觉时漂移。
 */
const mocks = vi.hoisted(() => ({
  listAppPermissions: vi.fn(),
  createAppPermission: vi.fn(),
  updateAppPermission: vi.fn(),
  deleteAppPermission: vi.fn(),
  exportPermissions: vi.fn(),
}))

const confirmMock = vi.hoisted(() => vi.fn())
const downloadMock = vi.hoisted(() => vi.fn())

vi.mock('@/api/app', () => mocks)
vi.mock('@/api/client', () => ({
  errorCode: (e: unknown) => (e as { code?: string })?.code ?? 'UNKNOWN',
  errorMessage: (e: unknown) => String((e as { message?: string })?.message ?? e),
}))
vi.mock('@/utils/file', () => ({ downloadJson: downloadMock }))
vi.mock('vue-router', () => ({ useRoute: () => ({ params: { clientId: 'demo-app' } }) }))
vi.mock('element-plus', async (importOriginal) => {
  const actual = await importOriginal<typeof import('element-plus')>()
  return { ...actual, ElMessageBox: { ...actual.ElMessageBox, confirm: confirmMock } }
})

import Permissions from './Permissions.vue'

// el-table / el-tree 依赖 ResizeObserver，jsdom 未实现。
beforeAll(() => {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver
})

function exportDoc(over: Partial<PermissionExportDocument> = {}): PermissionExportDocument {
  return {
    version: 1,
    mode: 'merge',
    importable: { ok: true, reasons: [] },
    permissions: [{ code: 'order', name: '订单', children: [{ code: 'order.read', name: '查询' }] }],
    ...over,
  }
}

async function mountPage() {
  const wrapper = mount(Permissions, {
    global: { stubs: { PermissionImportDialog: true, teleport: true } },
  })
  await flushPromises()
  return wrapper
}

async function clickButton(wrapper: Awaited<ReturnType<typeof mountPage>>, label: string) {
  const btn = wrapper.findAll('button').find((b) => b.text() === label)
  if (!btn) throw new Error(`未找到按钮：${label}`)
  await btn.trigger('click')
  await flushPromises()
}

describe('门户 · 应用权限页（011）', () => {
  beforeEach(() => {
    // 视图偏好存在 localStorage；给一个最小实现，避免依赖 jsdom 的存储可用性
    vi.stubGlobal('localStorage', {
      getItem: () => null,
      setItem: () => undefined,
      removeItem: () => undefined,
    })
    Object.values(mocks).forEach((fn) => fn.mockReset())
    confirmMock.mockReset()
    downloadMock.mockReset()
    mocks.listAppPermissions.mockResolvedValue({ list: [], total: 0, page: 1, pageSize: 20 })
  })

  it('头部并列提供「下载示例 / 导出 / 批量导入」三个入口', async () => {
    const wrapper = await mountPage()
    const labels = wrapper.findAll('button').map((b) => b.text())
    expect(labels).toContain('下载示例')
    expect(labels).toContain('导出')
    expect(labels).toContain('批量导入')
  })

  it('下载示例产出 3 层示例文档', async () => {
    const wrapper = await mountPage()
    await clickButton(wrapper, '下载示例')

    expect(downloadMock).toHaveBeenCalledTimes(1)
    const [name, payload] = downloadMock.mock.calls[0]!
    expect(name).toBe('permissions-example.json')
    expect((payload as { permissions: unknown[] }).permissions).toHaveLength(1)
  })

  it('导出的下载文件只含 version/mode/permissions', async () => {
    mocks.exportPermissions.mockResolvedValue(exportDoc())
    const wrapper = await mountPage()

    await clickButton(wrapper, '导出')

    expect(mocks.exportPermissions).toHaveBeenCalledWith('demo-app')
    expect(confirmMock).not.toHaveBeenCalled()
    const [name, payload] = downloadMock.mock.calls[0]!
    expect(name).toMatch(/^permissions-demo-app-\d{8}\.json$/)
    expect(Object.keys(payload as Record<string, unknown>)).toEqual([
      'version',
      'mode',
      'permissions',
    ])
  })

  it('不可回导时先提示后下载；取消则不下载', async () => {
    mocks.exportPermissions.mockResolvedValue(
      exportDoc({ importable: { ok: false, reasons: ['too_many'] } }),
    )
    confirmMock.mockRejectedValueOnce(new Error('cancel'))
    const wrapper = await mountPage()

    await clickButton(wrapper, '导出')

    expect(confirmMock).toHaveBeenCalledTimes(1)
    expect(String(confirmMock.mock.calls[0]![0])).toContain('列表页最多展示 1000 条')
    expect(downloadMock).not.toHaveBeenCalled()
  })
})

import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { PermissionExportDocument } from '@/api/types'

/**
 * 011：权限页的导出入口（T027）。
 * ① 下载文件只含 {version, mode, permissions}，importable 不得进文件；
 * ② 不可回导时先提示后下载，取消则不下载；
 * ③ too_many 的提示附带"列表页最多展示 1000 条"，避免被误认为导出出错。
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

vi.mock('@/api', () => mocks)
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

import ClientPermissions from './ClientPermissions.vue'

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
  const wrapper = mount(ClientPermissions, {
    global: {
      plugins: [ElementPlus],
      stubs: { teleport: true, PermissionImportDialog: true },
    },
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

describe('应用权限页 · 导出（011）', () => {
  beforeEach(() => {
    // 视图偏好存在 localStorage；这里给一个最小实现，避免依赖 jsdom 的存储可用性
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

  it('下载文件只含 version/mode/permissions，且文件名带 clientId 与日期', async () => {
    mocks.exportPermissions.mockResolvedValue(exportDoc())
    const wrapper = await mountPage()

    await clickButton(wrapper, '导出')

    expect(mocks.exportPermissions).toHaveBeenCalledWith('demo-app')
    expect(downloadMock).toHaveBeenCalledTimes(1)
    const [name, payload] = downloadMock.mock.calls[0]!
    expect(name).toMatch(/^permissions-demo-app-\d{8}\.json$/)
    expect(Object.keys(payload as Record<string, unknown>)).toEqual([
      'version',
      'mode',
      'permissions',
    ])
    expect(payload).not.toHaveProperty('importable')
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

  it('确认继续后按提示顺序下载（提示在下载之前）', async () => {
    const order: string[] = []
    mocks.exportPermissions.mockResolvedValue(
      exportDoc({ importable: { ok: false, reasons: ['empty'] } }),
    )
    confirmMock.mockImplementation(async () => {
      order.push('confirm')
    })
    downloadMock.mockImplementation(() => {
      order.push('download')
    })
    const wrapper = await mountPage()

    await clickButton(wrapper, '导出')

    expect(String(confirmMock.mock.calls[0]![0])).toContain('还没有权限点')
    expect(order).toEqual(['confirm', 'download'])
  })
})

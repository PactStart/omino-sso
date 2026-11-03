import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { nextTick } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { PermissionImportPreview } from '@/api/types'

/**
 * 011：门户侧导入对话框。
 * 断言与服务端返回同构的 issues 文案、三态计数渲染、ok=false 禁用确认，
 * 以及「与 admin 端同一后端实现」在前端表现上的一致性（SC-007）。
 *
 * 门户未全局安装 Element Plus，组件通过 `./ep` 局部引入；用例直接挂真实组件。
 */
const mocks = vi.hoisted(() => ({
  validatePermissionImport: vi.fn(),
  confirmPermissionImport: vi.fn(),
}))

vi.mock('@/api/app', () => mocks)
vi.mock('@/api/client', () => ({
  errorCode: (e: unknown) => (e as { code?: string })?.code ?? 'UNKNOWN',
  errorMessage: (e: unknown) => String((e as { message?: string })?.message ?? e),
}))
vi.mock('@/utils/file', () => ({
  readJsonFile: vi.fn(async () => ''),
  downloadJson: vi.fn(),
}))

import PermissionImportDialog from './PermissionImportDialog.vue'

function preview(over: Partial<PermissionImportPreview> = {}): PermissionImportPreview {
  return {
    ok: true,
    added: 20,
    updated: 0,
    unchanged: 0,
    moved: 0,
    total: 20,
    issues: [],
    issueTotal: 0,
    warnings: [],
    warningTotal: 0,
    movedItems: [],
    ...over,
  }
}

let wrapper: VueWrapper | null = null

async function mountDialog() {
  wrapper = mount(PermissionImportDialog, {
    props: { modelValue: true, clientId: 'demo-app' },
    attachTo: document.body,
  })
  await flushPromises()
  await nextTick()
  return wrapper
}

function bodyText(): string {
  return document.body.textContent ?? ''
}

async function clickButton(label: string) {
  const btn = Array.from(document.body.querySelectorAll('button')).find((b) =>
    (b.textContent ?? '').includes(label),
  )
  if (!btn) throw new Error(`未找到按钮：${label}`)
  btn.click()
  await flushPromises()
  await nextTick()
}

async function typeJson(text: string) {
  const el = document.body.querySelector('textarea')
  if (!el) throw new Error('未找到 JSON 输入框')
  el.value = text
  el.dispatchEvent(new Event('input'))
  await flushPromises()
}

const validDoc = '{"permissions":[{"code":"order","name":"订单"}]}'

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
  document.body.innerHTML = ''
})

describe('门户 · 权限点批量导入对话框', () => {
  beforeEach(() => {
    mocks.validatePermissionImport.mockReset()
    mocks.confirmPermissionImport.mockReset()
  })

  it('渲染与服务端同构的问题清单与三态计数', async () => {
    mocks.validatePermissionImport.mockResolvedValue(
      preview({
        ok: false,
        added: 0,
        updated: 0,
        unchanged: 0,
        total: 3,
        issues: [
          {
            path: 'permissions[1].children[0].code',
            field: 'code',
            code: 'dup_code',
            message: '编码 "order.read" 重复：已出现在 permissions[0].code',
            line: null,
          },
        ],
        issueTotal: 1,
      }),
    )
    await mountDialog()
    await typeJson(validDoc)

    await clickButton('校验预览')

    expect(mocks.validatePermissionImport).toHaveBeenCalledWith(
      'demo-app',
      expect.anything(),
      'paste',
    )
    expect(bodyText()).toContain('permissions[1].children[0].code')
    expect(bodyText()).toContain('重复')
    expect(bodyText()).toContain('共 3 条')
    expect(bodyText()).toContain('将新增 0')
    expect(bodyText()).toContain('将更新 0')
    expect(bodyText()).toContain('无变化 0')

    const confirmBtn = Array.from(document.body.querySelectorAll('button')).find((b) =>
      (b.textContent ?? '').includes('确认导入'),
    ) as HTMLButtonElement | undefined
    expect(confirmBtn?.disabled).toBe(true)
  })

  it('校验通过时展示三态计数并允许确认', async () => {
    mocks.validatePermissionImport.mockResolvedValue(
      preview({ added: 15, updated: 3, unchanged: 2, total: 20 }),
    )
    await mountDialog()
    await typeJson(validDoc)
    await clickButton('校验预览')

    expect(bodyText()).toContain('将新增 15')
    expect(bodyText()).toContain('将更新 3')
    expect(bodyText()).toContain('无变化 2')
    const confirmBtn = Array.from(document.body.querySelectorAll('button')).find((b) =>
      (b.textContent ?? '').includes('确认导入'),
    ) as HTMLButtonElement | undefined
    expect(confirmBtn?.disabled).toBe(false)
  })

  it('非法 JSON 不请求服务端，且保留输入', async () => {
    await mountDialog()
    await typeJson('{"permissions":')

    await clickButton('校验预览')

    expect(mocks.validatePermissionImport).not.toHaveBeenCalled()
    expect(bodyText()).toContain('内容不是合法的 JSON')
    const ta = document.body.querySelector('textarea') as HTMLTextAreaElement
    expect(ta.value).toBe('{"permissions":')
  })
})

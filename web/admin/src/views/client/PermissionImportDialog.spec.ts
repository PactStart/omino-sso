import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { nextTick } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { PermissionImportPreview } from '@/api/types'

/**
 * 011：导入对话框。
 * 覆盖前端只做基础检查（FR-028）、问题清单渲染（FR-005）、ok=false 禁用确认（FR-003）、
 * 上级变更明细（FR-016）、失败保留输入（FR-030）与 409 自动重跑预览（T023）。
 *
 * el-dialog 会 teleport 到 body，因此断言走 document.body。
 */
const mocks = vi.hoisted(() => ({
  validatePermissionImport: vi.fn(),
  confirmPermissionImport: vi.fn(),
}))

vi.mock('@/api', () => mocks)
vi.mock('@/api/client', () => ({
  errorCode: (e: unknown) => (e as { code?: string })?.code ?? 'UNKNOWN',
  errorMessage: (e: unknown) => String((e as { message?: string })?.message ?? e),
}))
vi.mock('@/utils/file', () => ({
  readJsonFile: vi.fn(async () => ''),
  downloadJson: vi.fn(),
}))

import PermissionImportDialog from './PermissionImportDialog.vue'
import { downloadJson } from '@/utils/file'

function preview(over: Partial<PermissionImportPreview> = {}): PermissionImportPreview {
  return {
    ok: true,
    added: 50,
    updated: 0,
    unchanged: 0,
    moved: 0,
    total: 50,
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
    global: { plugins: [ElementPlus] },
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

function textareaValue(): string {
  return (document.body.querySelector('textarea') as HTMLTextAreaElement | null)?.value ?? ''
}

function confirmButton(): HTMLButtonElement | undefined {
  return Array.from(document.body.querySelectorAll('button')).find((b) =>
    (b.textContent ?? '').includes('确认导入'),
  ) as HTMLButtonElement | undefined
}

const validDoc = '{"permissions":[{"code":"order","name":"订单"}]}'

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
  document.body.innerHTML = ''
})

describe('权限点批量导入对话框', () => {
  beforeEach(() => {
    mocks.validatePermissionImport.mockReset()
    mocks.confirmPermissionImport.mockReset()
    vi.mocked(downloadJson).mockClear()
  })

  it('非法 JSON 不发请求，并保留管理员输入', async () => {
    await mountDialog()
    await typeJson('{"permissions":')

    await clickButton('校验预览')

    expect(mocks.validatePermissionImport).not.toHaveBeenCalled()
    expect(bodyText()).toContain('内容不是合法的 JSON')
    expect(textareaValue()).toBe('{"permissions":')
  })

  it('渲染问题清单并禁用确认导入（ok=false）', async () => {
    mocks.validatePermissionImport.mockResolvedValue(
      preview({
        ok: false,
        added: 0,
        unchanged: 0,
        total: 0,
        issues: [
          {
            path: 'permissions[2].code',
            field: 'code',
            code: 'invalid_code',
            message: '编码含非法字符，只允许字母、数字与 _.:-',
            line: null,
          },
        ],
        issueTotal: 25,
      }),
    )
    await mountDialog()
    await typeJson(validDoc)

    await clickButton('校验预览')

    expect(mocks.validatePermissionImport).toHaveBeenCalledWith('demo-app', expect.anything(), 'paste')
    expect(bodyText()).toContain('permissions[2].code')
    expect(bodyText()).toContain('编码含非法字符')
    expect(bodyText()).toContain('共 25 条，仅列出前 1 条')
    expect(confirmButton()?.disabled).toBe(true)
  })

  it('只有提示（warnings）时仍允许确认导入', async () => {
    mocks.validatePermissionImport.mockResolvedValue(
      preview({
        warnings: [
          {
            path: 'permissions[4].code',
            field: 'code',
            code: 'prefix_hint',
            message: '编码未以父级编码为前缀，仅提示，不影响导入',
            line: null,
          },
        ],
        warningTotal: 1,
      }),
    )
    await mountDialog()
    await typeJson(validDoc)
    await clickButton('校验预览')

    expect(bodyText()).toContain('提示（不阻断）')
    expect(confirmButton()?.disabled).toBe(false)
  })

  it('上级变更明细把根级显示为「根级」，超过条数给出汇总', async () => {
    mocks.validatePermissionImport.mockResolvedValue(
      preview({
        updated: 25,
        moved: 25,
        movedItems: [
          { code: 'order.read', fromParent: 'order', toParent: '' },
          { code: 'pay.refund', fromParent: '', toParent: 'pay' },
        ],
      }),
    )
    await mountDialog()
    await typeJson(validDoc)
    await clickButton('校验预览')

    expect(bodyText()).toContain('上级将变更 25')
    expect(bodyText()).toContain('order.read')
    expect(bodyText()).toContain('根级')
    expect(bodyText()).toContain('共 25 条，仅列出前 2 条')
  })

  it('校验请求失败后不清空输入', async () => {
    mocks.validatePermissionImport.mockRejectedValue({
      code: 'APP_PERMISSION_IMPORT_INVALID',
      message: '导入内容不合法，请先校验预览',
    })
    await mountDialog()
    await typeJson(validDoc)

    await clickButton('校验预览')

    expect(bodyText()).toContain('请先校验预览')
    expect(textareaValue()).toBe(validDoc)
  })

  it('确认导入遇 409 时提示并自动重跑一次预览', async () => {
    mocks.validatePermissionImport.mockResolvedValue(preview())
    mocks.confirmPermissionImport.mockRejectedValue({ code: 'APP_RESOURCE_STALE', message: '冲突' })
    await mountDialog()
    await typeJson(validDoc)
    await clickButton('校验预览')
    expect(mocks.validatePermissionImport).toHaveBeenCalledTimes(1)

    await clickButton('确认导入')

    expect(mocks.confirmPermissionImport).toHaveBeenCalledTimes(1)
    expect(mocks.validatePermissionImport).toHaveBeenCalledTimes(2)
    expect(bodyText()).toContain('已自动重新校验')
  })

  it('下载示例文档为 3 层 5 条的合法结构', async () => {
    await mountDialog()
    await clickButton('下载示例文档')

    expect(downloadJson).toHaveBeenCalledTimes(1)
    const [name, payload] = vi.mocked(downloadJson).mock.calls[0]!
    expect(name).toBe('permissions-example.json')
    const doc = payload as {
      version: number
      mode: string
      permissions: { code: string; children?: unknown[] }[]
    }
    expect(doc.version).toBe(1)
    expect(doc.mode).toBe('merge')
    expect(doc.permissions[0]?.code).toBe('order')
    expect(doc.permissions[0]?.children).toHaveLength(2)
  })
})

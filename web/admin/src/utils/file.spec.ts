import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { MAX_JSON_BYTES, downloadJson, readJsonFile } from './file'

/**
 * 011：导入 / 导出文件工具。
 * ① 序列化固定 2 空格缩进（与服务端 importable.too_large 的判定口径一致）；
 * ② createObjectURL 只创建一次且必须 revoke；
 * ③ 超过 512KB 的文件在本地就被拒绝，不发请求。
 */
describe('utils/file', () => {
  let created: Blob[]
  let createObjectURL: ReturnType<typeof vi.fn>
  let revokeObjectURL: ReturnType<typeof vi.fn>

  beforeEach(() => {
    created = []
    createObjectURL = vi.fn((blob: Blob) => {
      created.push(blob)
      return 'blob:mock-url'
    })
    revokeObjectURL = vi.fn()
    Object.defineProperty(URL, 'createObjectURL', { value: createObjectURL, writable: true, configurable: true })
    Object.defineProperty(URL, 'revokeObjectURL', { value: revokeObjectURL, writable: true, configurable: true })
    // jsdom 未实现下载导航，会往 stderr 打 "Not implemented: navigation"；这里只验证调用链
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  /** jsdom 的 Blob 没有 .text()，用 FileReader 读取。 */
  function readBlob(blob: Blob): Promise<string> {
    return new Promise((resolve, reject) => {
      const reader = new FileReader()
      reader.onload = () => resolve(String(reader.result ?? ''))
      reader.onerror = () => reject(reader.error)
      reader.readAsText(blob)
    })
  }

  it('downloadJson 按 2 空格缩进序列化并释放 objectURL', async () => {
    const data = { version: 1, mode: 'merge', permissions: [{ code: 'order', name: '订单' }] }

    downloadJson('permissions-demo-20261104.json', data)

    expect(createObjectURL).toHaveBeenCalledTimes(1)
    expect(revokeObjectURL).toHaveBeenCalledTimes(1)
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:mock-url')

    const text = await readBlob(created[0]!)
    expect(text).toBe(JSON.stringify(data, null, 2))
    expect(text).toContain('\n  "version"')
  })

  it('readJsonFile 原样返回文件内容', async () => {
    const file = new File(['{"permissions":[]}'], 'perms.json', { type: 'application/json' })
    await expect(readJsonFile(file)).resolves.toBe('{"permissions":[]}')
  })

  it('超过 512KB 的文件直接报错（文案含上限）', async () => {
    const file = new File(['x'], 'big.json', { type: 'application/json' })
    Object.defineProperty(file, 'size', { value: MAX_JSON_BYTES + 1 })

    await expect(readJsonFile(file)).rejects.toThrow('512KB')
  })
})

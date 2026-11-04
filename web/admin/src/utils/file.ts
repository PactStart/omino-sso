/** 导入 / 导出用的文件工具（011-app-permission-import）。 */

/** 与服务端 maxImportBodyBytes 一致：512KB。 */
export const MAX_JSON_BYTES = 512 * 1024

/** 读取用户选择的 JSON 文件；超过 512KB 直接拒绝（省一次必然 413 的请求）。 */
export function readJsonFile(file: File): Promise<string> {
  if (file.size > MAX_JSON_BYTES) {
    const sizeKb = (file.size / 1024).toFixed(1)
    return Promise.reject(new Error(`文件 ${file.name} 有 ${sizeKb}KB，超过 512KB 上限，请裁剪后重试`))
  }
  // 用 FileReader 而非 file.text()：后者在 jsdom（测试环境）与部分旧浏览器缺失
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(String(reader.result ?? ''))
    reader.onerror = () => reject(new Error(`文件 ${file.name} 读取失败`))
    reader.readAsText(file)
  })
}

/**
 * 触发浏览器下载 JSON。
 *
 * 缩进固定 2 空格：与服务端 `importable.too_large` 的判定口径逐字节一致，
 * 否则会出现"服务端说可回导、下载后被 413 拒绝"的假承诺。
 */
export function downloadJson(name: string, data: unknown): void {
  const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' })
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = name
  anchor.style.display = 'none'
  document.body.appendChild(anchor)
  anchor.click()
  document.body.removeChild(anchor)
  URL.revokeObjectURL(url)
}

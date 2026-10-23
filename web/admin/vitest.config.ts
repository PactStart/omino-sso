import { fileURLToPath, URL } from 'node:url'

import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vitest/config'

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  test: {
    environment: 'jsdom',
    // jsdom 默认不实现 requestAnimationFrame；element-plus 的表格/下拉在 debounce 回调里调它，
    // 用例结束（环境已拆除）后才触发，会变成 "Unhandled Errors" 让 `make test` 偶发红。
    // pretendToBeVisual 提供 rAF 实现，从根上消除这类 teardown 期异常（口径与 web/portal 一致）。
    environmentOptions: { jsdom: { pretendToBeVisual: true } },
    include: ['src/**/*.spec.ts'],
    restoreMocks: true,
  },
})

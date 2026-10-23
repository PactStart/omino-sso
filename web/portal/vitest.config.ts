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
    // jsdom 默认不实现 requestAnimationFrame；element-plus 的表格在 debounce 回调里调它。
    // pretendToBeVisual 提供 rAF 实现，但只是缓解：真正管住偶发红的是 setup.ts 里的
    // enableAutoUnmount —— 用例结束先卸载组件，debounce 尾调用才不会落到环境拆除之后。
    environmentOptions: { jsdom: { pretendToBeVisual: true } },
    setupFiles: ['./src/test/setup.ts'],
    include: ['src/**/*.spec.ts'],
    restoreMocks: true,
  },
})

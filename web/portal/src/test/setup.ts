import { enableAutoUnmount } from '@vue/test-utils'
import { afterEach } from 'vitest'

// element-plus 的表格把 doLayout 包在 lodash debounce 里，若用例结束时不卸载组件，
// 尾调用会在 vitest 拆掉 jsdom 环境**之后**才执行，此时 requestAnimationFrame 已随环境消失，
// 于是抛出 "Uncaught Exception: requestAnimationFrame is not defined"，整轮 run 报红
// （`make test` / pre-commit 门禁偶发失败，见 Members.spec.ts 的表格用例）。
// 统一在每个用例结束后卸载组件，让 EP 的 onBeforeUnmount 清掉挂起的定时器。
enableAutoUnmount(afterEach)

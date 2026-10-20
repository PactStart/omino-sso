import { reactive } from 'vue'

export type ToastType = 'success' | 'error' | 'info'

export interface ToastItem {
  id: number
  type: ToastType
  text: string
}

const state = reactive<{ items: ToastItem[] }>({ items: [] })
let seq = 0

function dismiss(id: number) {
  const index = state.items.findIndex((item) => item.id === id)
  if (index >= 0) state.items.splice(index, 1)
}

function push(type: ToastType, text: string, duration = 3200) {
  const id = ++seq
  state.items.push({ id, type, text })
  window.setTimeout(() => dismiss(id), duration)
}

export function useToast() {
  return {
    items: state.items,
    dismiss,
    success: (text: string) => push('success', text),
    error: (text: string) => push('error', text),
    info: (text: string) => push('info', text),
  }
}

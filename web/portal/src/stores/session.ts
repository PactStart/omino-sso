import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { useRouter } from 'vue-router'
import { getMe, logout as logoutApi } from '@/api'
import { errorCode } from '@/api/client'
import type { Me } from '@/api/types'

/** 会话探测失败的两种性质：服务不可用 vs 未登录，决定跳 /offline 还是 /login */
export type ProbeFailure = 'unavailable' | 'unauth' | null

const UNAVAILABLE_CODES = ['NETWORK_ERROR', 'NETWORK_TIMEOUT', 'DEPENDENCY_UNAVAILABLE', 'INTERNAL_ERROR']

/**
 * 会话/用户唯一来源 = GET /api/v1/me（PRD §3.1）。
 * 只存内存，不写 localStorage，刷新即重新探测。
 */
export const useSessionStore = defineStore('session', () => {
  const user = ref<Me | null>(null)
  const loading = ref(false)
  const settled = ref(false)
  const probeFailure = ref<ProbeFailure>(null)
  let inflight: Promise<Me | null> | null = null

  const isAdmin = computed(() => user.value?.roles.includes('admin') ?? false)

  async function fetchMe(opts: { force?: boolean; silent?: boolean } = {}): Promise<Me | null> {
    if (settled.value && !opts.force) return user.value
    if (inflight) return inflight

    loading.value = true
    inflight = getMe({ silent: opts.silent ?? true })
      .then((me) => {
        user.value = me
        probeFailure.value = null
        return me
      })
      .catch((e) => {
        user.value = null
        probeFailure.value = UNAVAILABLE_CODES.includes(errorCode(e)) ? 'unavailable' : 'unauth'
        return null
      })
      .finally(() => {
        loading.value = false
        settled.value = true
        inflight = null
      })

    return inflight
  }

  function setUser(me: Me) {
    user.value = me
    settled.value = true
  }

  function reset() {
    user.value = null
    settled.value = false
    probeFailure.value = null
    inflight = null
  }

  async function signOut(): Promise<void> {
    try {
      await logoutApi()
    } catch {
      // 登出失败也要清空本地态并跳登录（PRD §4 P5）
    } finally {
      reset()
    }
  }

  return { user, loading, settled, probeFailure, isAdmin, fetchMe, setUser, reset, signOut }
})

/** 统一登出 + 跳转，供布局与页面复用 */
export function useSignOut() {
  const session = useSessionStore()
  const router = useRouter()
  return async () => {
    await session.signOut()
    await router.replace({ path: '/login' })
  }
}

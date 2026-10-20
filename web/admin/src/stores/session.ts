import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { getMe, logout as logoutApi } from '@/api'
import { errorCode } from '@/api/client'
import type { Me } from '@/api/types'

/** 会话探测失败的两种性质：服务不可用 vs 未登录 */
export type ProbeFailure = 'unavailable' | 'unauth' | null

const UNAVAILABLE_CODES = ['NETWORK_ERROR', 'NETWORK_TIMEOUT', 'DEPENDENCY_UNAVAILABLE', 'INTERNAL_ERROR']

/** 会话/用户唯一来源 = GET /api/v1/me，仅存内存（PRD §3.1） */
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
      // 失败也按已登出处理
    } finally {
      reset()
    }
  }

  return { user, loading, settled, probeFailure, isAdmin, fetchMe, reset, signOut }
})

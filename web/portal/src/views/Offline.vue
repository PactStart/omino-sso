<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ADMIN_HOME } from '@/config'
import { useSessionStore } from '@/stores/session'

const route = useRoute()
const router = useRouter()
const session = useSessionStore()

const retrying = ref(false)
const failed = ref(false)

const nextPath = computed(() => {
  const next = route.query.next
  if (typeof next !== 'string') return ''
  if (!next.startsWith('/') || next.startsWith('/offline')) return ''
  return next
})

async function retry() {
  retrying.value = true
  failed.value = false
  const me = await session.fetchMe({ force: true, silent: true })
  retrying.value = false

  if (!me) {
    // 服务恢复了但会话确实已失效 -> 该去登录页
    if (session.probeFailure === 'unauth') {
      await router.replace({
        path: '/login',
        query: nextPath.value ? { next: nextPath.value } : {},
      })
      return
    }
    failed.value = true
    return
  }
  if (nextPath.value) {
    await router.replace(nextPath.value)
    return
  }
  if (me.roles.includes('admin')) {
    window.location.assign(ADMIN_HOME)
    return
  }
  await router.replace('/account/profile')
}
</script>

<template>
  <div class="flex min-h-screen items-center justify-center bg-[#f5f7fb] px-4">
    <div class="w-full max-w-md rounded-card border border-ink-200 bg-white p-8 text-center">
      <div
        class="mx-auto flex h-12 w-12 items-center justify-center rounded-full bg-amber-50 text-amber-600"
        aria-hidden="true"
      >
        <svg viewBox="0 0 24 24" class="h-6 w-6" fill="none" stroke="currentColor" stroke-width="2">
          <path d="M12 9v4m0 4h.01M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0Z" />
        </svg>
      </div>

      <h1 class="mt-4 text-lg font-semibold text-ink-800">服务暂时不可用</h1>
      <p class="mt-2 text-sm leading-6 text-ink-600">
        暂时无法连接认证服务，你的登录状态没有丢失。请稍后重试。
      </p>
      <p v-if="failed" class="mt-2 text-xs text-rose-600">仍然无法连接，请稍后再试或联系管理员。</p>

      <div class="mt-6 flex items-center justify-center gap-3">
        <button
          type="button"
          class="flex items-center gap-2 rounded-lg bg-brand-600 px-4 py-2 text-sm font-medium text-white transition hover:bg-brand-700 disabled:cursor-not-allowed disabled:opacity-60"
          :disabled="retrying"
          @click="retry"
        >
          <span
            v-if="retrying"
            class="h-4 w-4 animate-spin rounded-full border-2 border-white/40 border-t-white"
            aria-hidden="true"
          />
          重试
        </button>
        <RouterLink
          to="/login"
          class="rounded-lg border border-ink-200 px-4 py-2 text-sm text-ink-600 transition hover:bg-ink-50"
        >
          重新登录
        </RouterLink>
      </div>
    </div>
  </div>
</template>

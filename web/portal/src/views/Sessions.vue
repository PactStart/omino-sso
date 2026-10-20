<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import PortalLayout from '@/layouts/PortalLayout.vue'
import EmptyState from '@/components/EmptyState.vue'
import ErrorState from '@/components/ErrorState.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import { getMySessions, revokeSession } from '@/api'
import { errorCode, errorMessage } from '@/api/client'
import type { SessionItem } from '@/api/types'
import { formatTime } from '@/utils/format'
import { useSessionStore, useSignOut } from '@/stores/session'
import { useToast } from '@/composables/useToast'

const toast = useToast()
const session = useSessionStore()
const signOut = useSignOut()

const sessions = ref<SessionItem[]>([])
const loading = ref(true)
const error = ref('')
const target = ref<SessionItem | null>(null)
const revoking = ref(false)

const currentSession = computed(() => sessions.value.find((item) => item.current) ?? null)

function displayUA(ua: string | null): string {
  if (!ua) return '未知设备'
  return ua.length > 60 ? `${ua.slice(0, 60)}…` : ua
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    sessions.value = await getMySessions()
    // 顺带同步会话态，避免本地用户信息与服务端不一致
    void session.fetchMe({ force: true, silent: true })
  } catch (e) {
    error.value = errorMessage(e)
  } finally {
    loading.value = false
  }
}

function askRevoke(item: SessionItem) {
  target.value = item
}

async function confirmRevoke() {
  const item = target.value
  if (!item) return
  revoking.value = true
  try {
    await revokeSession(item.sid)
    toast.success('已下线')
    target.value = null
    await load()
  } catch (e) {
    if (errorCode(e) === 'NOT_FOUND') {
      toast.error('该会话已失效')
      target.value = null
      await load()
    } else {
      toast.error(errorMessage(e))
    }
  } finally {
    revoking.value = false
  }
}

async function signOutCurrent() {
  await signOut()
}

onMounted(load)
</script>

<template>
  <PortalLayout>
    <div class="mb-6 flex items-end justify-between gap-4">
      <div>
        <h1 class="text-xl font-semibold text-ink-800">登录设备</h1>
        <p class="mt-1 text-sm text-ink-400">
          共 {{ sessions.length }} 个活跃会话；修改密码会退出全部设备
        </p>
      </div>
      <button
        type="button"
        class="rounded-lg border border-ink-200 bg-white px-3 py-1.5 text-sm text-ink-600 transition hover:border-brand-300 hover:text-brand-700"
        @click="load"
      >
        刷新
      </button>
    </div>

    <div class="overflow-hidden rounded-card border border-ink-200 bg-white">
      <!-- 加载中 -->
      <div v-if="loading" class="space-y-4 p-6">
        <div v-for="i in 3" :key="i" class="flex items-center gap-6">
          <div class="skeleton h-4 w-1/3" />
          <div class="skeleton h-4 w-20" />
          <div class="skeleton h-4 w-28" />
          <div class="skeleton ml-auto h-7 w-16" />
        </div>
      </div>

      <!-- 错误 -->
      <ErrorState v-else-if="error" :message="error" @retry="load" />

      <!-- 空态 -->
      <EmptyState
        v-else-if="sessions.length === 0"
        title="暂无登录记录"
        description="当前账号没有其他活跃会话。"
      />

      <!-- 列表 -->
      <div v-else class="overflow-x-auto">
        <table class="w-full min-w-[760px] text-left text-sm">
          <thead>
            <tr class="border-b border-ink-200 bg-ink-50/60 text-xs text-ink-400">
              <th class="px-4 py-3 font-medium">设备</th>
              <th class="px-4 py-3 font-medium">IP</th>
              <th class="px-4 py-3 font-medium">登录时间</th>
              <th class="px-4 py-3 font-medium">最近活跃</th>
              <th class="px-4 py-3 font-medium">状态</th>
              <th class="px-4 py-3 text-right font-medium">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="item in sessions"
              :key="item.sid"
              class="border-b border-ink-100 last:border-0 hover:bg-ink-50/40"
            >
              <td class="max-w-[280px] px-4 py-3">
                <span :title="item.userAgent ?? '未知设备'" class="block truncate text-ink-800">
                  {{ displayUA(item.userAgent) }}
                </span>
              </td>
              <td class="px-4 py-3 font-mono text-xs text-ink-600">{{ item.ip || '-' }}</td>
              <td class="px-4 py-3 text-ink-600">{{ formatTime(item.createdAt) }}</td>
              <td class="px-4 py-3 text-ink-600">{{ formatTime(item.lastSeenAt) }}</td>
              <td class="px-4 py-3">
                <span
                  class="rounded-full px-2.5 py-0.5 text-xs font-medium ring-1"
                  :class="
                    item.current
                      ? 'bg-emerald-50 text-emerald-700 ring-emerald-200'
                      : 'bg-ink-50 text-ink-600 ring-ink-200'
                  "
                >
                  {{ item.current ? '本机' : '其他设备' }}
                </span>
              </td>
              <td class="px-4 py-3 text-right">
                <button
                  v-if="item.current"
                  type="button"
                  class="rounded-lg border border-ink-200 px-3 py-1.5 text-xs text-ink-600 transition hover:border-rose-300 hover:text-rose-600"
                  @click="signOutCurrent"
                >
                  退出登录
                </button>
                <button
                  v-else
                  type="button"
                  class="rounded-lg border border-ink-200 px-3 py-1.5 text-xs text-ink-600 transition hover:border-rose-300 hover:text-rose-600"
                  @click="askRevoke(item)"
                >
                  下线
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <p v-if="currentSession && !error" class="mt-4 text-xs text-ink-400">
      当前设备登录时间：{{ formatTime(currentSession.createdAt) }}
    </p>

    <ConfirmDialog
      :open="target !== null"
      title="让该设备下线？"
      message="下线后该设备需要重新登录才能继续使用。"
      confirm-text="下线"
      danger
      :loading="revoking"
      @confirm="confirmRevoke"
      @cancel="target = null"
    />
  </PortalLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import PortalLayout from '@/layouts/PortalLayout.vue'
import EmptyState from '@/components/EmptyState.vue'
import ErrorState from '@/components/ErrorState.vue'
import { getMyApps } from '@/api'
import { errorMessage } from '@/api/client'
import type { AppItem } from '@/api/types'
import { initialOf, formatTime } from '@/utils/format'
import { useToast } from '@/composables/useToast'

const toast = useToast()
const router = useRouter()

const apps = ref<AppItem[]>([])
const loading = ref(true)
const error = ref('')
const keyword = ref('')
const failedLogos = ref<string[]>([])

const filtered = computed(() => {
  const k = keyword.value.trim().toLowerCase()
  if (!k) return apps.value
  return apps.value.filter(
    (app) =>
      app.name.toLowerCase().includes(k) || (app.description ?? '').toLowerCase().includes(k),
  )
})

function hasLogo(app: AppItem): boolean {
  return Boolean(app.logoUrl) && !failedLogos.value.includes(app.clientId)
}

function onLogoError(clientId: string) {
  failedLogos.value = [...failedLogos.value, clientId]
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    apps.value = await getMyApps()
  } catch (e) {
    error.value = errorMessage(e)
  } finally {
    loading.value = false
  }
}

function open(app: AppItem) {
  if (!app.homeUrl) {
    toast.info('该应用未配置入口地址')
    return
  }
  window.open(app.homeUrl, '_blank', 'noopener,noreferrer')
}

/**
 * 进入该应用的门户管理页（006）。
 * 只有 isAppAdmin 的应用才渲染该入口；服务端仍会再校验一次（FR-012），
 * 因此手改地址也进不去无权限的应用。
 */
function manage(app: AppItem) {
  void router.push(`/account/apps/${encodeURIComponent(app.clientId)}/manage`)
}

onMounted(load)
</script>

<template>
  <PortalLayout>
    <div class="mb-6 flex flex-wrap items-end justify-between gap-4">
      <div>
        <h1 class="text-xl font-semibold text-ink-800">我的应用</h1>
        <p class="mt-1 text-sm text-ink-400">共 {{ apps.length }} 个应用</p>
      </div>

      <div class="relative w-full sm:w-64">
        <input
          v-model="keyword"
          type="search"
          placeholder="搜索应用名称"
          class="w-full rounded-lg border border-ink-200 bg-white px-3.5 py-2 text-sm text-ink-800 outline-none transition placeholder:text-ink-400/70 focus:border-brand-400 focus:ring-4 focus:ring-brand-50"
        />
      </div>
    </div>

    <!-- 加载中 -->
    <div v-if="loading" class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
      <div v-for="i in 6" :key="i" class="rounded-card border border-ink-200 bg-white p-5">
        <div class="flex items-center gap-3">
          <div class="skeleton h-11 w-11 rounded-xl" />
          <div class="flex-1 space-y-2">
            <div class="skeleton h-4 w-24" />
            <div class="skeleton h-3 w-16" />
          </div>
        </div>
        <div class="skeleton mt-4 h-3 w-full" />
        <div class="skeleton mt-2 h-3 w-2/3" />
      </div>
    </div>

    <!-- 错误 -->
    <div v-else-if="error" class="rounded-card border border-ink-200 bg-white">
      <ErrorState :message="error" @retry="load" />
    </div>

    <!-- 空态 -->
    <div v-else-if="apps.length === 0" class="rounded-card border border-ink-200 bg-white">
      <EmptyState
        title="暂无可访问的应用"
        description="当前账号还没有被授权访问任何内部系统，请联系管理员开通。"
      />
    </div>

    <!-- 搜索无结果 -->
    <div v-else-if="filtered.length === 0" class="rounded-card border border-ink-200 bg-white">
      <EmptyState title="没有匹配的应用" :description="`没有找到与「${keyword}」相关的应用`">
        <button
          type="button"
          class="rounded-lg border border-ink-200 px-3.5 py-1.5 text-sm text-ink-600 transition hover:border-brand-300 hover:text-brand-700"
          @click="keyword = ''"
        >
          清除搜索
        </button>
      </EmptyState>
    </div>

    <!-- 列表 -->
    <div v-else class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
      <article
        v-for="app in filtered"
        :key="app.clientId"
        class="group flex flex-col rounded-card border border-ink-200 bg-white p-5 transition hover:border-brand-200 hover:shadow-[0_6px_20px_rgba(16,24,40,0.06)]"
      >
        <div class="flex items-center gap-3">
          <img
            v-if="hasLogo(app)"
            :src="app.logoUrl"
            :alt="app.name"
            class="h-11 w-11 rounded-xl object-cover ring-1 ring-ink-100"
            @error="onLogoError(app.clientId)"
          />
          <div
            v-else
            class="flex h-11 w-11 items-center justify-center rounded-xl bg-brand-50 text-base font-semibold text-brand-700 ring-1 ring-brand-100"
            aria-hidden="true"
          >
            {{ initialOf(app.name) }}
          </div>

          <div class="min-w-0 flex-1">
            <h2 class="truncate text-[15px] font-medium text-ink-800">{{ app.name }}</h2>
            <p class="mt-0.5 truncate font-mono text-xs text-ink-400">{{ app.clientId }}</p>
          </div>
        </div>

        <p class="mt-3 line-clamp-2 min-h-[40px] text-sm leading-5 text-ink-600">
          {{ app.description || '-' }}
        </p>

        <div class="mt-4 flex items-center justify-between border-t border-ink-100 pt-3">
          <span class="text-xs text-ink-400">
            {{ app.lastAccessAt ? `最近访问 ${formatTime(app.lastAccessAt)}` : '尚未访问' }}
          </span>

          <div class="flex items-center gap-2">
            <!-- 应用管理员专属入口，位于「打开」左侧；普通成员不渲染（FR-004） -->
            <button
              v-if="app.isAppAdmin"
              type="button"
              class="rounded-lg border border-brand-200 px-3 py-1.5 text-sm font-medium text-brand-700 transition hover:bg-brand-50"
              title="管理该应用（成员 / 角色 / 权限）"
              @click="manage(app)"
            >
              管理
            </button>
            <button
              type="button"
              class="rounded-lg px-3 py-1.5 text-sm font-medium transition"
              :class="
                app.homeUrl
                  ? 'bg-brand-50 text-brand-700 hover:bg-brand-100'
                  : 'cursor-not-allowed bg-ink-50 text-ink-400'
              "
              :title="app.homeUrl ? '在新标签页打开' : '该应用未配置入口地址'"
              @click="open(app)"
            >
              打开
            </button>
          </div>
        </div>
      </article>
    </div>
  </PortalLayout>
</template>

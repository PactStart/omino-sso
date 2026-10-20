<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { useSessionStore, useSignOut } from '@/stores/session'
import { ADMIN_HOME } from '@/config'
import { initialOf } from '@/utils/format'

const session = useSessionStore()
const signOut = useSignOut()
const route = useRoute()

const navs = [
  { to: '/account/profile', label: '个人信息' },
  { to: '/account/apps', label: '我的应用' },
  { to: '/account/sessions', label: '登录设备' },
  { to: '/account/password', label: '修改密码' },
]

const user = computed(() => session.user)
const showAdminEntry = computed(() => session.isAdmin)

function goAdmin() {
  window.location.assign(ADMIN_HOME)
}
</script>

<template>
  <div class="min-h-full bg-[#f5f7fb]">
    <header class="sticky top-0 z-30 border-b border-ink-200 bg-white/90 backdrop-blur">
      <div class="mx-auto flex h-16 max-w-5xl items-center gap-6 px-6">
        <RouterLink to="/account/profile" class="flex items-center gap-2.5">
          <span
            class="flex h-9 w-9 items-center justify-center rounded-xl bg-brand-600 text-sm font-semibold text-white"
            >SSO</span
          >
          <span class="text-[15px] font-semibold text-ink-800">统一身份认证</span>
        </RouterLink>

        <nav class="hidden items-center gap-1 md:flex">
          <RouterLink
            v-for="item in navs"
            :key="item.to"
            :to="item.to"
            class="rounded-lg px-3 py-2 text-sm transition"
            :class="
              route.path === item.to
                ? 'bg-brand-50 font-medium text-brand-700'
                : 'text-ink-600 hover:bg-ink-50 hover:text-ink-800'
            "
          >
            {{ item.label }}
          </RouterLink>
        </nav>

        <div class="ml-auto flex items-center gap-3">
          <button
            v-if="showAdminEntry"
            type="button"
            class="hidden rounded-lg border border-ink-200 px-3 py-1.5 text-sm text-ink-600 transition hover:border-brand-300 hover:text-brand-700 sm:block"
            @click="goAdmin"
          >
            管理后台
          </button>

          <div class="flex items-center gap-2">
            <span
              class="flex h-8 w-8 items-center justify-center rounded-full bg-brand-100 text-xs font-semibold text-brand-700"
            >
              {{ initialOf(user?.displayName) }}
            </span>
            <span class="hidden text-sm text-ink-600 sm:block">{{ user?.displayName || '-' }}</span>
          </div>

          <button
            type="button"
            class="rounded-lg px-3 py-1.5 text-sm text-ink-400 transition hover:bg-ink-50 hover:text-rose-600"
            @click="signOut"
          >
            退出登录
          </button>
        </div>
      </div>

      <!-- 小屏导航 -->
      <nav class="flex items-center gap-1 overflow-x-auto border-t border-ink-100 px-4 py-2 md:hidden">
        <RouterLink
          v-for="item in navs"
          :key="item.to"
          :to="item.to"
          class="shrink-0 rounded-lg px-3 py-1.5 text-sm transition"
          :class="
            route.path === item.to
              ? 'bg-brand-50 font-medium text-brand-700'
              : 'text-ink-600 hover:bg-ink-50'
          "
        >
          {{ item.label }}
        </RouterLink>
      </nav>
    </header>

    <main class="mx-auto max-w-5xl px-6 py-8">
      <slot />
    </main>
  </div>
</template>

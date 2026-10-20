<script setup lang="ts">
import { useRouter } from 'vue-router'

const router = useRouter()
</script>

<template>
  <div class="flex min-h-full items-center justify-center px-6 py-16">
    <div class="w-full max-w-md rounded-card border border-ink-200 bg-white p-10 text-center shadow-sm">
      <p class="text-6xl font-bold tracking-tight text-ink-200">404</p>
      <h1 class="mt-4 text-lg font-semibold text-ink-800">页面不存在</h1>
      <p class="mt-2 text-sm text-ink-400">你访问的页面可能已被移除，或地址输入有误。</p>
      <div class="mt-7 flex justify-center gap-3">
        <button
          type="button"
          class="rounded-lg bg-brand-600 px-4 py-2 text-sm font-medium text-white transition hover:bg-brand-700"
          @click="router.replace('/')"
        >
          返回首页
        </button>
        <button
          type="button"
          class="rounded-lg border border-ink-200 px-4 py-2 text-sm text-ink-600 transition hover:border-brand-300 hover:text-brand-700"
          @click="router.replace('/login')"
        >
          重新登录
        </button>
      </div>
    </div>
  </div>
</template>

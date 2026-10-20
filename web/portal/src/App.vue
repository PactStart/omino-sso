<script setup lang="ts">
import { RouterView } from 'vue-router'
import { useToast } from '@/composables/useToast'

const { items, dismiss } = useToast()
</script>

<template>
  <RouterView />

  <div class="pointer-events-none fixed inset-x-0 top-5 z-50 flex flex-col items-center gap-2 px-4">
    <TransitionGroup name="toast">
      <div
        v-for="item in items"
        :key="item.id"
        class="pointer-events-auto flex max-w-md items-start gap-2 rounded-xl px-4 py-3 text-sm shadow-lg ring-1"
        :class="{
          'bg-emerald-50 text-emerald-800 ring-emerald-200': item.type === 'success',
          'bg-rose-50 text-rose-800 ring-rose-200': item.type === 'error',
          'bg-white text-ink-800 ring-ink-200': item.type === 'info',
        }"
      >
        <span class="leading-5">{{ item.text }}</span>
        <button
          type="button"
          class="ml-1 shrink-0 opacity-50 transition hover:opacity-100"
          aria-label="关闭提示"
          @click="dismiss(item.id)"
        >
          ✕
        </button>
      </div>
    </TransitionGroup>
  </div>
</template>

<style scoped>
.toast-enter-active,
.toast-leave-active {
  transition:
    opacity 0.2s ease,
    transform 0.2s ease;
}
.toast-enter-from,
.toast-leave-to {
  opacity: 0;
  transform: translateY(-6px);
}
</style>

<script setup lang="ts">
withDefaults(
  defineProps<{
    open: boolean
    title: string
    message?: string
    confirmText?: string
    cancelText?: string
    danger?: boolean
    loading?: boolean
  }>(),
  { message: '', confirmText: '确定', cancelText: '取消', danger: false, loading: false },
)

const emit = defineEmits<{ confirm: []; cancel: [] }>()
</script>

<template>
  <Teleport to="body">
    <Transition name="fade">
      <div
        v-if="open"
        class="fixed inset-0 z-40 flex items-center justify-center bg-ink-800/30 px-4"
        @click.self="emit('cancel')"
      >
        <div class="w-full max-w-sm rounded-card bg-white p-6 shadow-xl">
          <h2 class="text-base font-semibold text-ink-800">{{ title }}</h2>
          <p v-if="message" class="mt-2 text-sm leading-6 text-ink-600">{{ message }}</p>
          <div class="mt-6 flex justify-end gap-3">
            <button
              type="button"
              class="rounded-lg border border-ink-200 px-4 py-2 text-sm text-ink-600 transition hover:bg-ink-50"
              :disabled="loading"
              @click="emit('cancel')"
            >
              {{ cancelText }}
            </button>
            <button
              type="button"
              class="flex items-center gap-2 rounded-lg px-4 py-2 text-sm font-medium text-white transition disabled:cursor-not-allowed disabled:opacity-60"
              :class="danger ? 'bg-rose-600 hover:bg-rose-700' : 'bg-brand-600 hover:bg-brand-700'"
              :disabled="loading"
              @click="emit('confirm')"
            >
              <span
                v-if="loading"
                class="h-3.5 w-3.5 animate-spin rounded-full border-2 border-white/40 border-t-white"
                aria-hidden="true"
              />
              {{ confirmText }}
            </button>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.15s ease;
}
.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}
</style>

<script setup lang="ts">
withDefaults(
  defineProps<{
    message: string
    title?: string
    retryText?: string
  }>(),
  { title: '加载失败', retryText: '重试' },
)

const emit = defineEmits<{ retry: [] }>()
</script>

<template>
  <div class="flex flex-col items-center justify-center px-6 py-12 text-center">
    <div
      class="mb-4 flex h-14 w-14 items-center justify-center rounded-2xl bg-rose-50 text-2xl text-rose-500"
      aria-hidden="true"
    >
      !
    </div>
    <p class="text-base font-medium text-ink-800">{{ title }}</p>
    <p class="mt-1.5 max-w-md text-sm break-words text-ink-400">{{ message }}</p>
    <button
      type="button"
      class="mt-5 rounded-lg bg-brand-600 px-4 py-2 text-sm font-medium text-white transition hover:bg-brand-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-brand-300"
      @click="emit('retry')"
    >
      {{ retryText }}
    </button>
  </div>
</template>

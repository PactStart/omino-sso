<script setup lang="ts">
withDefaults(
  defineProps<{
    title: string
    description?: string
  }>(),
  { description: '' },
)
</script>

<template>
  <div class="flex flex-col items-center justify-center px-6 py-14 text-center">
    <div
      class="mb-4 flex h-14 w-14 items-center justify-center rounded-2xl bg-ink-100 text-2xl text-ink-400"
      aria-hidden="true"
    >
      ◍
    </div>
    <p class="text-base font-medium text-ink-800">{{ title }}</p>
    <p v-if="description" class="mt-1.5 max-w-sm text-sm text-ink-400">{{ description }}</p>
    <div v-if="$slots.default" class="mt-5">
      <slot />
    </div>
  </div>
</template>

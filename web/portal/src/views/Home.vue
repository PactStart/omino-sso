<script setup lang="ts">
// 路由守卫会在渲染前决定落地页，这里只作为极端情况的兜底
</script>

<template>
  <div class="flex min-h-full items-center justify-center gap-3 text-sm text-ink-400">
    <span
      class="h-4 w-4 animate-spin rounded-full border-2 border-ink-200 border-t-brand-600"
      aria-hidden="true"
    />
    正在进入…
  </div>
</template>

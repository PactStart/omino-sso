<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useSessionStore } from '@/stores/session'
import { portalLoginUrl } from '@/config'

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
    // 服务恢复了但会话确实已失效 -> 去门户登录（可带 next 回跳）
    if (session.probeFailure === 'unauth') {
      window.location.assign(portalLoginUrl(nextPath.value || undefined))
      return
    }
    failed.value = true
    return
  }
  if (nextPath.value) {
    await router.replace(nextPath.value)
    return
  }
  await router.replace('/admin/users')
}

function relogin() {
  window.location.assign(portalLoginUrl())
}
</script>

<template>
  <div class="fullscreen-center">
    <el-result
      icon="warning"
      title="服务暂时不可用"
      sub-title="暂时无法连接认证服务，你的登录状态没有丢失，请稍后重试。"
    >
      <template #extra>
        <p v-if="failed" class="retry-hint">仍然无法连接，请稍后再试或联系管理员。</p>
        <el-button type="primary" :loading="retrying" @click="retry">重试</el-button>
        <el-button link type="primary" @click="relogin">重新登录</el-button>
      </template>
    </el-result>
  </div>
</template>

<style scoped>
.fullscreen-center {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 100vh;
  background: #fff;
}

.retry-hint {
  margin: 0 0 12px;
  font-size: 13px;
  color: #d14343;
}
</style>

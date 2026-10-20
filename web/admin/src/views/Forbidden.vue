<script setup lang="ts">
import { useRouter } from 'vue-router'
import { portalLoginUrl, PORTAL_HOME } from '@/config'

const router = useRouter()

function goPortal() {
  window.location.assign(PORTAL_HOME)
}

function relogin() {
  window.location.assign(portalLoginUrl())
}
</script>

<template>
  <div class="fullscreen-center">
    <el-result icon="warning" title="403 无访问权限" sub-title="当前账号不是管理员，无法访问管理后台。">
      <template #extra>
        <el-button type="primary" @click="router.replace('/')">返回首页</el-button>
        <el-button @click="goPortal">返回门户</el-button>
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
</style>

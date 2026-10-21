<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { useSessionStore } from '@/stores/session'
import { PORTAL_HOME, portalLoginUrl } from '@/config'

const route = useRoute()
const session = useSessionStore()

// 应用子页面（/admin/clients/:id/*）仍高亮「应用管理」一级菜单
const activeMenu = computed(() =>
  route.path.startsWith('/admin/clients') ? '/admin/clients' : route.path,
)
const displayName = computed(() => session.user?.displayName ?? '-')
const initial = computed(() => (displayName.value || '?').slice(0, 1))
const avatarUrl = computed(() => session.user?.avatarUrl ?? '')

// 管理后台最小宽度 1280（PRD §4 A1）
const windowWidth = ref(window.innerWidth)
function onResize() {
  windowWidth.value = window.innerWidth
}
onMounted(() => window.addEventListener('resize', onResize))
onBeforeUnmount(() => window.removeEventListener('resize', onResize))
const tooNarrow = computed(() => windowWidth.value < 1280)

async function handleCommand(command: string) {
  if (command === 'logout') {
    await session.signOut()
    window.location.replace(portalLoginUrl())
    return
  }
  if (command === 'portal') {
    window.location.assign(PORTAL_HOME)
  }
}
</script>

<template>
  <div class="admin-shell">
    <header class="admin-header">
      <div class="brand">
        <span class="brand-mark">SSO</span>
        <span class="brand-text">管理后台</span>
      </div>

      <el-menu
        :default-active="activeMenu"
        mode="horizontal"
        router
        :ellipsis="false"
        class="admin-menu"
      >
        <el-menu-item index="/admin/users">用户管理</el-menu-item>
        <el-menu-item index="/admin/clients">应用管理</el-menu-item>
        <el-menu-item index="/admin/audit">审计日志</el-menu-item>
        <el-menu-item index="/admin/files">文件管理</el-menu-item>
        <el-menu-item index="/admin/send-records">发送记录</el-menu-item>
      </el-menu>

      <div class="header-right">
        <el-button link type="primary" @click="handleCommand('portal')">返回门户</el-button>
        <el-dropdown trigger="click" @command="handleCommand">
          <span class="user-trigger">
            <img v-if="avatarUrl" :src="avatarUrl" class="avatar avatar-img" alt="头像" />
            <span v-else class="avatar">{{ initial }}</span>
            <span class="user-name">{{ displayName }}</span>
          </span>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="portal">返回门户</el-dropdown-item>
              <el-dropdown-item command="logout" divided>退出登录</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </div>
    </header>

    <main class="admin-main">
      <slot />
    </main>

    <div v-if="tooNarrow" class="narrow-mask">
      <el-result icon="warning" title="请使用桌面端" sub-title="管理后台最小支持宽度为 1280px，请调整窗口尺寸后重试。" />
    </div>
  </div>
</template>

<style scoped>
.admin-shell {
  min-height: 100%;
  background: var(--app-bg);
}

.admin-header {
  position: sticky;
  top: 0;
  z-index: 20;
  display: flex;
  align-items: center;
  gap: 24px;
  height: 56px;
  padding: 0 20px;
  background: #fff;
  border-bottom: 1px solid var(--app-border);
}

.brand {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-shrink: 0;
}

.brand-mark {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border-radius: 8px;
  background: var(--el-color-primary);
  color: #fff;
  font-size: 11px;
  font-weight: 600;
}

.brand-text {
  font-size: 14px;
  font-weight: 600;
  color: var(--app-text);
}

.admin-menu {
  border-bottom: none;
  height: 56px;
}

.admin-menu :deep(.el-menu-item) {
  height: 56px;
  line-height: 56px;
  font-size: 14px;
}

.header-right {
  display: flex;
  align-items: center;
  gap: 16px;
  margin-left: auto;
}

.user-trigger {
  display: flex;
  align-items: center;
  gap: 8px;
  cursor: pointer;
  outline: none;
}

.avatar {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border-radius: 50%;
  background: var(--el-color-primary-light-9);
  color: var(--el-color-primary);
  font-size: 12px;
  font-weight: 600;
}

.avatar-img {
  object-fit: cover;
}

.user-name {
  font-size: 13px;
  color: var(--app-text);
}

.admin-main {
  padding: 20px;
  min-width: 1280px;
}

.narrow-mask {
  position: fixed;
  inset: 0;
  z-index: 100;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--app-bg);
}
</style>

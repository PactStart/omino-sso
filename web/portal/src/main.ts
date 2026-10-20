import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import { router } from './router'
import './styles/tokens.css'
import { AUTH_EXPIRED_EVENT, FORBIDDEN_EVENT } from './api/client'
import { useSessionStore } from './stores/session'
import { useToast } from './composables/useToast'

const app = createApp(App)
const pinia = createPinia()
app.use(pinia)
app.use(router)

const session = useSessionStore()
const toast = useToast()

// 任意接口 401 UNAUTHENTICATED -> 清空内存态并回登录页（PRD §3.1）
window.addEventListener(AUTH_EXPIRED_EVENT, () => {
  session.reset()
  const current = router.currentRoute.value
  if (current.path === '/login') return
  toast.error('登录状态已失效，请重新登录')
  void router.replace({ path: '/login', query: { next: current.fullPath } })
})

// 任意接口 403 FORBIDDEN -> /403（PRD §3.4）
window.addEventListener(FORBIDDEN_EVENT, () => {
  const current = router.currentRoute.value
  if (current.path === '/403') return
  void router.replace('/403')
})

app.mount('#app')

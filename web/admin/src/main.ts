import { createApp } from 'vue'
import { createPinia } from 'pinia'
import ElementPlus from 'element-plus'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import 'element-plus/dist/index.css'
import './styles/index.css'

import App from './App.vue'
import { router } from './router'
import { AUTH_EXPIRED_EVENT, FORBIDDEN_EVENT } from './api/client'
import { portalLoginUrl } from './config'
import { useSessionStore } from './stores/session'

const app = createApp(App)
const pinia = createPinia()
app.use(pinia)
app.use(router)
app.use(ElementPlus, { locale: zhCn })

const session = useSessionStore()

// 401：清空内存态并回登录页（PRD §3.1）
window.addEventListener(AUTH_EXPIRED_EVENT, () => {
  session.reset()
  const current = router.currentRoute.value
  if (current.path === '/403') return
  window.location.replace(portalLoginUrl(current.fullPath))
})

// 403：非管理员访问管理接口
window.addEventListener(FORBIDDEN_EVENT, () => {
  const current = router.currentRoute.value
  if (current.path === '/403') return
  void router.replace('/403')
})

app.mount('#app')

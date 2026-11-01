import { createApp } from 'vue'
import { createPinia } from 'pinia'
import { installDesignSystem } from './components/system'
import { initializeTheme } from './lib/theme'
import './styles/design-system.css'
import App from './App.vue'
import router from './router'

// 重新部署后旧页面的恢复兜底：懒加载路由 chunk 的文件名随构建内容变化，
// 旧页面按需加载新路由时 404，点击导航毫无反应（用户视角即"点不进去"）。
// 捕获 chunk 加载失败自动整页刷新一次拿新 bundle；sessionStorage 防刷新循环
// （新 bundle 仍失败说明不是版本切换问题，交给路由错误页/控制台）。
window.addEventListener('vite:preloadError', () => {
  if (!sessionStorage.getItem('chunk-reloaded')) {
    sessionStorage.setItem('chunk-reloaded', '1')
    window.location.reload()
  }
})
router.onError((error) => {
  const msg = (error as Error)?.message ?? ''
  const isChunkLoadFailure =
    /Failed to fetch dynamically imported module|Importing a module script failed|error loading dynamically imported module/i.test(
      msg,
    )
  if (isChunkLoadFailure && !sessionStorage.getItem('chunk-reloaded')) {
    sessionStorage.setItem('chunk-reloaded', '1')
    window.location.reload()
  }
})

initializeTheme()
const app = createApp(App)
app.use(createPinia()).use(router)
installDesignSystem(app)
app.mount('#app')

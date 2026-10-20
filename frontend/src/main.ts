import { createApp } from 'vue'
import { createPinia } from 'pinia'
import ElementPlus from 'element-plus'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import 'element-plus/dist/index.css'
// 暗色主题：官方变量在前，hacker-dark 调色板覆盖在后（顺序不可颠倒）。
import 'element-plus/theme-chalk/dark/css-vars.css'
import './styles/hacker-dark.css'
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

// 全局中文 locale：分页/日期选择等内置文案跟随 UI 语言（否则默认英文）。
createApp(App)
  .use(createPinia())
  .use(router)
  .use(ElementPlus, { locale: zhCn })
  .mount('#app')
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

// 全局中文 locale：分页/日期选择等内置文案跟随 UI 语言（否则默认英文）。
createApp(App)
  .use(createPinia())
  .use(router)
  .use(ElementPlus, { locale: zhCn })
  .mount('#app')
import { createApp } from 'vue'
import { createPinia } from 'pinia'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
// 暗色主题：官方变量在前，hacker-dark 调色板覆盖在后（顺序不可颠倒）。
import 'element-plus/theme-chalk/dark/css-vars.css'
import './styles/hacker-dark.css'
import App from './App.vue'
import router from './router'

createApp(App).use(createPinia()).use(router).use(ElementPlus).mount('#app')
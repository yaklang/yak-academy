import { defineConfig } from 'vitest/config'

// 单元测试配置：jsdom 提供 localStorage/DOM；被测模块均为纯 TS（不挂载组件，
// 无需 vue 插件）。CI 与本地统一走 `npm test`（vitest run）。
export default defineConfig({
  test: {
    environment: 'jsdom',
    include: ['src/**/*.test.ts'],
  },
})

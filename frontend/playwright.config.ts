import { existsSync, mkdirSync, rmSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { defineConfig, devices } from '@playwright/test'

// package.json 为 ESM（type: module），无 __dirname。
const rootDir = fileURLToPath(new URL('.', import.meta.url))

// 浏览器获取策略（解决本地搭建痛点，README「环境搭建与镜像加速」有完整说明）：
//   E2E_CHANNEL=chrome|msedge → 显式用系统浏览器（零下载）
//   E2E_CHANNEL=bundled       → 强制 Playwright 内置 chromium（CI 用，配 playwright install）
//   未设置                    → 自动探测本机 Chrome/Edge，探测不到回退内置 chromium。
// 系统浏览器与本机日常浏览器一致，还顺带覆盖真实 Chrome 行为。
function resolveChannel(): string | undefined {
  if (process.env.E2E_CHANNEL) return process.env.E2E_CHANNEL === 'bundled' ? undefined : process.env.E2E_CHANNEL
  const chromePaths =
    process.platform === 'win32'
      ? ['C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe']
      : process.platform === 'darwin'
        ? ['/Applications/Google Chrome.app/Contents/MacOS/Google Chrome']
        : ['/usr/bin/google-chrome', '/usr/bin/google-chrome-stable']
  const edgePaths =
    process.platform === 'win32' ? ['C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe'] : []
  if (chromePaths.some((p) => existsSync(p))) return 'chrome'
  if (edgePaths.some((p) => existsSync(p))) return 'msedge'
  return undefined
}
const channel = resolveChannel()
const browserUse = channel ? { ...devices['Desktop Chrome'], channel } : devices['Desktop Chrome']

// E2E 独立端口栈（避开 dev 环境 8080/5173）：后端 :18080，前端 :15173。
// E2E_DOCKER=1 时切 docker 驱动并额外跑 e2e/docker/ 下的容器旅程。
const API_PORT = 18080
const FRONT_PORT = 15173
const apiURL = `http://127.0.0.1:${API_PORT}`
const baseURL = `http://127.0.0.1:${FRONT_PORT}`
const useDocker = process.env.E2E_DOCKER === '1'

// 一次性状态目录：每次整跑前清空 DB 与附件（E2E_KEEP_STATE=1 保留用于调试），
// global-setup 全量重播种 → 用例不依赖运行顺序、可重复执行。
// 注意：config 顶层代码会在每个 worker 进程重复执行——清理只允许主进程做
// （worker 有 TEST_WORKER_INDEX），否则删到后端已打开的 DB 会 EBUSY。
const stateDir = path.join(rootDir, 'e2e', '.state')
const isMainProcess = process.env.TEST_WORKER_INDEX === undefined
if (isMainProcess && process.env.E2E_KEEP_STATE !== '1') {
  rmSync(path.join(stateDir, 'e2e.db'), { force: true })
  rmSync(path.join(stateDir, 'attachments'), { recursive: true, force: true })
  rmSync(path.join(stateDir, 'backend.log'), { force: true })
}
if (isMainProcess) mkdirSync(stateDir, { recursive: true })

// 先编译再运行单进程二进制（而不是 go run）：Playwright 杀进程树时
// go run 的子进程在 Windows 上容易残留孤儿容器/进程。
const serverBin = path.join(stateDir, `academy-e2e${process.platform === 'win32' ? '.exe' : ''}`)
// 后端 stdout 落盘：注册 e2e 从中取开发投递的短信验证码；排障时直接看日志。
const backendLog = path.join(stateDir, 'backend.log')

export default defineConfig({
  testDir: './e2e',
  globalSetup: './e2e/global-setup.ts',
  timeout: 30_000,
  // 全套件共享同一个后端 DB 与同一批账号，且可见性/密码用例会改全局状态：
  // 并行 worker 互踩（隐藏题时别人正在做题、改密时别人在登录）。串行执行，
  // 换来确定性；单个 e2e 也就 ~20s，总时长可接受。
  fullyParallel: false,
  workers: 1,
  retries: process.env.CI ? 1 : 0,
  reporter: [['list'], ['html', { open: 'never' }]],
  use: { baseURL, trace: 'retain-on-failure', screenshot: 'only-on-failure' },
  projects: [
    // 默认套件不依赖 Docker（noop 驱动）；容器旅程独立 project。
    { name: 'chromium', use: { ...browserUse }, testIgnore: /\/docker\// },
    ...(useDocker
      ? [{ name: 'docker', use: { ...browserUse }, testMatch: /\/docker\// }]
      : []),
  ],
  webServer: [
    {
      command: `go build -o "${serverBin}" ./cmd/academy-server && "${serverBin}" > "${backendLog}" 2>&1`,
      cwd: path.join(rootDir, '..', 'backend'),
      url: `${apiURL}/health`,
      timeout: 180_000, // 首次编译 + 迁移
      reuseExistingServer: false,
      env: {
        ...process.env,
        ACADEMY_ADDR: `127.0.0.1:${API_PORT}`,
        ACADEMY_DB: path.join(stateDir, 'e2e.db'),
        ACADEMY_ATTACHMENTS_DIR: path.join(stateDir, 'attachments'),
        ACADEMY_JWT_SECRET: 'e2e-test-secret',
        ACADEMY_ADMIN_USERNAME: 'admin',
        ACADEMY_ADMIN_PASSWORD: 'e2e-admin-pass-1',
        ACADEMY_ENV_DRIVER: useDocker ? 'docker' : 'noop',
        ACADEMY_PUBLIC_URL: apiURL,
        // 回显模式：发码响应带验证码原文（sms-register 自助注册回归 spec 依赖它；
        // 日志投递照旧，既有"从 backend.log 取码"的注册用例不受影响）。
        ACADEMY_SMS_SHOW_CODE: '1',
      },
    },
    {
      command: `npx vite --port ${FRONT_PORT} --strictPort`,
      url: baseURL,
      timeout: 60_000,
      reuseExistingServer: false,
      env: { ...process.env, ACADEMY_PROXY_TARGET: apiURL },
    },
  ],
})

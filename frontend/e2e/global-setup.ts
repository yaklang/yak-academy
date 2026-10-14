import { readFile } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { request, type APIResponse } from '@playwright/test'
import { E2E } from './fixtures/consts'

// ESM 下无 __dirname：本文件位于 frontend/e2e/。
const e2eDir = fileURLToPath(new URL('.', import.meta.url))

// 种子全部走管理 API（无内置内容原则）：学生账号、示例题目
// （examples/challenges 现成 JSON）、教程包 zip（testdata 夹具副本）→ 发布。
// DB 每次整跑前已被 playwright.config 顶层清空，种子无需幂等。
export default async function globalSetup(): Promise<void> {
  const ctx = await request.newContext({ baseURL: E2E.apiURL })
  try {
    const login = await must(
      ctx.post('/api/v1/auth/login', {
        data: { username: E2E.admin.username, password: E2E.admin.password },
      }),
      'admin 登录',
    )
    const { token } = (await login.json()) as { token: string }
    const auth = { Authorization: `Bearer ${token}` }

    await must(
      ctx.post('/api/v1/admin/users', {
        headers: auth,
        data: { username: E2E.student.username, password: E2E.student.password },
      }),
      '创建学生账号',
    )

    const challengeJson = await readFile(
      path.join(e2eDir, '..', '..', 'examples', 'challenges', 'crypto-caesar-shift', 'challenge.json'),
      'utf-8',
    )
    await must(
      ctx.post('/api/v1/admin/challenges', { headers: auth, data: JSON.parse(challengeJson) }),
      '上传示例题目',
    )

    const zip = await readFile(path.join(e2eDir, 'fixtures', 'tutorial-package.zip'))
    await must(
      ctx.post('/api/v1/content/packages', {
        headers: auth,
        multipart: {
          package: { name: 'tutorial-package.zip', mimeType: 'application/zip', buffer: zip },
        },
      }),
      '上传教程包',
    )

    const list = await must(ctx.get('/api/v1/content/packages', { headers: auth }), '教程包列表')
    const body = (await list.json()) as { items?: Array<{ id: number; name: string }> }
    const pkg = (body.items ?? []).find((p) => p.name === E2E.tutorial.pkgName)
    if (!pkg) throw new Error(`种子后找不到教程包 ${E2E.tutorial.pkgName}`)
    await must(
      ctx.post(`/api/v1/content/packages/${pkg.id}/publish`, { headers: auth }),
      '发布教程包',
    )
  } finally {
    await ctx.dispose()
  }
}

async function must(resp: Promise<APIResponse>, what: string): Promise<APIResponse> {
  const r = await resp
  if (!r.ok()) {
    throw new Error(`种子失败（${what}）: HTTP ${r.status()} ${await r.text()}`)
  }
  return r
}

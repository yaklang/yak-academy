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

    // 关联题：tutorial_slug 指向教程 Room，覆盖教题双向入口
    // （题目详情「配套教程」/ Room 详情「前往配套题目」）。
    await must(
      ctx.post('/api/v1/admin/challenges', {
        headers: auth,
        data: {
          slug: E2E.challenge.linkedSlug,
          title: E2E.challenge.linkedTitle,
          category: 'web-security',
          difficulty: 'apprentice',
          description_md: '关联教程的配套练习题。',
          score: 60,
          flag_mode: 'shared',
          challenge: { type: 'static', flag: E2E.challenge.linkedFlag },
          tutorial_slug: E2E.tutorial.roomSlug,
        },
      }),
      '上传教程关联题目',
    )

    // 附件题：覆盖学生端凭 JWT 的附件下载链路（上传接口字段名 file）。
    await must(
      ctx.post('/api/v1/admin/challenges', {
        headers: auth,
        data: {
          slug: E2E.challenge.attachSlug,
          title: E2E.challenge.attachTitle,
          category: 'forensics',
          difficulty: 'apprentice',
          description_md: '下载附件并找出隐藏内容。',
          score: 40,
          flag_mode: 'shared',
          challenge: { type: 'static', flag: E2E.challenge.attachFlag },
        },
      }),
      '上传附件题目',
    )
    await must(
      ctx.post(`/api/v1/admin/challenges/${E2E.challenge.attachSlug}/attachments`, {
        headers: auth,
        multipart: {
          file: {
            name: E2E.challenge.attachFile,
            mimeType: 'text/plain',
            buffer: Buffer.from(E2E.challenge.attachContent),
          },
        },
      }),
      '上传附件',
    )

    // 基础题种子（bank=basic，四种题型）：管理端 tab/回填、学员作答、
    // 多选部分分与填空匹配旅程的稳定锚点。
    const B = E2E.basic
    await must(
      ctx.post('/api/v1/admin/challenges', {
        headers: auth,
        data: [
          {
            slug: B.single.slug,
            title: B.single.title,
            category: 'web-basics',
            difficulty: 'apprentice',
            description_md: '客户端未提供有效认证信息时，服务器通常返回哪个状态码？',
            score: B.single.score,
            bank: 'basic',
            question: {
              type: 'single',
              options: [
                { key: 'A', text: '200 OK' },
                { key: 'B', text: '401 Unauthorized' },
                { key: 'C', text: '403 Forbidden' },
              ],
              answer: 'B',
              explanation: '401 表示请求缺少有效认证信息；403 表示已认证但无权访问。',
            },
          },
          {
            slug: B.multiple.slug,
            title: B.multiple.title,
            category: 'web-basics',
            difficulty: 'apprentice',
            description_md: '下列哪些 HTTP 方法属于「安全方法」（不改变服务器状态）？',
            score: B.multiple.score,
            bank: 'basic',
            question: {
              type: 'multiple',
              options: [
                { key: 'A', text: 'GET' },
                { key: 'B', text: 'POST' },
                { key: 'C', text: 'HEAD' },
                { key: 'D', text: 'DELETE' },
              ],
              answer: ['A', 'C'],
              explanation: 'GET 与 HEAD 是只读的安全方法；POST 与 DELETE 会改变服务器状态。',
            },
          },
          {
            slug: B.truefalse.slug,
            title: B.truefalse.title,
            category: 'web-basics',
            difficulty: 'apprentice',
            description_md: '判断：HTTPS 会对 HTTP 请求与响应的正文进行加密传输。',
            score: B.truefalse.score,
            bank: 'basic',
            question: {
              type: 'truefalse',
              answer: true,
              explanation: 'TLS 在传输层加密整个 HTTP 报文。',
            },
          },
          {
            slug: B.fill.slug,
            title: B.fill.title,
            category: 'web-basics',
            difficulty: 'apprentice',
            description_md: '法国的首都是哪座城市？（答案忽略大小写与首尾空白）',
            score: B.fill.score,
            bank: 'basic',
            question: {
              type: 'fill',
              answer: ['Paris'],
              match: { case_sensitive: false, trim: true, regex: false },
              explanation: '巴黎（Paris）是法国首都。',
            },
          },
        ],
      }),
      '上传基础题种子',
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

    // ACM 编程题种子（题目包 zip：judge 段 + testdata/ 对拍数据）。
    const acmZip = await readFile(path.join(e2eDir, 'fixtures', E2E.acm.zip))
    await must(
      ctx.post('/api/v1/admin/challenges/package', {
        headers: auth,
        multipart: {
          package: { name: E2E.acm.zip, mimeType: 'application/zip', buffer: acmZip },
        },
      }),
      '上传 ACM 题目包',
    )

    // 1 秒时限 ACM 题（bugfix-acm-boot-timeout 回归锚点：正确程序执行仅
    // 数毫秒，引擎启动耗时不计入时限才可能 AC）。
    const acmBootZip = await readFile(path.join(e2eDir, 'fixtures', E2E.acmBoot.zip))
    await must(
      ctx.post('/api/v1/admin/challenges/package', {
        headers: auth,
        multipart: {
          package: { name: E2E.acmBoot.zip, mimeType: 'application/zip', buffer: acmBootZip },
        },
      }),
      '上传 1s 时限 ACM 题目包',
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

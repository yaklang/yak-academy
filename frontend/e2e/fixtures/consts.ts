// E2E 常量：账号与种子内容标识。账号/密码由 playwright.config 注入后端，
// 内容由 global-setup 经管理 API 上传（examples 现成 JSON + 教程包 zip 夹具）。
export const E2E = {
  apiURL: process.env.E2E_API_URL || 'http://127.0.0.1:18080',
  admin: { username: 'admin', password: 'e2e-admin-pass-1' },
  student: { username: 'stu-e2e', password: 'stu-e2e-pass-1' },
  challenge: {
    slug: 'crypto-caesar-shift',
    title: '凯撒密码：找回偏移量',
    category: 'cryptography',
    score: 50,
    flag: 'flag{the_quick_brown_fox}',
    hintTitle: '从首词入手',
    hintCost: 5,
    // tutorial_slug 指向教程 Room 的关联题（教题双向入口旅程用）
    linkedSlug: 'sqli-basics-lab',
    linkedTitle: 'SQL 注入：找 flag',
    linkedFlag: 'flag{sqli_e2e}',
    // 附件题（学生端 JWT 附件下载旅程用）
    attachSlug: 'forensics-attachment',
    attachTitle: '取证：隐藏的附件',
    attachFlag: 'flag{hidden_in_attachment}',
    attachFile: 'secret.txt',
    attachContent: 'flag{hidden_in_attachment}\n',
  },
  tutorial: {
    pkgName: 'e2e-test',
    topicSlug: 'web-security',
    topicTitle: 'Web 安全',
    roomSlug: 'sqli-basics',
    roomTitle: 'SQL 注入基础',
  },
  // 竞赛域种子（competition-journey / bugfix-relogin-lock 共用）：
  // 进行中竞赛只能用例内即时创建（起止时间相对“现在”），名称区分各旅程。
  competition: {
    lockName: 'E2E 隔离验证赛',
    endName: 'E2E 到点解锁赛',
    reloginName: 'E2E 重登仍锁赛',
    manualEndName: 'E2E 手动结束赛',
    reviveName: 'E2E 结束重排赛',
    // 公开赛/邀请赛分档旅程（competition-public / competition-invitational-generate）
    publicName: 'E2E 公开广场赛',
    hiddenName: 'E2E 广场隐藏邀请赛',
    generateName: 'E2E 一键生成赛',
  },
  // 删除旅程专用包（独立 slug，避免与 e2e-test 种子冲突）：
  // spec 内经 UI 上传→发布→删除，验证两侧消失。
  deletePkg: {
    name: 'e2e-delete-me',
    zip: 'delete-package.zip',
    topicSlug: 'e2e-del-topic',
    topicTitle: '待删除专题',
    roomSlug: 'e2e-del-room',
    roomTitle: '待删除教程',
  },
  // ACM 编程题种子（题目包 zip：challenge.yml judge 段 + testdata/ + scores.json）：
  // 学生提交 yak 源码判定（ACM 旅程 spec）。判题引擎由 ACADEMY_YAK_BIN
  // 门控——装配时断言 AC/WA，未装配（CI）断言 internal_error 路径。
  // scores.json 按 2/1/1/1 配分（题面 50 分）：partialSource 仅在 partialfail
  // 组（输入 5 5）输出 11≠10 → WA，专用于部分得分断言（4/5 组过 → 40 分）。
  // judge.allowed_libs 配 [str, io, os]（correctSource/partialSource 仅用这三库）：
  // 库策略断言（题面 chips、补全过滤、codec/exec 提交 422 拦截）以此为锚点。
  acm: {
    zip: 'acm-package.zip',
    slug: 'acm-e2e-sum',
    title: 'E2E 求和题（yak）',
    samples: ['demo', 'edge'],
    allowedLibs: ['str', 'io', 'os'],
    correctSource: [
      'res := io.ReadAll(os.Stdin)',
      'lines := str.ParseStringToLines(string(res[0]))',
      'for i := range lines {',
      '    toks := str.SplitAndTrim(lines[i], " ")',
      '    if len(toks) < 2 { continue }',
      '    println(int(toks[0]) + int(toks[1]))',
      '}',
    ].join('\n'),
    wrongSource: 'println(42)',
    partialExpected: 40,
    partialGroups: { demo: 2, edge: 1, hidden: 1, partialfail: 1 },
    // 正确求和但 5+5 故意输出 11：其余组全对，仅 partialfail 组 WA。
    partialSource: [
      'res := io.ReadAll(os.Stdin)',
      'lines := str.ParseStringToLines(string(res[0]))',
      'for i := range lines {',
      '    toks := str.SplitAndTrim(lines[i], " ")',
      '    if len(toks) < 2 { continue }',
      '    a := int(toks[0])',
      '    b := int(toks[1])',
      '    if a == 5 && b == 5 { println(11); continue }',
      '    println(a + b)',
      '}',
    ].join('\n'),
  },
  // 1 秒时限题（bugfix-acm-boot-timeout 专用）：正确程序只需 println(42)，
  // 执行耗时就几毫秒——判题计时必须不含 yak 引擎冷启动/退出滞留
  //（native 热跑全程约 2.3s > 1s），否则正确提交被误判 TLE。
  acmBoot: {
    zip: 'acm-bootlimit-package.zip',
    slug: 'acm-boot-limit',
    title: 'E2E 引擎启动时限题（yak）',
    correctSource: 'println(42)',
  },
} as const

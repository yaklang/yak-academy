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
  // ACM 编程题种子（题目包 zip：challenge.yml judge 段 + testdata/）：
  // 学生提交 yak 源码判定（ACM 旅程 spec）。判题引擎由 ACADEMY_YAK_BIN
  // 门控——装配时断言 AC/WA，未装配（CI）断言 internal_error 路径。
  acm: {
    zip: 'acm-package.zip',
    slug: 'acm-e2e-sum',
    title: 'E2E 求和题（yak）',
    samples: ['demo', 'edge'],
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
  },
} as const

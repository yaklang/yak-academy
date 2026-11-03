// 教程知识链接的内容目录。条目基于官方文档整理，知识浮层内提供官网原文入口；
// lesson.md 用 `[显示文本](#knowledge:<id>)` 引用，前端拦截点击打开浮层。
export type KnowledgeKind = 'security' | 'yakit'

export interface KnowledgeEntry {
  id: string
  title: string
  kind: KnowledgeKind
  summary: string
  points: string[]
  source: { label: string; url: string }
}

export const KNOWLEDGE: Record<string, KnowledgeEntry> = {
  'sql-injection': {
    id: 'sql-injection',
    title: 'SQL 注入',
    kind: 'security',
    summary:
      '应用把不可信输入直接拼进 SQL 语句时，攻击者可以改变查询语义，读取、篡改或删除数据库内容。',
    points: [
      '根因是字符串拼接 SQL；参数化查询 / 预编译是首选防御。',
      '常见入口：URL 参数、表单、JSON 字段、搜索与排序参数。',
      '常见形态：数字型 / 字符型、登录绕过、UNION、报错注入、盲注。',
      '纵深防御：最小权限、错误信息收敛、输入校验；WAF 只能作为补充。',
    ],
    source: {
      label: 'OWASP SQL Injection Prevention Cheat Sheet',
      url: 'https://cheatsheetseries.owasp.org/cheatsheets/SQL_Injection_Prevention_Cheat_Sheet.html',
    },
  },
  'sql-union': {
    id: 'sql-union',
    title: 'UNION 注入',
    kind: 'security',
    summary:
      '利用 UNION 把额外的 SELECT 结果追加到原查询结果集，常用于回显型注入的数据提取。',
    points: [
      'UNION 两侧的列数与类型必须一致；先用 ORDER BY 或 UNION SELECT 1,2,... 探测。',
      '让原查询返回空（如负数 id），页面才会显示 UNION 的结果。',
      '按列位对齐敏感字段，例如 username / password。',
      '防御仍是参数化查询，不要依赖关键字黑名单过滤。',
    ],
    source: {
      label: 'OWASP SQL Injection Prevention Cheat Sheet',
      url: 'https://cheatsheetseries.owasp.org/cheatsheets/SQL_Injection_Prevention_Cheat_Sheet.html',
    },
  },
  xss: {
    id: 'xss',
    title: 'XSS 跨站脚本',
    kind: 'security',
    summary:
      '把攻击者可控的脚本注入页面，在其他用户的浏览器里执行，从而窃取凭据或冒用身份。',
    points: [
      '反射型、存储型、DOM 型三类入口不同，防御都要回到“按输出上下文编码”。',
      'HTML、属性、URL、JavaScript、CSS 各有对应的编码规则，不能一把梭。',
      '富文本使用白名单清洗；CSP 作为纵深防御，不替代输出编码。',
      '不要用“过滤 <script>”作为唯一防线。',
    ],
    source: {
      label: 'OWASP XSS Prevention Cheat Sheet',
      url: 'https://cheatsheetseries.owasp.org/cheatsheets/Cross_Site_Scripting_Prevention_Cheat_Sheet.html',
    },
  },
  ssrf: {
    id: 'ssrf',
    title: 'SSRF 服务端请求伪造',
    kind: 'security',
    summary:
      '攻击者诱导服务器向指定地址发起请求，借此访问内网、回环服务或云元数据接口。',
    points: [
      '危险面：URL 抓取、图片代理、Webhook、文件导入、SSO 回调。',
      '防御要做协议与目标白名单，并在重定向后重新校验目标地址。',
      '网络层限制出站访问，比单纯过滤字符串更可靠。',
      '注意 DNS 重绑定与 IPv6 / 十进制 IP 等绕过形式。',
    ],
    source: {
      label: 'OWASP SSRF Prevention Cheat Sheet',
      url: 'https://cheatsheetseries.owasp.org/cheatsheets/Server_Side_Request_Forgery_Prevention_Cheat_Sheet.html',
    },
  },
  'file-upload': {
    id: 'file-upload',
    title: '文件上传安全',
    kind: 'security',
    summary:
      '上传校验不严可能导致脚本执行、存储型 XSS、路径穿越或恶意文件分发。',
    points: [
      '扩展名、Content-Type、文件魔数要组合校验，不能只看其中一个。',
      '上传后重命名并隔离存储，禁止上传目录直接执行脚本。',
      '限制文件大小与类型，下载时使用安全的 Content-Disposition。',
      '图片等富媒体应重新编码，而不是原样落盘。',
    ],
    source: {
      label: 'OWASP File Upload Cheat Sheet',
      url: 'https://cheatsheetseries.owasp.org/cheatsheets/File_Upload_Cheat_Sheet.html',
    },
  },
  jwt: {
    id: 'jwt',
    title: 'JWT 与 alg=none',
    kind: 'security',
    summary:
      'JWT 头部 alg 决定签名算法；服务端信任客户端提供的算法或密钥 ID 时，可能被伪造令牌。',
    points: [
      '服务端固定算法白名单，拒绝 alg=none 与算法混淆。',
      '校验签名、过期时间、签发者与受众，不要只解析 payload。',
      'kid 等头部字段必须做白名单或安全映射，禁止路径拼接。',
      '密钥要足够强并支持轮换；不要把敏感信息放进 payload。',
    ],
    source: {
      label: 'OWASP JSON Web Token Cheat Sheet',
      url: 'https://cheatsheetseries.owasp.org/cheatsheets/JSON_Web_Token_Cheat_Sheet.html',
    },
  },
  'session-security': {
    id: 'session-security',
    title: '会话与越权',
    kind: 'security',
    summary:
      '会话令牌是身份凭据；令牌泄露、固定或对象级鉴权缺失都会导致冒用与越权。',
    points: [
      'Cookie 使用 HttpOnly、Secure、SameSite；登录后轮换会话标识。',
      '每个请求都在服务端校验身份与权限，不能只靠前端隐藏入口。',
      '对象级授权（IDOR）：确认当前用户确实拥有目标资源的访问权。',
      '会话失效、注销与异常登录都要有明确的服务端处理。',
    ],
    source: {
      label: 'OWASP Session Management Cheat Sheet',
      url: 'https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html',
    },
  },
  'yakit-web-fuzzer': {
    id: 'yakit-web-fuzzer',
    title: 'Yakit Web Fuzzer',
    kind: 'yakit',
    summary:
      'Web Fuzzer 用于手工重放与批量模糊测试 HTTP 请求，是验证注入、越权与 SSRF 的主工作区。',
    points: [
      '可从 MITM / History 把请求发送到 Web Fuzzer，完整保留方法、路径、头与体。',
      '用 Fuzztag 生成字典、随机值与编码后的 payload，批量执行请求。',
      '配合数据匹配 / 提取规则判断响应差异，定位可回显的位置。',
      '先手工验证单条 payload，再扩展到批量测试。',
    ],
    source: {
      label: 'Yakit 使用手册 · Web Fuzzer 概述',
      url: 'https://www.yaklang.com/products/chapter-4/4-1/',
    },
  },
  'yakit-fuzztag': {
    id: 'yakit-fuzztag',
    title: 'Yakit Fuzztag',
    kind: 'yakit',
    summary:
      'Fuzztag 是 Web Fuzzer 的请求生成表达式，用 `{{...}}` 在请求中展开字典、随机值与编码结果。',
    points: [
      '常见形式：字典遍历、随机字符串、数字范围、编码 / 解码。',
      'Fuzztag 可以嵌在 URL、请求头与请求体里。',
      '批量测试前先用单条请求确认注入点与回显位置。',
      '复杂逻辑可配合热加载脚本处理。',
    ],
    source: {
      label: 'Yakit 使用手册 · Fuzztag 构造引擎',
      url: 'https://www.yaklang.com/products/chapter-4/4-4/',
    },
  },
  'yakit-mitm': {
    id: 'yakit-mitm',
    title: 'Yakit MITM',
    kind: 'yakit',
    summary:
      'MITM 通过代理劫持 HTTP/HTTPS 流量，用于观察、修改与重放浏览器与靶场之间的请求。',
    points: [
      '先在 Yakit 安装并信任 CA 证书，再把浏览器代理指向 MITM 监听端口。',
      '在历史记录中查看请求 / 响应，右键可发送到 Web Fuzzer 或 Codec。',
      '热加载脚本可以在请求发出前自动改包，适合批量篡改。',
      '只在自己的授权靶场或测试环境中使用。',
    ],
    source: {
      label: 'Yakit 使用手册 · MITM 技术原理',
      url: 'https://www.yaklang.com/products/chapter-3/3-1/',
    },
  },
  'yakit-codec': {
    id: 'yakit-codec',
    title: 'Yakit Codec',
    kind: 'yakit',
    summary:
      'Codec 是编解码工作台，用于处理 URL、Base64、Hex、HTML 实体等常见转换。',
    points: [
      '支持常见编码与链式处理，适合快速对照原文与编码结果。',
      'URL 编码可处理空格、引号、等号等特殊字符。',
      '编码后的 payload 可以直接复制到 Web Fuzzer。',
      '不要用编码替代输入校验；它只是表达 payload 的工具。',
    ],
    source: {
      label: 'Yakit 使用手册 · Codec 编解码',
      url: 'https://www.yaklang.com/products/chapter-2/2-3/2-3-5/',
    },
  },
  'yakit-vulinbox': {
    id: 'yakit-vulinbox',
    title: 'Vulinbox 靶场',
    kind: 'yakit',
    summary:
      'Vulinbox 是 Yakit 配套的漏洞靶场，用于在授权环境中复现漏洞与验证利用链。',
    points: [
      '按题目路径访问靶场接口，先观察正常响应再构造 payload。',
      '结合 MITM / Web Fuzzer 重放与批量验证。',
      '验证成功后读取靶场 flag，完成题目闭环。',
      '靶场仅用于学习与授权测试。',
    ],
    source: {
      label: 'Yakit 使用手册 · Vulinbox 靶场',
      url: 'https://www.yaklang.com/products/chapter-2/2-3/2-3-9/',
    },
  },
}

export function findKnowledge(id: string): KnowledgeEntry | undefined {
  return KNOWLEDGE[id]
}

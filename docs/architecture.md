# yak-academy 架构设计文档

> 版本：v0.8（Phase 3 完成 + 三赛制竞赛域（CTF/AWD/考试）+ 晋级/证书/实名名册 + PostgreSQL 并发基线 + 微信注册（唯一自助入口）与历史账号找回 + 部署约定）
> 本文档是项目的完整设计源。约束的执行摘要位于 `.agents/skills/`，两者冲突时以本文档为准。

## 1. 背景与对标分析

yak-academy 是一个安全实训平台，提供安全教程（文字+视频）、独立实战题目、进度与积分排行。设计对标调研结论：

| 参考对象 | 借鉴点 | 不借鉴点 |
|---|---|---|
| PortSwigger Web Security Academy | 训练场按分类分组展示题目；题目详情页承载描述/提示/环境/提交；服务端自动判定（远期）；徽章/认证分层激励 | 商业变现部分 |
| 实训常用判题约定 | challenges/flags/hints/submissions 数据模型；恒时比较；动态计分衰减函数 | 比赛制 teams 模式、第三方插件体系（第一版） |
| TryHackMe | 教题关联：教程与题目互指而非互嵌；XP 游戏化 | 靶机常驻 IaaS 成本模型；教题强制同库耦合 |
| CTFtime | 排行榜按赛季切片；题目页聚合题解（远期） | 纯 UGC 聚合模式（无判定） |

**核心架构决策**（均在 skill 中有对应纠偏规则）：

1. **教程与题目两类独立资源**：教程包只含教程（文字+视频），题目用固定格式 challenge.json 单独上传；题目可选 `tutorial_slug` 关联配套教程（双向入口），不关联的纯题目也合法（`.agents/skills/teaching-integration`）。
2. **内容只来源于管理员上传**：内容包 zip + 五步上传管线，禁止内置内容（`.agents/skills/content-package-spec`）。**唯一例外**：官方内置训练集（配套教程 + 题目，基于 yaklang vulinbox 靶场引擎）随后端二进制 go:embed 分发，但仅在管理员于管理端显式「一键导入」（`POST /admin/challenges/builtin`）时写入内容库——题目经同一上传管线 upsert，教程直接 upsert Topic/Room（重复导入即更新）——内容生效仍是管理员动作，vulinbox 二进制为 AGPL-3.0 的上游独立程序，平台不引用其源码（镜像内附 NOTICE，见 `backend/internal/service/builtin/vulinbox/image/`）。
3. **两档角色**：普通人(`student`) / 管理员(`admin`)，声明式中间件鉴权（`.agents/skills/rbac-model`）。
4. **事件溯源**：积分/进度/排行榜从 append-only 事件派生，判题异步（`.agents/skills/event-sourcing-rules`）。
5. **契约先行**：openapi.yaml 是接口唯一契约源（`.agents/skills/api-contract`）。
6. **分层单向依赖**：api → service → repo → model（`.agents/skills/project-conventions`）。

## 2. 总体架构

```
┌────────────────────────────────────────────────────────────┐
│ 表现层  frontend/  Vue3 + TS + Vite                          │
│   学生端：Topic/Room 教程浏览、题库、进度、排行榜、竞赛广场；     │
│           竞赛入口按 format 分发 CTF / AWD / 在线考试视图          │
│   管理端：教程/题目/容器/竞赛/用户；竞赛详情按 format 分发         │
│           CTF 详情 / AWD 控制台 / 考试管理，晋级与证书面板共享      │
│           （用户管理为只读画像表格；建号入口在微信注册与邀请赛     │
│            「一键生成账号」；角色是账号固有属性）                  │
├────────────────────────────────────────────────────────────┤
│ 服务层  backend/internal/api  REST + JWT + RBAC 中间件        │
│   ├─ auth        登录 / JWT / 两档角色 RBAC / 微信注册与找回密码│
│   ├─ content     教程包上传→解析→校验→入库→发布（五步管线）    │
│   ├─ challenge   Challenge/Flag/Hint 业务（题目独立资源）      │
│   ├─ judge       异步判题 worker（恒时比较、动态计分、反作弊）  │
│   ├─ env         Lab 环境生命周期（docker 驱动 / noop 兜底）   │
│   ├─ userprogress 进度跟踪 + 事件派生器（含竞赛积分）          │
│   ├─ competition 共享赛务（赛程/参赛者/名册/晋级/证书）         │
│   ├─ ctf         CTF 域（题目绑定/双榜/WP；由 competition 承载） │
│   ├─ awd         AWD 独立域（game/服务/回合/攻防/战报/最终排名） │
│   ├─ exam        考试独立域（试卷/作答/评分/成绩派生）          │
│   └─ eventbus    进程内 append-only 事件总线 + 派生器           │
├────────────────────────────────────────────────────────────┤
│ 数据层  SQLite（开发/单机）/ PostgreSQL（生产并发基线）+ gorm   │
│   业务库：用户/内容/题目/事件/派生表/竞赛/AWD/考试/证书          │
├────────────────────────────────────────────────────────────┤
│ 基础设施  Docker 单机：题目实例 + AWD 内部网络/靶机/SSH 攻击盒   │
└────────────────────────────────────────────────────────────┘
```

依赖方向（唯一合法）：`api → service → repo → model`。service 横向依赖白名单：`judge→event`、`content→challenge`。跨赛制能力（晋级/证书）由共享赛务定义 `QualificationRankingSource` / `CertificateRankingSource` 接口，CTF/AWD/考试各自实现，赛务域不 import 三域实现。

## 3. 模块设计

### 3.1 auth — 认证与权限

- 登录换取 JWT（HS256），中间件解析并注入请求上下文。
- 两档角色：`student`（普通人，仅学习端）/ `admin`（管理员，管理端全部能力）；普通人前端不显示管理入口。
- 路由注册时声明所需角色，中间件统一执行；历史 `teacher` 启动迁移降为 `student`。
- **账号编码（学号式）**：每号分配唯一编码 `YA{YY}-{NNNNN}`（建号年两位 + 年内序号 1~99999 零填充，如 `YA26-00042`），建号入口统一在 `userRepo.Create`（同年前缀字典序 MAX+1 分配，唯一冲突重试）；序号按建号年分组互不挤占。旧库迁移按 created_at 年分组回填（幂等，起始取该年已有最大序号+1）；部分唯一索引（`account_code <> ''`）兼容旧空值行。编码是用户对外可报读的稳定标识（区别于可读用户名），邀请赛导入表格、用户管理搜索均支持按编码引用/检索。
- **登录画像（管理端用户管理）**：登录成功 best-effort 写 `last_login_at`；不活跃判定 = `COALESCE(last_login_at, created_at)` 早于 now−6 个月（从未登录的账号按注册时间起算，SQL 绑定参数方言中立）。`GET /admin/users` 提供 `q`（用户名/编码/邮箱/手机号子串）/ `role` / `active` / `no_solves`（从未有判定通过的提交）过滤，与表格内标记同口径；管理端用户管理页为画像表格（不活跃 ∩ 从未解题可组合过滤）+ 行内「重置密码」，建号走微信注册或邀请赛一键生成。
- **微信注册/登录（唯一自助入口）**：登录页不提供手机号/邮箱自助注册。流程为 `GET /auth/wechat/status`（能力探测）→ `GET /auth/wechat/start`（返回微信 `authorize_url` 并写入签名 state 会话 Cookie）→ 微信授权页回跳前端 `/login?code&state` → `POST /auth/wechat/callback`（校验 state Cookie 后换微信身份、查找或建号、签发应用 JWT）。两种官方模式：`official_account`（公众号网页授权 `snsapi_userinfo`，可用免费测试号联调）与 `open_platform`（开放平台网站应用扫码 `snsapi_login`）；`ACADEMY_WECHAT_MOCK=1` 仅供本地/E2E 模拟（严格模式禁止）。state 为 HMAC-SHA256 签名会话 + 随机 nonce，HttpOnly + SameSite=Lax，10 分钟有效且一次性（防登录 CSRF/重放）；JWT 只经 JSON 响应体返回，绝不放进 URL。微信账号按 openid（优先）或 unionid（兜底）归并，首次授权自动建号（角色固定 student，用户名由昵称生成、冲突追加稳定后缀），`PasswordHash` 为空即 `password_set=false`——此类账号只能微信登录，管理员重置密码后才可密码登录。历史账号（邮箱/手机号）继续用用户名密码登录；手机号/邮箱字段仅为历史数据保留，不参与登录。
- **运行期统一设置（管理端系统设置）**：可热更新的服务参数存 `system_settings` 表（一行一个配置域，Value 为域内 JSON），当前提供 SMTP 域：`GET/PUT /admin/settings/smtp` 读取/保存，`POST /admin/settings/smtp/test` 发送测试邮件。保存时先构造投递器再落库，成功后原子替换 `DynamicEmailSender`，无需重启；配置非法不写入、不切换，旧配置继续服务。SMTP 密码使用 AES-256-GCM 加密落库（随机 nonce，密钥由 JWT secret 经 HKDF-SHA256 按域分离派生），API 只返回 `has_password`、永不回显；JWT secret 轮换后旧密码不可解密，设置页会提示重新填写。每次保存/测试写 `admin_audit_logs`（不含密码）。
- **验证码回显（仅本地演示）**：默认关闭。本地联调/E2E 可设 `ACADEMY_SHOW_VERIFICATION_CODE=1`（旧变量 `ACADEMY_SMS_SHOW_CODE` 兼容）让找回密码发码响应携带验证码原文；`ACADEMY_STRICT=1` 时该开关必须关闭，否则拒绝启动。
- **找回密码/找回账号（仅历史账号）**：登录页「找回账号」入口，流程为 `POST /auth/password-reset/code`（发码，公开）→ `POST /auth/password-reset`（凭 code 重置密码，公开）。请求体 `email` 与 `phone` 必须且只能填一个；两者都是历史账号凭据，微信/竞赛批量建号没有绑定值，无法自助找回。验证码 6 位、5 分钟有效、核销一次性；按**用途隔离**（`SmsCode.purpose` = password_reset）。重置成功返回用户名与账号编码——忘记用户名的历史账号可凭邮箱/手机号一并找回；密码更新与本人改密/管理员重置同语义：bcrypt 更新 + `TokenInvalidBefore` 推进（该账号全部已签发 token 立即失效），不自动登录（回登录页用新密码登录）。未注册目标发码即拒（422）；微信注册账号与竞赛批量建号走管理端用户管理「重置密码」（`POST /admin/users/{id}/reset-password`，不校验旧密码，同样全量失效 token，预填平台初始密码 123456 可改）——密码一旦设置，`password_set` 变 true，该账号即可用用户名密码登录。

### 3.2 content — 教程包管线

教程包只含教程（文字 + 视频），题目定义不经此管线。五步管线 `Upload → Parse → Validate → Persist → Publish`，整体拒绝（禁止部分入库）：

1. Upload：接收 zip，校验大小与 MIME；
2. Parse：解析 manifest.yaml / topic.md / lesson.md（front matter 可选 `video`、`flow` 字段，video 仅 http/https 绝对地址）；
3. Validate：front matter 必填、难度枚举、slug 冲突；
4. Persist：写 ContentPackage/ContentVersion，状态 draft；
5. Publish：管理员显式发布；新版本 = 新 ContentVersion，旧版本可回滚。

状态机：`draft → published → archived`（详见 `.agents/skills/content-package-spec`）。

**删除**（`DELETE /content/packages/{id}`，管理员显式清理，不可恢复）：删除包记录与全部版本，并按**历代版本 Payload 的 slug 并集**清扫内容表（applyPayload 只 upsert 不删行，v2 移除的教程也须清掉）；同时清空指向被删 Room 的 `challenges.tutorial_slug`（题目本体保留）并删除被删 Room 的 `progresses` 行。事件表 append-only 不动；配套守卫：派生器处理 `room.completed` 时 Room 已不存在则跳过（`SetRoomLookup` 注入查询），启动重放不会复活已删进度。内置训练集不走内容包记录，无删除入口（重新一键导入即恢复）。

### 3.3 challenge — 题目域（独立资源）

- 题目以自有 `slug` 为主键入口，管理员经实训常用题目包 zip（`challenge.yml` + 容器文件）或兼容 challenge.json 上传：**upsert 即生效**（slug 重复 = 更新），不走 draft/publish 管线（版本化仍属教程包机制）。
- `category` 为自由分类 slug（题库分组键）；`description_md` 承载背景与任务目标。
- `bank`（tutorial/ctf/acm）：题库页三 tab（教程题目/CTF 题目/ACM 题目）的分组键。**教程题目 tab 专属官方内置题集**——上传路径仅接受 ctf/acm（显式 tutorial 一律 422），缺省时新建默认 ctf、重传保留原值；内置导入统一注入 tutorial（历史误入 tutorial 的上传题由迁移改判 ctf）。`is_builtin` 为导入来源标记——仅内置导入置 true（`MarkBuiltin`），普通上传/编辑 upsert 不触碰（学生端 `GET /challenges` 按 bank/builtin 过滤）。
- Challenge 多态 `type` 列（v1：static / regex flag）。
- Flag 比较使用恒时比较（逐字符 XOR），防时序侧信道。
- `tutorial_slug` 可选关联教程 Room：题目详情给"配套教程"入口，Room 详情反查给"前往题目"入口；纯题目（不关联）合法。
- Hint 扣分解锁：详情页折叠；`POST .../hints/{id}/unlock` 写 `hint.unlocked` 事件，派生器扣分；未解锁正文不返回。
- **内置训练集（vulinbox，配套教程 + 题目）**：教程与题目定义、基础镜像 Dockerfile 随后端二进制分发（`service/builtin/vulinbox/`，教程在 `tutorials/` 子目录），管理端一键导入（`ImportBuiltinChallenges`）：Docker 可用时先构建共享镜像 `yak-academy/vulinbox:latest`（构建失败整体 422、零写入，与题目包语义一致），再导入配套教程（`importBuiltinTutorials`：复用 `parseFiles` 解析与静态校验，绕过包状态机直接 upsert Topic/Room，幂等），最后复用 `UploadChallenges` 逐题 upsert——**教程先、题目后**，题目落库时 `tutorial_slug` 指向的 Room 已存在，双向入口即刻生效；noop 驱动下跳过构建仅导入定义。**一教程对一题**：`challenges.json` 每题带 `tutorial_slug`，11 篇教程（`yakit-` 前缀 slug 命名空间，避免与上传教程包碰撞）分属 4 个专题，覆盖 Web Fuzzer/MITM/Codec 等核心 Yakit 功能。全题集共用单容器（vulinbox 单进程多靶机、按路径路由，镜像分层：flaggateway 入口层 :8787 → 上游 vulinbox :8788），**全部题目 `flag_mode=instance`**（`flag{lab-{token}}` 模板，每次启动环境生成唯一 flag）：上游 vulinbox 不消费 FLAG 环境变量，镜像内 flaggateway 层负责把平台注入的 FLAG 写入 `/flag.txt` 并暴露 `GET /flag` 读取端点——**端点带按题门禁**（平台随实例注入 `CHALLENGE_SLUG` 标识归属：SSRF 两题与未知 slug 只放行靶机内部自请求，RemoteAddr 回环判定——实例端口映射 `127.0.0.1::port` 后浏览器直连的源地址是 Docker 网桥、伪造 Host 头无效；8 道攻击题在网关反代流量中观察到本题攻击成功信号后放行，回环也不豁免，封死跨题借 SSRF 读 flag；authz 题正路是命令模拟接口），命令模拟接口的 `cat flag.txt` 注入仅注册在 authz 题（其余题 cat 落回上游 command not found，封死跨题捷径）；另把上游 `/ssrf/flag` 演示接口的随机 `flag{uuid}` 改写为固定假 `flag{test}`（与实例真 flag 形态相近、易误提交，改写后一眼可辨）；其余路径原样反代上游，不改靶机行为。统一「完成攻击 → 读 flag → 提交 flag」通关形态。

### 3.4 judge — 异步判题

- 提交接口只入队并返回 pending；worker 消费执行判定，写 `submission.judged` 事件。
- 动态计分：`value = f(solve_count)`，linear / logarithmic 衰减，下限 minimum。
- 服务端自动判定（检测点回调，参考 PortSwigger）：实例启动时生成 `checkpoint_token`，随 `CHECKPOINT_TOKEN`/`CHECKPOINT_URL` 注入容器；题目内 checker 达成目标后 POST `CHECKPOINT_URL`（公开接口，token 即能力凭证）。服务端以哨兵答案入队，worker 走与手动提交相同的管线判过计分（防重复通关、动态计分、room.completed 复用）；提交接口拒绝哨兵值，用户无法伪造环境判定。
- **ACM 编程题（type=code，对齐 ICPC 判定模型）**：题目带 `judge` 段（`acm_judges` + `judge_testdata` 表，包 zip 以 `testdata/<组名>.in`/`<组名>.answer` 成对携带数据组）时不经 flag 判定——学生经同一 `/submissions` 接口提交 yak 源码全文，judge worker 用 `CodeEngine`（`ACADEMY_YAK_BIN` 装配 yak 可执行文件；未装配 → 判 `internal_error`，平台侧失败可重试）**逐组全跑**「运行 → token 化比对输出」（每组结论独立；**时限只约束用户代码执行段**——引擎在源码首尾注入起止标记，首标记出现（解释器完成装载编译、用户代码开跑）才开始计时，末标记出现即杀进程回收：yak 冷启动（首次运行初始化 profile，实测 20s+）与退出滞留（实测 2~7s/组）不占 `time_limit_s`，启动+收尾整体受 60s 兜底上限；stdout/stderr 走自有 `os.Pipe` 而非 exec 拷贝 goroutine，引擎派生的孙辈进程持有管道写端也拖不住判题 worker——主进程死亡即收尾，短排空窗口后强制关读端），判定结果写 `Submission.verdict`（= 全过 ac / 否则首个失败组 verdict：wa/tle/re/internal_error，`submission.judged` 事件 payload 附带 `groups` 逐组明细，`Submission.groups` 列同步落库）且 status 仍为 correct/incorrect——**仅全过为 correct**，计分/首通/竞赛反作弊/room.completed 与 flag 题完全同管线。**部分得分制**：数据组可配分值（challenge.json `judge.testdata[].score` 内联或包 zip `testdata/scores.json` 组名→分值映射，引用不存在的组 422；管理端编辑弹窗可改分值）——`score = round(动态题分 × 通过组权重和 / 总权重)`（`(full*passed + total/2)/total`），全部权重 0（未配置）= 全有全无退化（全过才得分，兼容旧题）；部分通过 = status incorrect 但 score>0（详情页「部分通过」警示 + 逐组结论），**积分/竞赛派生器不累计部分分**（`correct=false` 事件行跳过，题目通关与总分结算仍以全过首通为准，防重复刷部分分）。提交侧护栏：源码 ≤128KiB、同题 pending 期间重复提交 422；样例组（sample=true）经 `GET /challenges/{slug}/judge/samples` 下发（隐藏组不出网）。元数据维护语义：管理端编辑保存 judge 段不带数据组 = 保留库内测试数据（新建编程题必须携带数据组：包 zip 装配或直传 JSON 内联）。
- **自测运行（动态执行框 / custom invocation）**：`POST /challenges/{slug}/judge/runs`（sample 组名或自定义 stdin 二选一）→ 202 票据 id → `GET .../runs/{id}` 轮询至 done。学生端在题目详情页 CodeMirror 编辑器（yak 语法高亮 + 暗色主题，词法对齐 yakit）里直接编码自测（对样例组或自定义输入跑当前源码），与正式判题共用 `CodeEngine` 与题目时限；**运营态而非判定**——不产生 Submission 行、不写事件流、不计分（与事件溯源完全隔离，可无限次自测），票据仅进程内存（重启即清，无审计/重放诉求）。样例模式服务端按样例期望输出 token 化比对（`matches`），自定义模式仅回显 stdout。护栏：源码 ≤128KiB、stdin ≤256KiB、响应 stdout 截断 64KiB、每用户同时仅一个进行中（422）、全局并发闸 2（正式判题不共用，判题优先）、票据 TTL 10 分钟；引擎未装配 → 创建即 422（区别于提交判定的 internal_error：自测可提前知道平台不可用，无需等待判定）。
- **引擎装配与管理**：`CodeEngine` 装配三来源——`ACADEMY_YAK_BIN` 显式路径（最高优先）> 管理端 OSS 下载产物 > 重启探测。管理端「题目管理 → ACM tab」状态卡（`GET /admin/challenges/engine/yak`）展示装配态/路径/版本，`POST .../engine/yak/download` 从 yaklang 官方 OSS（按运行时 GOOS/GOARCH 拼 `yak_<os>_<arch>[.exe]`，约 220MB）异步下载到 `ACADEMY_YAK_DIR`（默认数据库目录，容器内 /data）：`.part` 临时文件 + 原子改名 + chmod 0755，完成即换装（判题 worker 与自测运行经互斥锁保护的访问器无感切换，**无需重启**），进行中再触发 422，进度（received/total 字节）轮询状态接口；任务态仅进程内存（重启回 idle），产物在磁盘、重启自动探测复用。与 vulinbox 镜像构建（同步 1-3 分钟）不同走异步：大文件经 nginx 代理同步转发放有超时风险。
- **库策略（防高危库执行，yak_libpolicy.go）**：双层名单管住「源码能用哪些 yak 标准库」——**全局黑名单**（默认 8 库：`exec`/`cli`/`ssh` 本机命令执行 + `file`/`filesys`/`filemonitor`/`fileparser`/`filescanner` 宿主文件访问；`os` 不入列因 ACM stdin 模板依赖 `os.Stdin`；`ACADEMY_YAK_LIB_BLACKLIST` 逗号分隔**替换**默认表，网络攻击类 poc/fuzz 等是训练价值所在不入黑名单、由题目白名单管控）+ **题目白名单** `judge.allowed_libs`（challenge.yml/challenge.json/管理端编辑弹窗可配；空 = 未配置即允许全部非黑名单库；上传校验拒绝未知库名/黑名单库/重复项 422，合法性宇宙 = yaklang `common/` `yaklang.Import` 注册的 135 库）。**执行边界在服务端受理层**：提交（`/submissions`）与自测（`/judge/runs`）受理前对源码做标识符级静态扫描——剥注释/字符串字面量后按标识符 token 与名单求交集（`exec.Command` 与 `a := exec` 别名同样检出；变量名恰与库同名的遮蔽会误报，保守方向），命中黑名单或白名单外 → 422 点名违规库，**不产生 Submission 行、不进引擎**；自测的扫描先于引擎装配检查（未装配引擎也能拒掉违规源码）。学生端配套引导（非安全边界）：详情页 judge 段随发 `allowed_libs`（已扣除黑名单）与 `blocked_libs`，题面「本题可用库」chips + 高危库禁用提示，编辑器补全菜单按同源策略过滤（白名单外/黑名单库的词首与成员补全不出现）。

### 3.5 env — Lab 环境

- `InstanceDriver`：Create（可注入 `FLAG` 等环境变量）/ Destroy；`ImageBuilder`：题目包 `image: .` 时 `docker build`。
- 单用户同时仅一个活跃容器；TTL 默认 1h，`POST .../instance/extend` 续期不重建。
- `flag_mode=instance`：启动时生成唯一 flag 写入 `LabInstance.flag_value` 并注入容器；判题只接受 running 实例上的值。
- 每实例生成 `checkpoint_token`（`LabInstance.checkpoint_token` 列，API JSON 隐藏）：注入 `CHECKPOINT_TOKEN`/`CHECKPOINT_URL`（`ACADEMY_PUBLIC_URL` 前缀，默认 `host.docker.internal`）；实例销毁 token 即失效。
- Docker 驱动实现；驱动选择 `ACADEMY_ENV_DRIVER`：`auto`（默认，启动探测 docker CLI，可用即 docker，否则 noop）/ `docker` / `noop`。noop（启动/构建返回明确错误 501/422）。
- vulinbox 基础镜像可独立于「一键导入」构建：`GET/POST /admin/challenges/builtin/image`（状态查询 / 仅构建——docker build 内从官方 OSS 下载二进制；管理端题目页镜像状态卡）。
- 内置教程带 `is_builtin` 来源标记（Topic/Room 列，与 Challenge 同模式）：**禁删禁编辑**——上传内容包认领内置 slug 一律 422（含同名包重传豁免路径），删除内容包时版本 Payload 含内置行整体拒绝；更新只走重新一键导入。教程管理页「内置训练集」区块展示目录与导入状态（`GET /admin/challenges/builtin/tutorials`：帐面清单来自 embed 资产，`imported` 逐篇探测内容表），无删除/编辑操作；旧库按 `yakit-` 命名空间回填标记（`migrateTutorialBuiltinFlag`）。

### 3.6 userprogress — 进度

- Progress 为事件派生物：Room 完成率、XP。

### 3.7 eventbus — 事件总线

- 进程内 append-only：`Append(event)` 落库 + 内存广播。
- 派生器（deriver）订阅事件 → 增量更新 ScoreRecord / Progress / CompetitionScore / 排行榜缓存。
- Phase 2 可替换为 Redis Stream / MQ，接口不变。

### 3.8 competition — 共享赛务与 CTF

- **赛制（format）**：创建时三选一——`ctf`（解题赛）/ `awd`（攻防赛）/ `exam`（在线考试），**创建后不可更改**。共享核心只承载 Competition/Participant/Team/Profile/Qualification/Certificate 与入场锁定；三种赛制的领域模型、服务、路由、管理视图完全独立（AWD 不引用 Challenge/CompetitionScore/judge，CTF 不引用 Awd*）。旧竞赛行迁移按 `format=ctf` 解释。
- **CTF 计分**：`scoring_mode=dynamic|fixed`（仅 CTF；开赛后锁定）。dynamic 按通过人数衰减，fixed 直接取 `Challenge.Score`；`CtfAdjustment` 是 append-only 附加分源事实（理由+分值，竞赛级 cap 由配置约束），榜单 = 首通分 + 附加分。双榜包含零分参赛者，支持 standings CSV。
- **报名窗口**：公开赛可配置 `registration_start/end`（成对或同时为空），仅在窗口内允许 `register`；邀请赛不使用。
- **赛制分档（kind）**：创建时二选一——`public`（公开赛）/ `invitational`（邀请赛，旧库缺省回填），**创建后不可更改**。公开赛不发邀请函：展示在竞赛广场（`GET /competitions`，仅 kind=public），学生自助报名（`POST /competitions/{id}/register`，幂等个人报名；仍生成备用邀请码但不投递），已报名者免邀请码入场。邀请赛不对外展示，参赛者由管理端定向导入或一键生成。
- **赛程状态机**：`scheduled → running → ended`（派生自 start_time/end_time，非落库列）；运行中的竞赛只允许延后 end_time 与改描述（收紧起止时间会破坏邀请赛已入场者的锁定语义）。
- **题目绑定**：三种入参统一入口——题目 JSON（对象/数组，复用 content 管线导入后绑定）、`{"slugs": [...]}` 绑定既有题目、zip 题目包；单题失败整体拒绝，已绑定幂等跳过；running 竞赛禁止解绑（榜单已产生，撤题破坏现场）。
- **参赛者与队伍（仅邀请赛）**：批量导入条目按「账号编码或用户名」引用既有账号，**不自动建号**——未命中条目在响应 `missing[]` 排序回报（管理端可先用一键生成补号再重导）；每人生成唯一邀请码并投递站内邀请邮件（投递失败仅标记，不回滚，管理端可重发）；已参赛幂等跳过并沿用原邀请码；队伍归属以首次导入为准。前端导入走固定格式 CSV（个人名单每行一条；队伍名单每行 `队伍名,成员,…`，首行表头自动跳过）。
- **一键生成参赛账号（仅邀请赛）**：`POST /admin/competitions/{id}/generate-users`——按前缀批量建号（用户名 = `前缀-零填充序号`，count ≤500）并全部导入为本场参赛者；可配置自动组队（team_size >0 按生成顺序每 N 人编入「第 n 队」，余数进最后一队）与初始密码（默认 `123456`，≥6 位）。新建账号的初始密码仅本次响应返回（管理端可导出 CSV 分发）；同名既有账号复用（沿用原密码）。
- **入场与锁定**：凭邀请码入场仅 running 竞赛有效；公开赛已报名者可免码入场（未报名先去广场报名，邀请赛无码拒绝）。**锁定只属于邀请赛**：邀请赛入场后不可退出，锁定状态 = `(已入场 且 running 且 kind=invitational)`，比赛结束自动解锁；公开赛报名入场不锁定，可随时返回平台。中间件层面强制：已锁定的普通用户访问平台任何非竞赛接口 → 403，前端路由守卫做同语义 UX 重定向（服务端是兜底权威）；管理员进入任何竞赛都不受锁定约束。
- **竞赛做题**：题目/容器/提交都在 `/competitions/{id}/...` 命名空间下复用训练场能力（容器按竞赛隔离管理），判定走与平台完全相同的 judge 管线（异步、恒时比较、动态计分）。
- **反作弊**：judge 在竞赛提交上执行跨队检测——提交串命中「本竞赛其他队伍成员」的运行中实例 flag（cross_team_flag）等类型写 CheatWarning，只产生管理端警告，不打断提交与判定。
- **双榜**：队伍榜/个人榜均从 CompetitionScore 派生表只读聚合（派生行由事件派生器在竞赛首通时写入，AddScore 幂等）；竞赛服务不触碰派生表写入（event-sourcing-rules）。
- **WP（Writeup）**：仅竞赛 ended 后开放；按题目分条提交/覆盖（文字 markdown + 可选脚本 zip ≤20MB），一人一题一份（队伍内各自提交）；管理端可浏览正文与下载 zip 附件。

### 3.9 awd — AWD 独立域

- **实体**：`AwdGame`（competition 一对一，状态 pending/running/paused/ended、回合秒数、竞域单位 team|user、计分 JSON）、`AwdService`（镜像/slug/端口/checker 配置/基础分与权重）、`AwdTeamService`（竞域单位 × 服务的靶机容器/internal host）、`AwdRound`、`AwdFlag`、`AwdCheck`、`AwdAttack`、`AwdScoreEvent`（append-only 得分账本）、`AwdStanding`（派生排名）、`AwdIncident`、`AwdAttackBox`、`AwdReport/Review`。
- **竞域单位抽象**：个人赛 competitor=user，队伍赛 competitor=team；同一引擎同时支持公告中的个人 AWD 与成熟团队 AWD。团队赛要求全员已编队。
- **Docker 运行时（单机 v1）**：每场竞赛创建一个 `--internal` 网络和一个普通 edge 网络；靶机只接入内部网络，**不发布宿主端口、不挂载 Docker socket/宿主目录**，资源限额、pids-limit、`cap-drop ALL` + 最小能力集（Web 服务 bind/降权）、`no-new-privileges`。SSH 攻击盒先创建在 edge 网络以随机发布 SSH 端口，再接入内部网络访问靶机（Docker 不为仅接 `--internal` 网络的容器建立端口发布规则，见 moby#36174）；密码随机生成、加密落库，接口只回传连接信息不回传密码明文之外的内部字段。攻击盒/靶机都带 label，`DestroyGame` 按 label 清理容器与两个网络。
- **回合引擎**：管理员开启回合时先落 `pending` 回合，再幂等播种确定性 flag（HMAC(seed, teamServiceID:round)）、执行 PUT/GET/CHECK，全部完成后置 `running` 并刷新计时。回合创建/结算/启动恢复按竞赛持有 PostgreSQL advisory lock（SQLite 为进程内键控锁），避免并发重复开回合/结算。进程重启扫描 running 比赛：pending 回合补 flag、只补缺失检查后续跑；已是 running 的回合保留现场不重复检查；同时判定滞留 pending 攻击。
- **Checkers**：内置 HTTP/TCP/command（RuCTF 退出码 101/102/103/104/110）；检查明细落 `AwdCheck`，失败同步写 incident，输出中的 flag 脱敏。
- **攻击判定**：提交只入 `AwdAttack.pending` 并写事件；worker 异步校验 flag 存在、未过期、非自攻、未被首杀，条件更新保证恰好一次 accepted；拒绝原因留运营记录。
- **计分（公式可配置）**：SLA `round(base_score × sla_weight/100 × 成功检查/总检查 × sla_weight_cfg)`；攻击池 `attack_points` 按本轮不同攻击者均分；victim 每服务每回合只扣一次 `defense_loss`；checkdown 每服务每回合扣一次；总分下限 0。所有结果写 `AwdScoreEvent`（`idempotency_key` 唯一），`AwdStanding` 只由 `AwdScoreDeriver` 重放账本重建。
- **战报与最终排名**：赛后 competitor 提交 Markdown/zip；管理员按 rubric 评审打分。`final = (awd_weight × AWD分/榜首×100 + report_weight × 报告分) / (awd_weight+report_weight)`，默认 70/30；并列比较报告分、最后有效攻击时间、competitor id。权重来自 `scoring_config`，赛前公布。
- **导出**：最终排名/攻击/告警 CSV；证书与获奖名单复用共享证书域。
- **边界**：单后端副本、Docker 单机；不承诺 HA，不做浏览器终端/K8s（runtime 接口预留）。

### 3.10 exam — 在线考试独立域

- **实体**：`ExamPaper`（标题/时长/总分/及格线）、`ExamQuestion`（single/multiple/truefalse/code；选项/答案/分值/题序，code 题可绑 Challenge）、`ExamAttempt`（单次尝试、服务端 expires_at、状态）、`ExamAnswer`、`ExamStanding`（派生）。
- **生命周期**：开始作答时按 `min(start+duration, competition.end)` 计算到期时间并落库；到期由 worker 自动交卷，读取作答时懒判定过期（时间统一 UTC 存储/比较，避免 SQLite 时区偏移误判）。单次尝试、记录 IP；不做摄像头/防切屏。
- **评分**：客观题交卷即时评分（多选全对才得分）；编程题复用 yak 判题引擎的 `exam` scope（`Submission.ExamAttemptID` + 事件 payload `exam_attempt_id`），全过 AC 得该题满分，全局派生器跳过 XP/竞赛积分，`ExamDeriver` 在 `exam.attempt.graded` 时重建成绩。试题/试卷在开赛前可编辑，开赛后锁定。
- **结果**：成绩单含未开始/作答中/已交卷/超时状态、客观分/编程分/总分/是否通过，按分数降序、用时升序、交卷时间、用户 ID 排名；支持 CSV。

### 3.11 赛务 — 实名名册 / 晋级 / 证书

- **CompetitionProfile（实名名册）**：competition+user 唯一，字段 real_name/school/student_no/email/phone；选手自助补填，管理员可 CSV 导入；仅管理端可见。名册是证书接收人快照的来源。
- **QualificationRule/Entry**：来源竞赛榜单（CTF 个人/队伍榜、Exam 成绩单、AWD 最终排名）按 `min_score/top_n/top_percent` 预览，应用到目标**未开始的邀请赛**：导入参赛者并生成邀请码；同一来源竞赛对同一目标只应用一次。AWD 团队排名按队伍展开为成员。
- **CertificateRule/Certificate**：竞赛结束后按排名/比例/最低分签发；规则与接收人快照只增不改；同一竞赛内证书名称唯一，接收人去重；公开 16 位十六进制验证码 + 无需登录的验证页 + 可打印 HTML；支持证书/获奖名单 CSV 导出。不对接 NISP/AD 等外部认证 API。

## 4. 数据模型（ER 概览）

```
User ─┬─ Role(global: student|admin)
      ├─ Submission ─→ Challenge
      ├─ Progress(派生) ─→ Room
      ├─ SmsCode(phone|email, code, expires_at, used)   # 注册/找回验证码（一次性核销）
      └─ Participant ─→ Competition ─→ Team       # 参赛/组队

ContentPackage ─→ ContentVersion(draft|published|archived)   # 教程包
Topic ─→ Room(lesson.md + 可选 video)
Challenge(slug 主键, category, difficulty, description_md,     # 独立题目资源
          type: flag|code, tutorial_slug → Room 可选关联)
       ─┬─ Flag(type: static|regex)                              # flag 题（type=flag）
         ├─ ACMJudge(engine, time_limit_s, memory_limit_mb, allowed_libs)  # 编程题（type=code）；allowed_libs JSON 文本列（空 = 未配置白名单）
         │    └─ JudgeTestdata(name, input, answer, sample)      # sample 组学生端可见
         ├─ Hint(title/content/cost)
         └─ Submission(user, answer, status, verdict)            # verdict: ac|wa|tle|re|internal_error（code 题判定结论）
Event(id, user_id, type, payload, created_at)   # append-only
ScoreRecord(派生) / LabInstance(user×challenge, status, expire_at)
SystemSetting(key, value JSON, updated_by)       # 运行期统一设置（SMTP；密码字段加密落库）

Competition(name, kind, format: ctf|awd|exam, scoring_mode,     # 共享赛务；format/kind 创建后不可改
            registration_start/end, start_time, end_time → status 派生)
  ─┬─ CompetitionChallenge(绑定题目+分值快照)                     # CTF
   ├─ CtfAdjustment(append-only 附加分)                           # CTF
   ├─ Team(name) ← Participant(user, invite_code, entered_at, last_ip)
   ├─ CompetitionProfile(competition×user, 实名/学校/学号/邮箱/电话)
   ├─ CheatWarning(user, team, type, reason)                     # CTF 反作弊警告
   ├─ Writeup(user×challenge, content_md + zip 附件)             # CTF ended 后提交
   ├─ CompetitionScore(派生: user/team × challenge 首通分值)      # CTF 双榜数据源
   ├─ QualificationRule/Entry(来源榜单 → 目标邀请赛参赛者)
   └─ CertificateRule/Certificate(规则快照 + 接收人快照 + 唯一验证码)

AwdGame(competition 1:1, status, competitor_type, round_seconds, scoring_config)
  ─┬─ AwdService(镜像/端口/checker/分值权重) ─→ AwdTeamService(competitor×service 靶机)
   ├─ AwdRound(number, starts_at/ends_at, seed) ─→ AwdFlag(round×teamService)
   ├─ AwdCheck(round×teamService, action/status/output)
   ├─ AwdAttack(attacker×victim flag, pending|accepted|rejected)
   ├─ AwdScoreEvent(append-only 账本, idempotency_key 唯一) → AwdStanding(派生)
   ├─ AwdIncident / AwdAttackBox(SSH 凭据加密)
   └─ AwdReport(competitor, Markdown+zip) ─→ AwdReportReview(rubric 分/意见)

ExamPaper(competition 1:1, duration, total, pass_score)
  ─┬─ ExamQuestion(seq, type, options, answer, score, challenge_id)
   └─ ExamAttempt(user×paper, started/expires/submitted, status)
        ─┬─ ExamAnswer(question, answer, objective/code 分)
        └─ ExamStanding(派生: 分数/通过/用时)
```

字段级定义见 `backend/internal/model/`（gorm 实体即数据字典）。

## 5. 关键数据流

主要时序（泳道图见 `docs/data-flow.md`）：

1. **教程上传流**：admin 上传教程 zip → content 管线五步 → draft → admin 发布 → 对学生可见；
2. **题目上传流**：admin 上传 challenge.json → 校验 → upsert 即生效（无 draft/publish）；
3. **做题判定流**：student 在题目详情页提交 → 入队 pending → worker 恒时比较 → `submission.judged` 事件 → 派生器更新积分/进度/排行榜（题目关联教程时同步发 Room 通关事件；竞赛提交另派生 CompetitionScore 并做跨队反作弊检测）；
4. **鉴权流**：请求 → JWT 解析 → 角色中间件（student/admin）→ handler；
5. **注册流**：登录页发码（限频/窗口校验 → SmsCode 落库 → SMTP 投递；管理端设置优先、环境变量兜底，未配置时开发投递器写日志）→ 凭码建号（核销一次性 → 建号角色固定 student → 签发登录 token）；
6. **竞赛流**：admin 建赛（选 kind/format，创建后不可改）→ 按 format 配置（CTF 绑题 / AWD 服务定义与部署 / 考试组卷）→ 配参赛者（邀请赛：CSV/晋级导入或一键生成；公开赛：广场自助报名）→ 参赛者入场（邀请赛锁定至结束）→ CTF 走 judge、AWD 走回合攻防、考试走作答交卷 → 各域派生榜单 → 结束后 WP/战报/证书（泳道图见 `docs/data-flow.md`）；
7. **AWD 攻防流**：开回合（pending→flag/检查→running）→ 选手提交 flag（pending→worker 判定）→ 结算写 AwdScoreEvent → AwdScoreDeriver 重建排名；
8. **考试流**：开考（服务端 expires_at）→ 自动保存 → 交卷/到期 → 客观题即时 + 编程题 exam scope 判分 → ExamDeriver 重建成绩；
9. **赛务流**：实名名册 → 来源榜单预览（CTF/AWD/Exam）→ 应用到目标邀请赛 → 赛后按排名签发证书 → 公开验证；
10. **系统设置流**：admin 保存 SMTP → 校验并构造投递器 → 密码加密写入 `system_settings` → 原子替换 `DynamicEmailSender` 立即生效；校验/落库失败不切换，旧配置继续服务；每次变更记管理审计。

## 6. API 契约总览

完整契约见 `docs/openapi.yaml`。第一版关键接口：

| 方法与路径 | 角色 | 说明 |
|---|---|---|
| `POST /auth/login` | 公开 | 登录换 JWT（历史账号/管理员） |
| `GET /auth/wechat/status` | 公开 | 微信登录能力探测（enabled/mock/mode） |
| `GET /auth/wechat/start` | 公开 | 发起微信网页授权，返回 authorize_url 并写 state 会话 Cookie |
| `POST /auth/wechat/callback` | 公开 | 校验 state 后换微信身份、建号/登录并签发 JWT |
| `POST /auth/password-reset/code` | 公开 | 历史账号找回密码发验证码（限频返回 retry_after） |
| `POST /auth/password-reset` | 公开 | 凭验证码重置密码，返回用户名与账号编码（找回账号） |
| `GET /me` | student | 当前用户与角色 |
| `GET /topics` / `GET /topics/{slug}` | student | 主题列表/详情（含 Room 列表） |
| `GET /topics/{slug}/rooms/{roomSlug}` | student | Room 教程详情（文字+视频，含关联题目 slug） |
| `GET /challenges` | student | 题库题目列表（bank/builtin 过滤 + category 分组展示） |
| `GET /challenges/{slug}` | student | 题目详情（描述/提示/教程关联） |
| `POST /challenges/{slug}/instance` | student | 启动题目容器环境 |
| `DELETE /challenges/{slug}/instance` | student | 销毁环境 |
| `POST /submissions` | student | 提交答案（challenge_slug 键，返回 pending） |
| `GET /submissions/{id}` | student | 查询判定结果 |
| `GET /progress` | student | 个人进度 |
| `GET /leaderboard` | student | 排行榜（派生数据） |
| `GET /badges` | student | 徽章目录与获得状态（计算型读模型） |
| `POST /checkpoints/{token}` | 公开（token 能力鉴权） | 环境检测点自动判定回调（容器内 checker 调用） |
| `POST /content/packages` | admin | 上传教程包（进入管线） |
| `GET /content/packages/{id}` | admin | 包状态与版本 |
| `POST /content/packages/{id}/publish` | admin | 发布 |
| `DELETE /content/packages/{id}` | admin | 删除内容包（历代版本写入的教程一并清扫，不可恢复；事件历史保留） |
| `POST /admin/challenges` | admin | 上传题目 JSON（upsert 即生效） |
| `POST /admin/challenges/builtin` | admin | 一键导入内置训练集（配套教程 + 题目；重复导入 = 更新；教程/题目落库置 `is_builtin` 来源标记——内置教程禁删禁编辑） |
| `GET /admin/challenges/builtin/tutorials` | admin | 内置训练集教程目录与导入状态（教程管理页「内置训练集」区块） |
| `GET /admin/challenges/builtin/image` | admin | vulinbox 基础镜像状态（驱动 / 镜像是否就绪 / 可否构建） |
| `POST /admin/challenges/builtin/image` | admin | 仅构建基础镜像（docker build 内从官方 OSS 下载二进制；noop 422） |
| `GET /admin/challenges` | admin | 题目管理表格 |
| `GET /admin/challenges/{slug}` | admin | 读取单题完整定义（上传 JSON 同构，管理端行内编辑回填用；含完整 flag 列表） |
| `DELETE /admin/challenges/{slug}` | admin | 删除题目（级联 flags/hints） |
| `GET /admin/users` | admin | 用户管理表格：`q`（用户名/账号编码/邮箱/手机号子串）/`role`/`active`/`no_solves` 过滤，条目附 active（6 个月口径）与 solved_any 标记 |
| `POST /admin/users` / `PATCH /admin/users/{id}/roles` | admin | 用户建号/改角色（建号入口在微信注册与邀请赛一键生成；API 保留供账号运维） |
| `POST /admin/users/{id}/reset-password` | admin | 重置用户密码（不校验旧密码，目标用户全部 token 失效；未绑定邮箱/手机号的账号找回入口） |
| `POST /admin/competitions` 等 `/admin/competitions/*` | admin | 竞赛管理：CRUD（创建选 kind/format/scoring_mode/报名窗口，kind/format 不可改）/题目绑定/参赛者导入/名册 CSV/晋级预览与应用/证书预览签发与 CSV/容器总览/反作弊警告/WP/双榜 |
| `DELETE /admin/competitions/{id}` | admin | 删除竞赛（running 422 拒绝；级联清除参赛者/名册/晋级/证书/题目绑定/提交/WP/AWD/考试数据/容器记录，事件历史保留） |
| `/admin/competitions/{id}/awd/*` | admin | AWD 控制台：配置/服务定义与服务包/部署靶机与攻击盒/回合开启结算/排名/检查攻击告警/战报评审/最终排名与 CSV |
| `/admin/competitions/{id}/exam/*` | admin | 考试管理：试卷与试题 CRUD/成绩单与 CSV |
| `/admin/competitions/{id}/roster` | admin | 实名名册 CSV 导入与查询（仅邀请赛；引用既有账号） |
| `GET /competitions` / `POST /competitions/{id}/register` | student | 竞赛广场（仅公开赛，含报名状态）/ 公开赛自助报名（幂等；邀请赛拒绝；报名窗口校验） |
| `GET /competitions/me` | student | 本人参赛与锁定状态（仅邀请赛锁定；路由守卫轮询） |
| `POST /competitions/{id}/enter`、`GET|PUT /competitions/{id}/profile` | student | 入场（邀请码/公开赛免码，running 限定）/ 参赛实名信息补填 |
| `/competitions/{id}/view`、`.../challenges/{slug}/instance`、`.../submissions`、`.../leaderboard`、`.../writeups*` | 参赛者 | CTF 做题闭环：题目列表/容器/提交/双榜/WP 提交与覆盖（ended 后） |
| `/competitions/{id}/awd`、`.../attacks`、`.../scoreboard`、`.../report*`、`.../final` | 参赛者 | AWD：概览/攻击盒/目标目录/提交 flag/排行榜/赛后战报与最终排名 |
| `/competitions/{id}/exam`、`.../attempts*` | 参赛者 | 考试：试卷/开始作答/自动保存/交卷/编程题提交/结果 |
| `GET /certificates/{code}` | public | 无需登录的证书验证页数据（仅接收人/竞赛/奖项快照，不含内部用户 ID） |

## 7. 技术选型

| 项 | 选型 | 理由 |
|---|---|---|
| 后端 | Go 1.22+ / net/http（标准库路由） | 零框架依赖起步，分层清晰 |
| ORM | gorm + SQLite（开发/单机）/ PostgreSQL（生产） | repo 接口抽象；同一迁移与派生路径方言兼容，AWD 锁与并发由 PG advisory lock 承载 |
| 认证 | golang-jwt/jwt/v5（HS256） | 成熟、无状态 |
| 前端 | Vue3 + TS + Vite + Pinia + Vue Router | 管理后台开发效率 |
| API 契约 | OpenAPI 3.0（手写 yaml） | 契约先行，前后端并行 |

## 8. 测试策略（分层金字塔）

| 层 | 位置 | 手段 | 运行 |
|---|---|---|---|
| 纯函数单测 | service 内 | 表驱动 + 时钟注入 | `go test` |
| 服务/仓库单测 | service / repo 同包 | 真实内存 SQLite + 真实 repo，禁止 mock repo | `go test -race ./...` |
| 契约守卫 | api 包 | contract_test 双向比对 openapi.yaml ↔ 注册路由（集合、角色、公开性），漂移即红 | 同上 |
| Docker 门控集成 | service / repo | 真实 daemon（`ACADEMY_TEST_DOCKER=1` 门控，无 Docker 自动 Skip）：Lab 驱动、AWD 靶机隔离/SSH 攻击盒、PostgreSQL 迁移与 advisory lock | CI backend-docker job |
| 并发竞态 | service / judge / AWD | 并发提交/开回合，恰好一次结算；AWD 回合锁串行 | `go test -race` |
| 前端单测 | frontend | vitest 纯逻辑 | `npm test` |
| E2E 旅程 | frontend/e2e | Playwright：双 webServer 自管理（后端 :18080 预编译单二进制 + vite :15173）、整跑全新 DB、管理 API 播种；覆盖认证/教程/题目全链路/三赛制竞赛（CTF/AWD/考试）与赛季晋级证书旅程，`E2E_DOCKER=1` 追加容器旅程 | `npm run e2e` |

E2E 基建约定（README「环境搭建与镜像加速」详述）：浏览器默认用系统 Chrome/Edge
channel（零下载），串行 worker（共享 DB 与账号），改全局状态的用例自恢复。
E2E 已抓出两个生产 bug（release_at 跨时区字典序泄漏未发布题、改密后 1 秒内
重登被拒），是回归防线的重要组成部分。

## 9. 部署约定（容器化 Linux 单机）

- **唯一受支持的部署形态**：Docker Compose 容器化部署于 **Linux 宿主**（amd64，受 vulinbox 基础镜像架构限制）。部署入口为根目录 `docker-compose.yml`（backend + frontend/nginx），配置样例 `deploy/env.example`；生产必改 `ACADEMY_JWT_SECRET` / `ACADEMY_ADMIN_PASSWORD`（启动告警兜底）。数据（SQLite/附件/头像/WP）统一持久化在 `./data` 卷：升级 = 重新 `docker compose up -d --build`，备份 = 备份 `./data`。环境要求 Docker Engine ≥ 20.10（`host-gateway` 支持）+ compose 插件。
- **题目容器治理**：后端容器内置 docker CLI 并挂载宿主 `/var/run/docker.sock`，题目容器创建在宿主 daemon 上（兄弟容器，非嵌套）；compose 显式 `ACADEMY_ENV_DRIVER=docker`。
- **检测点回达**：`DockerDriver.Create` 以 `--add-host host.docker.internal:host-gateway` 注入解析，`ACADEMY_PUBLIC_URL` 默认值（`http://host.docker.internal:8080`）在 Linux 宿主开箱即用（Docker Desktop 兼容，显式条目不冲突）；仅当宿主防火墙拦截容器→宿主流量时改为宿主 LAN IP。题目容器端口默认发布宿主 `127.0.0.1`（本地开发，浏览器在宿主上）；`ACADEMY_INSTANCE_PUBLIC_HOST` 设为学员浏览器可达的宿主地址（如内网 IP）时端口发布全网卡、实例 endpoint 用该地址拼装，远程学员可直连靶机。
- **AWD 运行资产**：攻击盒镜像由 `deploy/build-awd-attackbox.sh` 构建（默认 `yak-awd-attackbox:latest`，auto-update 自动补建）；compose 暴露 `ACADEMY_AWD_ATTACK_IMAGE`/`ACADEMY_AWD_ATTACK_SSH_BIND`/`ACADEMY_AWD_ATTACK_PUBLIC_HOST` 与靶机/攻击盒资源限额。远程比赛必须把 PUBLIC_HOST 设为学员可达的宿主地址，并用宿主防火墙限制 SSH 端口来源；AWD 生产必须 PostgreSQL，且按单后端副本规划（回合锁跨进程，但进程内事件总线不承诺 HA）。
- **非部署形态**：Docker Desktop（Win/Mac）仅本地体验；裸跑后端二进制（start.sh / start.bat）仅本地开发（env 驱动默认 auto：探测到 Docker 即 docker，否则 noop 501）。

## 10. 演进路线

- **Phase 1** ✅ 已完成：架构文档 + skill 体系 + 可编译骨架。
- **Phase 2** ✅ 已完成：最小闭环——真实登录、内容包上传与发布（版本化/回滚/归档）、Room 浏览、提交与异步判题（恒时比较 + 动态计分 + 持久 pending 队列）、事件派生积分/进度/排行榜。
- **Phase 3** ✅ 已完成：题目环境容器化（docker CLI 驱动、单容器约束、TTL reaper、实例 flag、管理端容器总览）、服务端自动判定（检测点回调 + 哨兵答案复用判题管线）、徽章（计算型读模型，进度/积分/路径的实时投影，零新表零新事件）。
- **Phase 3.5** ✅ 已完成：竞赛系统（赛程状态机、题目绑定、参赛者批量导入自动建号、邀请码入场与邀请赛锁定、竞赛内做题复用 judge 管线、跨队反作弊、CompetitionScore 派生双榜、ended 后 WP 提交）与微信注册/登录（公众号网页授权 + 开放平台网站扫码，唯一自助入口；历史邮箱/手机号账号保留找回密码）。
- **Phase 3.6** ✅ 已完成：内置训练集（基于 yaklang vulinbox 靶场引擎的 11 题精选集，管理端一键导入/更新，共享基础镜像 [flaggateway 入口层 + 上游二进制] + instance 判定：每实例唯一 flag，靶机内 /flag.txt 与带按题门禁的 GET /flag、authz 题命令模拟接口 cat flag.txt，跨题捷径均已封死）；扩展为教程 + 题目：4 专题 11 篇 Yakit 教程一教程对一题（`tutorial_slug` 1:1 注入，教程先于题目落库、直接 upsert 无 draft/publish）。
- **Phase 3.7** ✅ 已完成：ACM 编程题源码判定（对齐 ICPC 出题/判题模型）：题目包 zip `judge` 段 + `testdata/` 数据组（`acm_judges`/`judge_testdata` 表），学生提交 yak 源码，`CodeEngine`（`ACADEMY_YAK_BIN`）逐组运行对拍（token 化比对、时限、逐组全跑），`Submission.verdict` 判定结果（ac/wa/tle/re/internal_error）随 `submission.judged` 事件下发，计分/首通/反作弊复用既有管线；样例组专端点下发，管理端编辑支持元数据维护（judge 段不带数据组 = 保留库内数据）。**部分得分制**：数据组配分（`judge.testdata[].score` 内联 / `testdata/scores.json` / 管理端编辑弹窗），部分通过按权重折算得分（未配置 = 全有全无），详情页逐组结论反馈（`Submission.groups` + 事件 `groups`）。
- **Phase 3.8** ✅ 已完成：ACM 自测运行（动态执行框，custom invocation）：题目详情页内置 CodeMirror 6 编辑器（Go 语法近似 yak 高亮）+ 运行面板，对样例组或自定义 stdin 异步运行当前源码（202 票据 + 轮询，与判题共用 `CodeEngine` 与题目时限）；运营态与判定/事件流完全隔离（无 Submission、无事件、不计分），样例模式服务端比对期望输出，票据进程内存 + TTL 10 分钟；引擎未装配创建即 422（与提交判定的 internal_error 路径区分）；判题引擎管理端化——题目管理 ACM tab 状态卡可从 yaklang 官方 OSS 异步下载 yak 二进制（约 220MB，进度轮询、完成即换装无需重启、重启自动探测复用，见 §3.4 引擎装配与管理）。
- **Phase 3.9** ✅ 已完成：编辑器 yak 化：yakit Monarch 词法移植为 CodeMirror 6 StreamLanguage（关键字/类型/内建函数/标准库前缀、f-string 内插表达式、x-string、反引号原串、heredoc、注释、shebang），替换此前的 Go 近似高亮；暗色编辑器主题（yakit kurior 取色，底 #212428、代码字白）+ 外观美化（gutter/选区/激活行/焦点环/禁用态）。**代码补全**：静态补全表（yakit 走 yak 语言服务器 IPC，浏览器端无通道故改静态）——词汇表提取自 yaklang 源码标准库导出表（Import 表 → yaklib 导出 map，ref 0971cec23d，12 库 835 个真实函数名），词首补全关键字/类型/内建/库前缀（含中文说明）+ 片段（for-range / ACM stdin 模板），`lib.` 成员补全给该库导出函数。纯前端改动，无接口/数据变更。
- **Phase 3.10** ✅ 已完成：yak 库白名单/黑名单（防高危库执行，见 §3.4 库策略）：题目级 `judge.allowed_libs` 白名单（`acm_judges.allowed_libs` JSON 文本列，空 = 允许全部非黑名单；上传校验未知/黑名单/重复项 422，宇宙 = yaklang Import 注册 135 库）+ 全局黑名单（默认 exec/cli/ssh/file/filesys/filemonitor/fileparser/filescanner，`ACADEMY_YAK_LIB_BLACKLIST` 环境变量可替换）；提交与自测受理前标识符级静态扫描（剥注释/字符串，别名同检）命中即 422、不进引擎；详情页随发扣除黑名单后的白名单与黑名单列表（题面 chips + 补全菜单同源过滤），管理端编辑弹窗可配白名单。
- **Phase 3.11** ✅ 已完成：判题计时模型修正（时限不含引擎启动/收尾）：`CodeEngine.Run` 接口改为显式传 `limit`；引擎在源码首尾注入 `__ACADEMY_YAK_BOOT__`/`__ACADEMY_YAK_DONE__` 标记行，首标记（用户代码开跑）到末标记（执行完毕）之间才受 `time_limit_s` 约束，末标记即杀进程（省 yak 退出滞留 2~7s/组）；启动+收尾整体受 60s 兜底；stdout/stderr 改自有 `os.Pipe`（`cmd.Wait` 不再被孙辈进程拖住，排空窗口后强制关读端收尾）；自测运行 `time_ms` 汇报执行段毫秒数。回归：`e2e/bugfix-acm-boot-timeout.spec.ts`（1s 时限题正确提交，修复前误 TLE）。同 PR 补引擎未装配守卫：题目详情 judge 段新增 `engine_available`（api 层从 JudgeService 拼装），false 时前端禁用提交/自测入口并警示联系管理员（编辑器仍可编码），代替点击后才收到 internal_error/422 的滞后反馈（服务端拦截仍兜底）。回归：`e2e/bugfix-acm-engine-unconfigured-guard.spec.ts`。
- **Phase 3.12** ✅ 已完成：三赛制竞赛域——Competition 增加不可变 `format=ctf|awd|exam`、CTF fixed 计分/附加分/零分榜单/CSV；AWD 独立域（game/服务包/Docker 内部网络靶机/SSH 攻击盒/checker/回合引擎/攻击异步判定/账本结算/战报 rubric/最终排名/CSV）；在线考试独立域（试卷/单次作答/服务端计时/客观题与 yak exam scope 编程题评分/成绩单）；赛务（实名名册/跨赛制晋级/证书规则签发与公开验证页）；AWD 回合锁与启动恢复、Docker 门控集成测试、部署攻击盒镜像；赛季端到端旅程 e2e。
- **Phase 4** 🔄 进行中：多人规模——SQLite → PostgreSQL（**已落地基础**：`ACADEMY_DB_DSN` 设 `postgres://` 连接串即切 PG，迁移与派生表路径方言兼容、真实 PG 集成测试回归，AWD 回合使用 PG advisory lock；SQLite 仍为默认。待办：存量数据迁移工具）、事件总线外置（Redis Stream/MQ）、排行榜按赛季切片。

## 11. 当前边界（明确不做）

- 事件总线为进程内实现：无持久消费位点，崩溃窗口靠启动 Replay 补偿（Phase 4 外置后移除）。
- 排行榜未按赛季切片；实例端口默认仅回环发布，对外直连依赖 `ACADEMY_INSTANCE_PUBLIC_HOST` 显式配置（无自动 NAT/域名分发）。
- 徽章为计算型读模型：不落库、不产生事件；如未来需要"颁发时间/稀有度"再演进为事件派生表。
- 邮箱通道通过 SMTP/事务邮件服务投递：管理端「系统设置」保存后热加载，环境变量 `ACADEMY_SMTP_*` 仅作为首次启动默认值；未配置/停用时仅非严格模式写开发日志，严格模式发码返回 501——生产必须在管理端配置 SMTP（或保留环境变量默认值）后才能对外开放注册/找回。旧手机号通道仅保留兼容，未接真实短信网关。竞赛 WP 的 zip 附件默认落盘本地目录（`ACADEMY_WRITEUPS_DIR`），可选切 S3 兼容对象存储（`ACADEMY_WRITEUPS_STORE=s3` + `ACADEMY_WRITEUPS_S3_*`，MinIO/AWS S3 等；切换只影响新写入，存量文件不迁移）。
- 备份为应用内单机快照（SQLite `VACUUM INTO` + 数据目录 tar.gz，`ACADEMY_BACKUP_INTERVAL` 周期触发 / 管理端手动触发，按 `ACADEMY_BACKUP_KEEP` 保留份数自动清理）：PostgreSQL 模式不支持（外部 pg_dump）；归档落数据卷本地目录，数据卷整卷损坏时备份同失——异地容灾由运维侧下载归档离机保存或同步 backups/ 目录（平台不内置）。
- 可观测性为「暴露而不内置」：`/metrics`（Prometheus 文本格式，HTTP 按路由聚合 + 运行时指标，`ACADEMY_METRICS_TOKEN` 令牌门控，未配置即关闭）与 slog 结构化访问日志（`ACADEMY_LOG_JSON=1` 切 JSON 行）；采集、存储、看板与告警由外部 Prometheus + Alertmanager / 日志采集器完成，平台不内置监控栈。`/health` 探针与 `/metrics` 采集不进指标与访问日志（高频探针噪音）。
- 管理操作审计为独立 append-only 表（`admin_audit_logs`：管理端全部写操作——内容上传/发布/回滚/删除、题目与附件、竞赛、用户与角色、备份等——成功后 best-effort 落一行，查询走 `GET /admin/audit-logs`）：与 Event 学生行为流分工，不进事件总线、不参与积分重放；审计写失败仅服务日志告警不阻断业务响应，无防篡改（单库同卷，与库共生死）。
- 性能面板（容器管理页，`GET /admin/performance`）为即时快照无历史：数据源 docker info/stats + Linux 宿主 `/proc` 采样（容器内即宿主全局视图）+ 数据卷 statfs；趋势与告警不在平台内置（走外部 Prometheus 栈），非 Linux 开发态宿主占用与数据卷字段为 null。
- AWD 为 Docker 单机单后端副本方案：不承诺 HA/横向扩展；不做浏览器终端/K8s；攻击盒 SSH 端口默认发布全网卡，必须由部署侧防火墙收敛来源。AWD 不内置题目/服务镜像，服务包只由管理员上传构建。
- 在线考试不做摄像头/防切屏/人脸核验，仅服务端计时 + 单次尝试 + IP 记录；编程题依赖 `ACADEMY_YAK_BIN`，未装配时按 internal_error 处理并可重试。
- 证书只生成本平台记录 + 可打印 HTML + CSV，不对接 NISP/AD 等外部认证 API；外部发证由赛务侧办理。
- 不引入 Casbin / Redis / K8s；数据层默认 SQLite，`ACADEMY_DB_DSN` 可选切 PostgreSQL（Phase 4 已落地的第一步；存量 SQLite 数据不自动迁移）。
- JWT secret 与初始管理员密码存在开发默认值（本地开发便利）；生产以环境变量覆盖并建议开启严格模式（`ACADEMY_STRICT=1`：凭据为内置默认或低于强度下限、或开启验证码回显时拒绝启动，非严格路径仅启动告警）。邮箱验证码的 SMTP 配置在管理端「系统设置」统一管理（保存后覆盖环境变量默认值）——未配置/停用时严格模式发码返回 501。SMTP 密码加密密钥由 JWT secret 派生，轮换 JWT secret 后需在设置页重新填写 SMTP 密码。
- 内置训练的 vulinbox 基础镜像构建依赖官方 CDN（`oss-qn.yaklang.com`，离线部署需手工预构建镜像）且仅 amd64；二进制 sha256 锁定在 Dockerfile，上游更新会使导入 422（防靶机行为漂移，更新题集时重新锁定）。

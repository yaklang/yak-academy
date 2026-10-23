package api

import (
	"fmt"
	"strings"
)

// 统一报错接入口（ops 硬化）：所有错误响应经 writeError 唯一出口，把英文
// 原文映射为面向用户的中文提示；英文原文保留在服务端 error 日志（slog
// api_error，见 errors.go）。新增错误输出的流程约束见 api-contract skill：
// 必须在本目录登记中文映射，禁止在 handler 或前端另起翻译层。
//
// 查找顺序（zhMessage）：detail 精确匹配（固定文案）→ dynamicDetails 前后缀
// 剥取（动态中段嵌入中文模板，保留库名等值）→ matchRules 包含匹配（带
// where 前缀或动态参数的文案，如 "rooms[0]: duplicate slug \"x\""）→ 错误码
// 级默认中文。matchRules 按特异性手工排序：更具体的模式必须在更泛化的
// 模式之前（如 "is builtin" 在 "challenge " 之前），新增条目插到所属
// 泛化条目前面。

// codeDefaults 错误码级兜底中文（detail 无映射时使用）。
var codeDefaults = map[string]string{
	"UNAUTHORIZED":      "登录已过期或未登录，请重新登录",
	"FORBIDDEN":         "没有权限执行此操作",
	"NOT_FOUND":         "资源不存在或已被删除",
	"VALIDATION_FAILED": "请求参数有误，请检查后重试",
	"INSTANCE_CONFLICT": "你已有一个运行中的靶机环境，请先关闭它再启动新的",
	"CONFLICT":          "操作与当前状态冲突，请刷新后重试",
	"INTERNAL":          "服务器内部错误，请稍后重试；若持续出现请联系管理员",
	"NOT_IMPLEMENTED":   "该功能在当前部署下未启用（缺少对应运行时配置）",
}

// exactDetails 精确匹配（无动态参数的固定文案）。key 为英文 detail：非
// VALIDATION_FAILED 码即 handler 传入的完整 message（见 errorDetail）。
var exactDetails = map[string]string{
	// 注册 / 账号（学员高频）
	"invalid phone number":                "手机号格式不正确（需 11 位大陆手机号）",
	"code must be 6 digits":               "验证码为 6 位数字",
	"code not sent or expired":            "验证码未发送或已过期，请重新获取验证码",
	"incorrect code":                      "验证码不正确，请核对后重试",
	"phone already registered":            "该手机号已注册过账号：请返回登录；忘记用户名或密码请联系管理员",
	"username already exists":             "用户名已被占用，请换一个用户名",
	"username must be 3-64 characters":    "用户名需 3-64 个字符",
	"password must be at least 6 characters": "密码至少 6 位",
	"empty username":                      "用户名不能为空",
	"global_role must be student or admin": "角色只能是 student 或 admin",

	// 登录 / 会话
	"invalid credentials":            "用户名或密码不正确",
	"invalid username or password":   "用户名或密码不正确",
	"current password incorrect":     "当前密码不正确，请核对后重试",
	"token revoked, please login again": "登录已失效（令牌被吊销），请重新登录",

	// 竞赛入场锁 / 监控（FORBIDDEN 等）
	"competition in progress: only competition endpoints are accessible": "竞赛进行中：仅竞赛相关功能可用，其他功能暂被锁定",
	"metrics endpoint disabled (set ACADEMY_METRICS_TOKEN to enable)":   "监控指标端点未启用（需设置 ACADEMY_METRICS_TOKEN）",
	"invalid metrics token": "监控访问令牌不正确",

	// CONFLICT（必须精确命中，避免落入下方泛化 "challenge " 规则）
	"challenge has solve history; submissions are append-only, delete refused": "该题目已有通关记录，不能删除（提交记录只增不删）",

	// 内容包大小
	"package exceeds 50MB limit": "内容包超出 50MB 大小限制",

	// 靶机实例 / 提示解锁
	"lab has no container environment":            "该靶机没有容器环境，无需启动",
	"only running instances can be extended":      "只有运行中的靶机可以延时",
	"hint unlock not wired":                       "该题目未配置提示解锁",
	"insufficient score to unlock hint":           "积分不足以解锁该提示",
	"instance flag_mode requires a flag template": "题目配置缺失：instance 模式 flag 需要模板",

	// 竞赛域
	"invalid invite code":                     "邀请码无效",
	"invite_code is required":                 "请填写邀请码",
	"competition has not started":             "竞赛尚未开始",
	"competition has ended":                   "竞赛已结束",
	"competition is not running":              "竞赛当前不在进行中",
	"only a running competition can be ended": "只有进行中的竞赛可以手动结束",
	"cannot change start_time while competition is running":     "竞赛进行中不能修改开始时间",
	"cannot unbind challenges while competition is running":     "竞赛进行中不能解绑题目",
	"writeup is accepted only after competition ends":           "竞赛结束后才能提交 Writeup",
	"team name and members are required":                        "队伍名与成员不能为空",
	"empty competition update":                                  "没有需要更新的字段",
	"name is required":                                          "名称不能为空",
	"name cannot be empty":                                      "名称不能为空",
	"end_time must be after start_time":                         "结束时间必须晚于开始时间",

	// 竞赛 kind / 广场报名 / 一键生成 / 表格导入
	"invalid competition kind":                                       "竞赛类型无效（公开赛 public / 邀请赛 invitational 二选一，创建后不可更改）",
	"cannot register for an invitational competition":               "邀请赛不开放自助报名，参赛账号由管理员导入或生成",
	"register before entering a public competition":                 "请先在竞赛广场报名，再进入公开赛",
	"participants can only be imported into an invitational competition": "只有邀请赛支持导入参赛者；公开赛由用户在广场自行报名",
	"user generation is only available for invitational competitions":   "只有邀请赛支持一键生成参赛账号",
	"prefix must be 1-16 lowercase letters, digits or hyphens":      "生成前缀需为 1-16 位小写字母、数字或连字符（如 class26）",
	"count must be between 1 and 500":                               "生成数量需在 1-500 之间",
	"team_size must be between 0 and 10":                            "每队人数需在 0-10 之间（0 = 不自动组队）",
	"empty entry":                                                   "导入条目不能为空（账号编码或用户名）",
	"account creation unavailable":                                  "账号创建入口未装配，请联系管理员",

	// 代码判题 / 自测
	"code engine not configured (self-test unavailable)":  "判题引擎未配置，自测暂不可用（请联系管理员）",
	"exactly one of sample or stdin is required":          "样例组与自定义输入必须二选一",
	"a previous self-test run is still executing":         "上一次自测还在运行，请稍后再试",
	"a previous code submission is still being judged":    "上一次提交还在判题中，请稍后再试",
	"a yak engine download is already in progress":        "引擎下载已在进行中，请等待完成",

	// 内容包 / 题目导入（管理端）
	"challenge.yml is required in package":                      "内容包缺少 challenge.yml",
	"flags required in challenge.yml":                           "challenge.yml 缺少 flags 定义",
	"empty challenge list":                                      "题目列表为空",
	"body must be a challenge object or array":                  "请求体必须是题目对象或数组",
	"not a valid zip archive":                                   "不是有效的 zip 压缩包",
	"invalid attachment filename":                               "附件文件名不合法",
	"empty slugs":                                               "slug 列表不能为空",
	"empty visibility update":                                   "可见性更新不能为空",
	"Dockerfile is required when challenge declares a container image":       "题目声明容器镜像时必须提供 Dockerfile",
	"docker-compose.yml (or compose.yml) is required when challenge declares a container image": "题目声明容器镜像时必须提供 docker-compose.yml",
	"judge problem requires testdata/*.in and testdata/*.answer pairs":       "ACM 题目需要 testdata/*.in 与 *.answer 成对提供",
	"challenge import unavailable":                              "题目导入不可用（缺少内容服务）",

	// 备份 / 运维
	"in-app backup supports SQLite only; dump the PostgreSQL database externally with pg_dump and back up data dirs separately": "应用内备份仅支持 SQLite；PostgreSQL 请用 pg_dump 外部备份，数据目录另行归档",
	"docker driver unavailable; cannot build builtin image": "Docker 驱动不可用，无法构建内置靶机镜像",
	"reserved answer":                                        "该答案为保留答案，不可作为提交",
}

// matchRules 带动态参数/位置前缀的 detail 用包含匹配；按特异性排序。
var matchRules = []struct{ pat, zh string }{
	// 内置题集（比下面的泛化 "challenge " 更具体，必须在前）
	{"is builtin", "内置题目由内置导入统一管理，不能单独修改或删除"},
	{"builtin manifest ", "内置题集清单损坏或缺失，请重新导入"},
	{"builtin tutorials ", "内置教程数据损坏或缺失，请重新导入"},
	{"builtin challenges ", "内置题目数据损坏或缺失，请重新导入"},
	{"is a builtin tutorial and cannot be deleted", "内置教程内容由内置导入统一管理，不能删除"},

	// 题目/内容包校验（where 前缀形态："rooms[0]: ..." 与 "challenge %q ..."）
	{"duplicate judge.allowed_libs entry", "allowed_libs 存在重复项"},
	{"is not a yak lib", "不是可用的 yak 库"},
	{"is blacklisted and can never be enabled", "该库在黑名单内，无法启用"},
	{"judge.testdata must not be empty", "judge.testdata 不能为空"},
	{"requires inline input and output", "测试点必须内联提供 input 与 output"},
	{"not found in stored testdata", "引用的测试数据在存储中不存在"},
	{"duplicate testdata name", "测试点名称重复"},
	{"name must match [a-z0-9-]+", "名称只能包含小写字母、数字和连字符"},
	{"name is required", "名称不能为空"},
	{"score must be >= 0", "测试点分值不能为负"},
	{"judge.engine must be yak", "judge.engine 目前只支持 yak"},
	{"judge.time_limit_s must be 1-60", "时限必须在 1-60 秒之间"},
	{"judge.memory_limit_mb must be 16-512", "内存限制必须在 16-512 MB 之间"},
	{"judge and challenge are mutually exclusive", "judge 与 challenge 段互斥，只能二选一"},
	{"judge section requires bank=acm", "judge 段只有 bank=acm 的题目可用"},
	{"bank=acm requires a judge section", "bank=acm 的题目必须有 judge 段"},
	{"bank must be ctf or acm", "bank 只能是 ctf 或 acm"},
	{"slug must match [a-z0-9-]+", "slug 只能包含小写字母、数字和连字符"},
	{"slug is required", "slug 不能为空"},
	{"duplicate slug", "slug 重复（同一内容包内 slug 必须唯一）"},
	{"title is required", "标题不能为空"},
	{"category is required", "分类不能为空"},
	{"category must match [a-z0-9-]+", "分类只能包含小写字母、数字和连字符"},
	{"score must be non-negative", "分值不能为负"},
	{"hint cost must be non-negative", "提示扣分不能为负"},
	{"flag_mode must be shared or instance", "flag_mode 只能是 shared 或 instance"},
	{"flag_mode=instance requires env.image and env.port", "instance 模式 flag 必须配置 env.image 与 env.port"},
	{"instance flag template must contain {token}", "instance 模式 flag 模板必须包含 {token} 占位符"},
	{"challenge.flag is required", "缺少 challenge.flag"},
	{"challenge.type must be static or regex", "challenge.type 只能是 static 或 regex"},
	{"invalid regex flag", "flag 正则表达式无效"},
	{"env.image is required when env is present", "配置了 env 时必须提供 env.image"},
	{"env.port must be 1-65535", "env.port 必须在 1-65535 之间"},
	{"links[].name is required", "链接名称不能为空"},
	{"url must be http(s)", "链接必须是 http(s) 地址"},

	// 大小限制 / 版本 / 存储（尾部带动态值）
	{"source exceeds", "源码超出大小限制"},
	{"stdin exceeds", "自测输入超出大小限制"},
	{"sample ", "样例组不存在"},
	{"zip exceeds", "Writeup 压缩包超出大小限制"},
	{"extend limit reached", "延时次数已用完，请销毁后重新启动靶机"},
	{"rollback requires published package", "只有已发布的内容包可以回滚"},
	{"rollback target must be an archived version", "回滚目标必须是已归档的版本"},
	{"does not belong to package", "版本不存在或不属于该内容包"},
	{"s3 writeup store requires", "S3 存储配置不完整（缺 ACADEMY_WRITEUPS_S3_ENDPOINT/_BUCKET）"},
	{"unknown ACADEMY_WRITEUPS_STORE", "未知的 WP 存储后端（仅支持 fs 或 s3）"},

	// 泛化兜底（必须放最后）
	{"challenge ", "题目配置校验失败"},
}

// dynamicDetails 带动态中段的 detail（如判题库校验的库名列表）：剥掉英文
// 前后缀，把动态值嵌入中文模板（%s）——保留库名等用户需要知道的值。
var dynamicDetails = []struct{ prefix, suffix, zh string }{
	{"source uses blacklisted yak lib(s): ", " (平台高危库黑名单，任何题目不可用)",
		"源码引用了平台黑名单的高危库（任何题目不可用）：%s"},
	{"source uses yak lib(s) outside the challenge whitelist: ", "",
		"源码引用了题目白名单之外的库：%s"},
}

// zhMessage 错误码 + 英文 detail → 中文提示（唯一入口，供 writeError 使用）。
// detail 为空（码级错误）直接走 codeDefaults。
func zhMessage(code, detail string) string {
	zh, ok := codeDefaults[code]
	if !ok {
		return "操作失败，请稍后重试"
	}
	if detail == "" {
		return zh
	}
	if exact, ok := exactDetails[detail]; ok {
		return exact
	}
	for _, r := range dynamicDetails {
		if strings.HasPrefix(detail, r.prefix) {
			mid := strings.TrimPrefix(detail, r.prefix)
			mid = strings.TrimSuffix(mid, r.suffix)
			return fmt.Sprintf(r.zh, mid)
		}
	}
	for _, r := range matchRules {
		if strings.Contains(detail, r.pat) {
			return r.zh
		}
	}
	return zh
}

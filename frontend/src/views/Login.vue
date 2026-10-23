<script setup lang="ts">
// 登录/注册页：登录走 POST /auth/login；注册走 手机验证码 两步
// （POST /auth/sms/code 发码 → POST /auth/register 建号，角色固定 student），
// 建号成功后端直接签发 token，前端视同登录进入平台。
import { computed, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { useAuthStore } from '../stores/auth'
import { authApi } from '../api'
import { ApiError } from '../api/client'

const auth = useAuthStore()
const route = useRoute()
const router = useRouter()

const mode = ref<'login' | 'register'>('login')
const username = ref('')
const password = ref('')
const phone = ref('')
const code = ref('')
const loading = ref(false)
// 回显模式的验证码（服务端 ACADEMY_SMS_SHOW_CODE=1 时发码响应携带原文）：
// 内联展示在验证码输入框下方，学员无需访问服务端日志即可自助注册。
const sentCode = ref('')

// 发码按钮态：请求中 + 60s 倒计时（后端限频返回 retry_after 时以其为准）。
const sending = ref(false)
const countdown = ref(0)
let timer: ReturnType<typeof setInterval> | undefined

// 登录/注册后跳回原目标页（路由守卫 redirect query）
const redirect = computed(() => (route.query.redirect as string) || null)

function errMsg(e: unknown, fallback: string) {
  return e instanceof ApiError ? e.message : fallback
}
// 错误信封 message 已由后端统一接入口映射为中文（api/errors.go writeError，
// api-contract skill：禁止前端另起翻译层），这里直接透出。

function startCountdown(seconds: number) {
  countdown.value = seconds
  if (timer) clearInterval(timer)
  timer = setInterval(() => {
    countdown.value--
    if (countdown.value <= 0 && timer) {
      clearInterval(timer)
      timer = undefined
    }
  }, 1000)
}
onUnmounted(() => {
  if (timer) clearInterval(timer)
})

async function sendCode() {
  if (!/^1[3-9]\d{9}$/.test(phone.value)) {
    ElMessage.warning('请输入 11 位大陆手机号')
    return
  }
  sending.value = true
  try {
    const resp = await authApi.sendSmsCode(phone.value)
    startCountdown(resp.retry_after ?? 60)
    if (resp.code) {
      // 回显模式（内网/演示部署）：验证码原文随响应返回，页面内联显示。
      sentCode.value = resp.code
    } else {
      // 默认开发投递通道（SmsSender 抽象已留好），验证码写服务端日志。
      ElMessage.success(`验证码已发送${resp.retry_after ? '（发送过于频繁，显示的是剩余等待时间）' : '；当前为开发通道，验证码见服务端日志'}`)
    }
  } catch (e) {
    ElMessage.error(errMsg(e, '发送失败，请稍后再试'))
  } finally {
    sending.value = false
  }
}

async function submit() {
  if (mode.value === 'login') {
    if (!username.value || !password.value) {
      ElMessage.warning('请输入用户名和密码')
      return
    }
    loading.value = true
    try {
      await auth.login(username.value, password.value)
      await auth.fetchMe()
      router.push(redirect.value ?? '/')
    } catch (e) {
      ElMessage.error(errMsg(e, '登录失败'))
    } finally {
      loading.value = false
    }
    return
  }
  // 注册：参数校验与后端一致（后端仍是权威校验，这里只做即时反馈）。
  if (!/^1[3-9]\d{9}$/.test(phone.value)) {
    ElMessage.warning('请输入 11 位大陆手机号')
    return
  }
  if (!/^\d{6}$/.test(code.value)) {
    ElMessage.warning('验证码为 6 位数字')
    return
  }
  if (username.value.length < 3 || username.value.length > 64) {
    ElMessage.warning('用户名需 3-64 个字符')
    return
  }
  if (password.value.length < 6) {
    ElMessage.warning('密码至少 6 位')
    return
  }
  loading.value = true
  try {
    await auth.register(phone.value, code.value, username.value, password.value)
    await auth.fetchMe()
    ElMessage.success(`欢迎加入，${username.value}`)
    router.push(redirect.value ?? '/')
  } catch (e) {
    ElMessage.error(errMsg(e, '注册失败'))
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="login-wrap">
    <div class="login-hero">
      <div class="hero-brand"><span class="logo">&lt;/&gt;</span> yak-academy</div>
      <h1 class="hero-title">安全实训<br><span class="accent">从入门到实战</span></h1>
      <p class="hero-sub">文字 + 视频教程，配套独立靶机题目。<br>通关即积分，进度实时结算。</p>
      <div class="hero-points">
        <span class="pt"><span class="pt-mark">▸</span> 专题式教程路线</span>
        <span class="pt"><span class="pt-mark">▸</span> 一键启动容器靶机</span>
        <span class="pt"><span class="pt-mark">▸</span> 排行榜 / 徽章成长体系</span>
      </div>
    </div>
    <div class="login">
      <div class="terminal-line"><span class="prompt">$</span> ssh user@yak-academy <span class="cursor">▊</span></div>
      <el-form @submit.prevent="submit">
        <h2>yak-academy {{ mode === 'login' ? '登录' : '注册' }}</h2>

        <template v-if="mode === 'login'">
          <el-form-item><el-input v-model="username" placeholder="用户名" /></el-form-item>
          <el-form-item><el-input v-model="password" type="password" placeholder="密码" show-password /></el-form-item>
          <el-button type="primary" native-type="submit" :loading="loading" class="submit-btn">登录</el-button>
          <div class="switch">还没有账号？<el-link type="primary" :underline="false" @click="mode = 'register'">手机验证码注册</el-link></div>
        </template>

        <template v-else>
          <el-form-item><el-input v-model="phone" placeholder="手机号（仅用于注册验证）" maxlength="11" /></el-form-item>
          <el-form-item>
            <div class="code-row">
              <el-input v-model="code" placeholder="验证码" maxlength="6" inputmode="numeric" />
              <el-button :disabled="countdown > 0" :loading="sending" @click="sendCode">
                {{ countdown > 0 ? `${countdown}s` : '获取验证码' }}
              </el-button>
            </div>
            <div v-if="sentCode" class="code-hint">
              验证码：<b>{{ sentCode }}</b>（5 分钟内有效，可直接复制填入）
            </div>
          </el-form-item>
          <el-form-item><el-input v-model="username" placeholder="用户名（3-64 字符，登录用）" /></el-form-item>
          <el-form-item><el-input v-model="password" type="password" placeholder="密码（至少 6 位）" show-password /></el-form-item>
          <el-button type="primary" native-type="submit" :loading="loading" class="submit-btn">注册并进入</el-button>
          <div class="switch">已有账号？<el-link type="primary" :underline="false" @click="mode = 'login'">返回登录</el-link></div>
        </template>
      </el-form>
    </div>
  </div>
</template>

<style scoped>
.login-wrap {
  min-height: 100vh; display: grid;
  grid-template-columns: 1.15fr 1fr;
  align-items: center;
  max-width: 1060px; margin: 0 auto; padding: 0 1.5rem;
  gap: 3rem;
}
.login-hero { display: flex; flex-direction: column; gap: 1.1rem; }
.hero-brand {
  display: flex; align-items: center; gap: 0.5rem;
  font-family: Consolas, monospace; font-weight: 700; font-size: 1rem;
  color: var(--el-text-color-primary);
}
.hero-brand .logo {
  display: inline-flex; align-items: center; justify-content: center;
  width: 28px; height: 28px; border-radius: 7px;
  background: linear-gradient(135deg, rgba(0, 230, 118, 0.22), rgba(0, 230, 118, 0.06));
  border: 1px solid var(--el-color-primary-light-7);
  color: var(--el-color-primary); font-size: 0.75rem;
}
.hero-title {
  font-size: 2.6rem; line-height: 1.25; margin: 0;
  font-weight: 800; color: var(--el-text-color-primary);
}
.accent { color: var(--el-color-primary); }
.hero-sub {
  margin: 0; color: var(--el-text-color-secondary);
  font-size: 0.98rem; line-height: 1.7;
}
.hero-points { display: flex; flex-direction: column; gap: 0.5rem; margin-top: 0.4rem; }
.pt { color: var(--el-text-color-regular); font-size: 0.9rem; }
.pt-mark { color: var(--el-color-primary); margin-right: 0.5rem; font-size: 0.8rem; }
.login { max-width: 380px; width: 100%; justify-self: end; }
.terminal-line {
  font-family: Consolas, monospace; font-size: 0.85rem;
  color: var(--el-text-color-secondary); margin-bottom: 0.8rem;
}
.terminal-line .prompt { color: var(--el-color-primary); font-weight: 700; }
.terminal-line .cursor { color: var(--el-color-primary); animation: blink 1s step-end infinite; }
@keyframes blink { 50% { opacity: 0; } }
.login :deep(.el-form) {
  background: var(--el-bg-color-overlay);
  border: 1px solid var(--el-border-color);
  border-radius: 10px;
  padding: 2rem;
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.35);
}
.login h2 { margin: 0 0 1.2rem; font-family: Consolas, monospace; }
.submit-btn { width: 100%; }
.code-row { display: flex; gap: 0.5rem; width: 100%; }
.code-hint {
  width: 100%; margin-top: 0.4rem; font-size: 0.82rem;
  color: var(--el-color-primary); word-break: break-all;
}
.code-hint b { font-family: Consolas, monospace; letter-spacing: 0.15em; }
.switch {
  margin-top: 1rem; font-size: 0.85rem;
  color: var(--el-text-color-secondary); text-align: center;
}
@media (max-width: 880px) {
  .login-wrap { grid-template-columns: 1fr; padding-top: 4vh; }
  .login { justify-self: stretch; margin: 0 auto; }
}
</style>

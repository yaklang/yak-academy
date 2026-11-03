<script setup lang="ts">
// 登录/注册/找回账号页：登录走 POST /auth/login；注册走 手机验证码 两步
// （POST /auth/sms/code 发码 → POST /auth/register 建号，角色固定 student），
// 建号成功后端直接签发 token，前端视同登录进入平台；
// 找回账号走 POST /auth/password-reset/code 发码 → POST /auth/password-reset
// 凭码重置密码，成功回显用户名与账号编码（忘记用户名的学员凭手机号一并找回）。
import { computed, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ArrowLeft, ArrowRight, Check, Eye, EyeOff, LoaderCircle, Terminal } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import Brand from '@/components/Brand.vue'
import ThemeToggle from '@/components/ThemeToggle.vue'
import TerminalRain from '@/components/TerminalRain.vue'
import CipherReveal from '@/components/CipherReveal.vue'
import ClickSpark from '@/components/ClickSpark.vue'
import { notify } from '@/lib/notifications'
import { useAuthStore } from '../stores/auth'
import { authApi } from '../api'
import { ApiError } from '../api/client'

const auth = useAuthStore()
const route = useRoute()
const router = useRouter()

const showPassword = ref(false)
const mode = ref<'login' | 'register' | 'forgot'>('login')
const pageTitle = computed(() => recovered.value ? '账号已找回。' : mode.value === 'login' ? '欢迎回来。' : mode.value === 'register' ? '开始你的学习。' : '找回你的账号。')
const username = ref('')
const password = ref('')
const phone = ref('')
const code = ref('')
const loading = ref(false)
// 回显模式的验证码（服务端 ACADEMY_SMS_SHOW_CODE=1 时发码响应携带原文）：
// 内联展示在验证码输入框下方，学员无需访问服务端日志即可自助注册/重置。
const sentCode = ref('')
// 找回成功回显的账号标识（重置成功后表单切换为结果面板；不自动登录，回登录页用新密码登录）。
const recovered = ref<{ username: string; account_code: string } | null>(null)

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

// 模式切换：清掉上一模式的验证码状态与找回结果，避免串场。
function switchMode(m: 'login' | 'register' | 'forgot') {
  mode.value = m
  sentCode.value = ''
  code.value = ''
  recovered.value = null
}

async function sendCode() {
  if (!/^1[3-9]\d{9}$/.test(phone.value)) {
    notify.warning('请输入 11 位大陆手机号')
    return
  }
  sending.value = true
  try {
    // 注册与找回是两条发码接口（验证码按用途隔离，互不通用）。
    const resp = mode.value === 'forgot'
      ? await authApi.sendResetCode(phone.value)
      : await authApi.sendSmsCode(phone.value)
    startCountdown(resp.retry_after ?? 60)
    if (resp.code) {
      // 回显模式（内网/演示部署）：验证码原文随响应返回，页面内联显示。
      sentCode.value = resp.code
    } else {
      // 默认开发投递通道（SmsSender 抽象已留好），验证码写服务端日志。
      notify.success(`验证码已发送${resp.retry_after ? '（发送过于频繁，显示的是剩余等待时间）' : '；当前为开发通道，验证码见服务端日志'}`)
    }
  } catch (e) {
    notify.error(errMsg(e, '发送失败，请稍后再试'))
  } finally {
    sending.value = false
  }
}

async function submit() {
  if (mode.value === 'login') {
    if (!username.value || !password.value) {
      notify.warning('请输入用户名和密码')
      return
    }
    loading.value = true
    try {
      await auth.login(username.value, password.value)
      await auth.fetchMe()
      router.push(redirect.value ?? '/learn')
    } catch (e) {
      notify.error(errMsg(e, '登录失败'))
    } finally {
      loading.value = false
    }
    return
  }
  // 找回账号：手机验证码重置密码，成功回显账号（用户名/编码），不自动登录。
  if (mode.value === 'forgot') {
    if (!/^1[3-9]\d{9}$/.test(phone.value)) {
      notify.warning('请输入 11 位大陆手机号')
      return
    }
    if (!/^\d{6}$/.test(code.value)) {
      notify.warning('验证码为 6 位数字')
      return
    }
    if (password.value.length < 6) {
      notify.warning('新密码至少 6 位')
      return
    }
    loading.value = true
    try {
      recovered.value = await authApi.resetPassword(phone.value, code.value, password.value)
      notify.success('密码已重置，原登录状态已全部失效')
    } catch (e) {
      notify.error(errMsg(e, '重置失败，请稍后再试'))
    } finally {
      loading.value = false
    }
    return
  }
  // 注册：参数校验与后端一致（后端仍是权威校验，这里只做即时反馈）。
  if (!/^1[3-9]\d{9}$/.test(phone.value)) {
    notify.warning('请输入 11 位大陆手机号')
    return
  }
  if (!/^\d{6}$/.test(code.value)) {
    notify.warning('验证码为 6 位数字')
    return
  }
  if (username.value.length < 3 || username.value.length > 64) {
    notify.warning('用户名需 3-64 个字符')
    return
  }
  if (password.value.length < 6) {
    notify.warning('密码至少 6 位')
    return
  }
  loading.value = true
  try {
    await auth.register(phone.value, code.value, username.value, password.value)
    await auth.fetchMe()
    notify.success(`欢迎加入，${username.value}`)
    router.push(redirect.value ?? '/learn')
  } catch (e) {
    notify.error(errMsg(e, '注册失败'))
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="public-container auth-page">
    <header class="auth-top">
      <Brand />
      <div class="public-actions"><RouterLink to="/" class="auth-back"><ArrowLeft :size="16" /><span>返回首页</span></RouterLink><ThemeToggle /></div>
    </header>
    <main class="auth-grid">
      <section class="auth-story" aria-labelledby="auth-story-title">
        <TerminalRain class="auth-rain" :density=".32" :speed=".85" />
        <p class="eyebrow"><Terminal :size="14" />LEARNING BY DOING</p>
        <h1 id="auth-story-title"><CipherReveal text="让每一次动手，" /><br><em><CipherReveal text="都成为下一步的底气。" :duration="700" /></em></h1>
        <p>从原理到实践，从学习到挑战。<br>在 Yak Academy，积累属于你的安全能力。</p>
        <div class="auth-signature"><span class="auth-orbit-mark">Y<span>→</span></span><span>EXPLORE. PRACTICE. EVOLVE.</span></div>
      </section>
      <section class="auth-form-area" aria-labelledby="auth-form-title">
        <form class="auth-form" @submit.prevent="submit">
          <h2 id="auth-form-title" :aria-label="`yak-academy ${mode === 'login' ? '登录' : mode === 'register' ? '注册' : '找回账号'}`">{{ pageTitle }}</h2>
          <p class="auth-form-description">{{ recovered ? '请使用新密码，继续你的学习之旅。' : mode === 'login' ? '登录账号，继续你的学习与实战。' : mode === 'register' ? '用手机号注册，开启新的探索。' : '通过绑定手机号验证，重置密码并找回用户名。' }}</p>
          <div v-if="recovered" class="auth-recovered" role="status">
            <Check :size="28" class="text-accent-foreground" />
            <p>账号找回成功，原登录状态已全部失效。</p>
            <dl><dt class="text-muted-foreground">用户名</dt><dd>{{ recovered.username }}</dd><dt class="mt-3 text-muted-foreground">账号编码</dt><dd>{{ recovered.account_code }}</dd></dl>
            <Button class="auth-submit" type="button" @click="switchMode('login')">返回登录<ArrowRight :size="16" /></Button>
          </div>
          <template v-else>
            <div v-if="mode !== 'login'" class="auth-field">
              <Label for="auth-phone">手机号</Label>
              <Input id="auth-phone" v-model="phone" type="tel" inputmode="tel" autocomplete="tel" maxlength="11" :placeholder="mode === 'forgot' ? '手机号（注册时绑定的手机号）' : '手机号（仅用于注册验证）'" />
            </div>
            <div v-if="mode !== 'login'" class="auth-field">
              <Label for="auth-code">短信验证码</Label>
              <div class="auth-code-row"><Input id="auth-code" v-model="code" placeholder="验证码" maxlength="6" inputmode="numeric" autocomplete="one-time-code" /><Button type="button" variant="outline" :disabled="countdown > 0 || sending" @click="sendCode"><LoaderCircle v-if="sending" :size="16" class="spin" />{{ countdown > 0 ? `${countdown}s` : '获取验证码' }}</Button></div>
              <p v-if="sentCode" class="auth-hint" role="status" data-testid="sms-code">验证码：<b>{{ sentCode }}</b>（5 分钟内有效，可直接复制填入）</p>
            </div>
            <div v-if="mode !== 'forgot'" class="auth-field">
              <Label for="auth-username">用户名</Label>
              <Input id="auth-username" v-model="username" autocomplete="username" :placeholder="mode === 'register' ? '用户名（3-64 字符，登录用）' : '请输入用户名'" />
            </div>
            <div class="auth-field">
              <div class="auth-label-row"><Label for="auth-password">{{ mode === 'forgot' ? '新密码' : '密码' }}</Label><button v-if="mode === 'login'" type="button" class="auth-text-button" @click="switchMode('forgot')">找回账号</button></div>
              <div class="auth-password"><Input id="auth-password" v-model="password" :type="showPassword ? 'text' : 'password'" :autocomplete="mode === 'login' ? 'current-password' : 'new-password'" :placeholder="mode === 'login' ? '请输入密码' : mode === 'forgot' ? '新密码（至少 6 位）' : '密码（至少 6 位）'" /><button type="button" :aria-label="showPassword ? '隐藏密码' : '显示密码'" :aria-pressed="showPassword" @click="showPassword = !showPassword"><EyeOff v-if="showPassword" :size="17" /><Eye v-else :size="17" /></button></div>
            </div>
            <Button type="submit" class="auth-submit" :disabled="loading" :aria-busy="loading"><LoaderCircle v-if="loading" :size="16" class="spin" />{{ mode === 'login' ? '登录' : mode === 'register' ? '注册并进入' : '重置密码并找回账号' }}<ArrowRight v-if="!loading" :size="16" /></Button>
            <p v-if="mode === 'login'" class="auth-switch"><span>还没有账号？</span><button type="button" class="auth-text-button" @click="switchMode('register')">手机验证码注册</button></p>
            <p v-else class="auth-switch"><span>{{ mode === 'register' ? '已有账号？' : '已想起账号？' }}</span><button type="button" class="auth-text-button" @click="switchMode('login')">返回登录</button></p>
            <p class="auth-support">{{ mode === 'forgot' ? '竞赛批量建号或管理员创建的账号未绑定手机号，请联系管理员重置密码。' : '首次参加邀请赛？请使用组织方提供的账号登录。' }}</p>
          </template>
        </form>
      </section>
    </main>
    <footer class="auth-footer"><span>学以致用，知行合一。</span><span>© {{ new Date().getFullYear() }} Yak Academy</span></footer>
    <!-- 点击火花仅在公开页生效（fixed 画布随组件挂载） -->
    <ClickSpark />
  </div>
</template>

<style scoped>
/* 品牌故事区的矩阵雨：铺满左侧栏，文字层压上方。
   ::before 用面板色向左渐隐雨幕，避免正文压在字符上影响可读性。 */
.auth-story { position: relative; }
.auth-story::before {
  content: '';
  position: absolute;
  inset: 0;
  z-index: 1;
  pointer-events: none;
  background: linear-gradient(90deg, color-mix(in srgb, var(--panel) 90%, transparent) 0%, color-mix(in srgb, var(--panel) 76%, transparent) 52%, transparent 82%);
}
.auth-rain { position: absolute; inset: 0; z-index: 0; border-radius: inherit; mask-image: radial-gradient(ellipse at 70% 40%, #000 30%, transparent 78%); }
.auth-story > .eyebrow, .auth-story > h1, .auth-story > p:not(.eyebrow), .auth-story > .auth-signature { position: relative; z-index: 2; }
.auth-story h1 .cipher-mask { color: var(--accent-foreground); }
@media (max-width: 767px) {
  .auth-rain { opacity: .6; }
}
</style>

<script setup lang="ts">
// 登录页：微信扫码是唯一自助注册入口（start → 微信/前端回调 → callback 换应用
// token，首次授权自动建号）；历史账号与管理员仍可用用户名密码登录；找回密码
// 仅服务历史邮箱/手机号账号。手机号/邮箱注册入口已下线。
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ArrowLeft, ArrowRight, Check, Eye, EyeOff, LoaderCircle, QrCode, Terminal } from '@lucide/vue'
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
const mode = ref<'login' | 'forgot'>('login')
const pageTitle = computed(() =>
  recovered.value ? '账号已找回。' : mode.value === 'login' ? '欢迎回来。' : '找回你的账号。',
)
const username = ref('')
const password = ref('')
// 找回密码的历史账号标识（含 @ 视为邮箱，否则按旧手机号处理）。
const identifier = ref('')
const identifierIsEmail = computed(() => identifier.value.includes('@'))
const codeLabel = computed(() => (identifierIsEmail.value ? '邮箱验证码' : '短信验证码'))
const code = ref('')
const loading = ref(false)
// 验证码回显（仅本地演示；生产默认关闭）：配置 ACADEMY_SHOW_VERIFICATION_CODE=1
// 时响应携带原文，内联展示在验证码输入框下方；关闭时请查收邮件/短信。
const sentCode = ref('')
// 找回成功回显的账号标识（重置成功后表单切换为结果面板；不自动登录）。
const recovered = ref<{ username: string; account_code: string } | null>(null)

// 微信登录状态：未配置时按钮禁用并提示管理员；本地/E2E mock 也可用。
const wechatAvailable = ref(false)
const wechatChecking = ref(true)
const wechatLoading = ref(false)
const wechatCallbackPending = ref(false)

// 发码按钮态：请求中 + 60s 倒计时（后端限频返回 retry_after 时以其为准）。
const sending = ref(false)
const countdown = ref(0)
let timer: ReturnType<typeof setInterval> | undefined

// 登录后跳回原目标页（路由守卫 redirect query）
const redirect = computed(() => (route.query.redirect as string) || null)

function errMsg(e: unknown, fallback: string) {
  return e instanceof ApiError ? e.message : fallback
}
// 错误信封 message 已由后端统一接入口映射为中文；组件直接透出。

function validEmail(value: string) {
  return value.length <= 254 && /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value)
}

function validIdentifier(value: string) {
  return value.includes('@') ? validEmail(value) : /^1[3-9]\d{9}$/.test(value)
}

function identifierInput() {
  return identifierIsEmail.value ? { email: identifier.value } : { phone: identifier.value }
}

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
function switchMode(m: 'login' | 'forgot') {
  mode.value = m
  sentCode.value = ''
  code.value = ''
  recovered.value = null
}

async function checkWechat() {
  try {
    wechatAvailable.value = (await authApi.wechatStatus()).enabled
  } catch {
    wechatAvailable.value = false
  } finally {
    wechatChecking.value = false
  }
}

// 微信回调落到 /login?code&state：把 code/state 交回后端换 token（首次授权
// 自动建号），成功后清除 URL 参数并跳回原目标页。
async function handleWechatCallback() {
  const callbackCode = route.query.code
  const callbackState = route.query.state
  if (typeof callbackCode !== 'string' || typeof callbackState !== 'string') return
  wechatCallbackPending.value = true
  try {
    const target = await auth.completeWechatLogin(callbackCode, callbackState)
    await auth.fetchMe()
    notify.success('微信登录成功')
    await router.replace(target || redirect.value || '/learn')
  } catch (e) {
    notify.error(errMsg(e, '微信登录失败，请重新发起'))
    await router.replace({ path: '/login', query: redirect.value ? { redirect: redirect.value } : {} })
  } finally {
    wechatCallbackPending.value = false
  }
}

async function startWechat() {
  if (!wechatAvailable.value) {
    notify.warning('微信登录暂未配置，请联系管理员')
    return
  }
  wechatLoading.value = true
  try {
    const resp = await authApi.wechatStart(redirect.value ?? '/learn')
    // 外部授权页必须整页跳转（fetch 无法携带微信登录态）；回调由服务端
    // 302 回前端登录页，登录页再调用 callback 换 token。
    window.location.assign(resp.authorize_url)
  } catch (e) {
    notify.error(errMsg(e, '无法发起微信登录，请稍后再试'))
    wechatLoading.value = false
  }
}

onMounted(() => {
  void checkWechat()
  void handleWechatCallback()
})

async function sendCode() {
  if (!validIdentifier(identifier.value)) {
    notify.warning('请输入有效邮箱或 11 位大陆手机号')
    return
  }
  sending.value = true
  try {
    const resp = await authApi.sendResetCode(identifierInput())
    startCountdown(resp.retry_after ?? 60)
    if (resp.code) {
      // 回显模式（仅本地演示开启）：验证码原文随响应返回，页面内联显示。
      sentCode.value = resp.code
    } else {
      notify.success(
        `验证码已发送，请查收${identifierIsEmail.value ? '邮箱' : '短信'}${
          resp.retry_after ? '（发送过于频繁，显示的是剩余等待时间）' : ''
        }`,
      )
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
  // 找回账号：历史邮箱/手机号验证码重置密码，成功回显账号，不自动登录。
  if (!validIdentifier(identifier.value)) {
    notify.warning('请输入有效邮箱或 11 位大陆手机号')
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
    recovered.value = await authApi.resetPassword({
      ...identifierInput(),
      code: code.value,
      newPassword: password.value,
    })
    notify.success('密码已重置，原登录状态已全部失效')
  } catch (e) {
    notify.error(errMsg(e, '重置失败，请稍后再试'))
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
          <h2 id="auth-form-title" :aria-label="`yak-academy ${mode === 'login' ? '登录' : '找回账号'}`">{{ pageTitle }}</h2>
          <p class="auth-form-description">{{ recovered ? '请使用新密码，继续你的学习之旅。' : mode === 'login' ? '微信扫码注册或登录；管理员与历史账号可使用用户名密码。' : '仅历史邮箱/手机号账号可重置密码；微信注册账号无需密码。' }}</p>
          <div v-if="recovered" class="auth-recovered" role="status">
            <Check :size="28" class="text-accent-foreground" />
            <p>账号找回成功，原登录状态已全部失效。</p>
            <dl><dt class="text-muted-foreground">用户名</dt><dd>{{ recovered.username }}</dd><dt class="mt-3 text-muted-foreground">账号编码</dt><dd>{{ recovered.account_code }}</dd></dl>
            <Button class="auth-submit" type="button" @click="switchMode('login')">返回登录<ArrowRight :size="16" /></Button>
          </div>
          <template v-else>
            <div v-if="mode === 'login'" class="auth-wechat-block">
              <Button
                type="button"
                class="auth-submit auth-wechat"
                :disabled="wechatChecking || wechatLoading || wechatCallbackPending || !wechatAvailable"
                :aria-busy="wechatLoading || wechatCallbackPending"
                @click="startWechat"
              >
                <LoaderCircle v-if="wechatLoading || wechatCallbackPending" :size="18" class="spin" />
                <QrCode v-else :size="18" />
                {{ wechatCallbackPending ? '正在完成微信登录…' : wechatChecking ? '正在检查微信登录…' : wechatLoading ? '正在跳转微信…' : '微信扫码注册 / 登录' }}
              </Button>
              <p v-if="!wechatChecking && !wechatAvailable" class="auth-wechat-hint" role="status">微信登录暂未配置，请联系管理员。</p>
              <p v-else class="auth-wechat-hint">首次使用微信会自动创建账号，无需邮箱/手机号或密码。</p>
              <div class="auth-divider"><span>或使用账号密码</span></div>
            </div>
            <div v-if="mode !== 'login'" class="auth-field">
              <Label for="auth-identifier">邮箱或手机号</Label>
              <Input id="auth-identifier" v-model="identifier" type="text" autocomplete="username" maxlength="254" placeholder="历史账号绑定的邮箱或手机号" />
            </div>
            <div v-if="mode !== 'login'" class="auth-field">
              <Label for="auth-code">{{ codeLabel }}</Label>
              <div class="auth-code-row"><Input id="auth-code" v-model="code" placeholder="验证码" maxlength="6" inputmode="numeric" autocomplete="one-time-code" /><Button type="button" variant="outline" :disabled="countdown > 0 || sending" @click="sendCode"><LoaderCircle v-if="sending" :size="16" class="spin" />{{ countdown > 0 ? `${countdown}s` : '获取验证码' }}</Button></div>
              <p v-if="sentCode" class="auth-hint" role="status" data-testid="verification-code">验证码：<b>{{ sentCode }}</b>（5 分钟内有效，可直接复制填入）</p>
            </div>
            <div v-if="mode === 'login'" class="auth-field">
              <Label for="auth-username">用户名</Label>
              <Input id="auth-username" v-model="username" autocomplete="username" placeholder="请输入用户名" />
            </div>
            <div class="auth-field">
              <div class="auth-label-row"><Label for="auth-password">{{ mode === 'forgot' ? '新密码' : '密码' }}</Label><button v-if="mode === 'login'" type="button" class="auth-text-button" @click="switchMode('forgot')">找回账号</button></div>
              <div class="auth-password"><Input id="auth-password" v-model="password" :type="showPassword ? 'text' : 'password'" :autocomplete="mode === 'login' ? 'current-password' : 'new-password'" :placeholder="mode === 'login' ? '请输入密码' : '新密码（至少 6 位）'" /><button type="button" :aria-label="showPassword ? '隐藏密码' : '显示密码'" :aria-pressed="showPassword" @click="showPassword = !showPassword"><EyeOff v-if="showPassword" :size="17" /><Eye v-else :size="17" /></button></div>
            </div>
            <Button type="submit" class="auth-submit" :disabled="loading" :aria-busy="loading"><LoaderCircle v-if="loading" :size="16" class="spin" />{{ mode === 'login' ? '登录' : '重置密码并找回账号' }}<ArrowRight v-if="!loading" :size="16" /></Button>
            <p v-if="mode === 'login'" class="auth-switch"><span>新用户请使用上方微信注册，自动完成建号与登录。</span></p>
            <p v-else class="auth-switch"><span>已想起账号？</span><button type="button" class="auth-text-button" @click="switchMode('login')">返回登录</button></p>
            <p class="auth-support">{{ mode === 'forgot' ? '微信注册账号无需密码；历史邮箱/手机号账号可在此重置，竞赛批量建号请联系管理员。' : '首次参加邀请赛？请使用组织方提供的账号登录。' }}</p>
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
.auth-wechat-block { display: grid; gap: 10px; }
.auth-wechat { margin-top: 0; }
.auth-wechat-hint { margin: 0; color: var(--muted-foreground); font-size: 12px; line-height: 1.6; }
.auth-divider { display: flex; align-items: center; gap: 12px; margin-top: 12px; color: var(--muted-foreground); font-size: 12px; }
.auth-divider::before, .auth-divider::after { content: ''; height: 1px; flex: 1; background: var(--border); }
@media (max-width: 767px) {
  .auth-rain { opacity: .6; }
}
</style>

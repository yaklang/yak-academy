<script setup lang="ts">
// 登录页：微信扫码是唯一自助注册入口（start → 微信/前端回调 → callback 换应用
// token，首次授权自动建号）；历史账号与管理员仍可用用户名密码登录；找回密码
// 仅服务历史邮箱/手机号账号。页面为静态品牌排版，不加载装饰动画。
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ArrowLeft, ArrowRight, BookOpen, Check, Eye, EyeOff, Flag, LoaderCircle, QrCode, ScanLine } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import FormField from '@/components/layout/FormField.vue'
import PublicShell from '@/components/layout/PublicShell.vue'
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
// 表单级错误：toast 会消失，登录失败必须留下可读的可见入口。
const formError = ref('')
// 验证码回显（仅本地演示；生产默认关闭）。
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

// 模式切换：清掉上一模式的验证码状态与错误，避免串场。
function switchMode(m: 'login' | 'forgot') {
  mode.value = m
  sentCode.value = ''
  code.value = ''
  formError.value = ''
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
  formError.value = ''
  try {
    const target = await auth.completeWechatLogin(callbackCode, callbackState)
    await auth.fetchMe()
    notify.success('微信登录成功')
    await router.replace(target || redirect.value || '/learn')
  } catch (e) {
    formError.value = errMsg(e, '微信登录失败，请重新发起')
    await router.replace({ path: '/login', query: redirect.value ? { redirect: redirect.value } : {} })
  } finally {
    wechatCallbackPending.value = false
  }
}

async function startWechat() {
  formError.value = ''
  if (!wechatAvailable.value) {
    formError.value = '微信登录暂未配置，请联系管理员'
    return
  }
  wechatLoading.value = true
  try {
    const resp = await authApi.wechatStart(redirect.value ?? '/learn')
    // 外部授权页必须整页跳转（fetch 无法携带微信登录态）；回调由服务端
    // 302 回前端登录页，登录页再调用 callback 换 token。
    window.location.assign(resp.authorize_url)
  } catch (e) {
    formError.value = errMsg(e, '无法发起微信登录，请稍后再试')
    wechatLoading.value = false
  }
}

onMounted(() => {
  void checkWechat()
  void handleWechatCallback()
})

async function sendCode() {
  formError.value = ''
  if (!validIdentifier(identifier.value)) {
    formError.value = '请输入有效邮箱或 11 位大陆手机号'
    return
  }
  sending.value = true
  try {
    const resp = await authApi.sendResetCode(identifierInput())
    startCountdown(resp.retry_after ?? 60)
    if (resp.code) {
      sentCode.value = resp.code
    } else {
      notify.success(
        `验证码已发送，请查收${identifierIsEmail.value ? '邮箱' : '短信'}${
          resp.retry_after ? '（发送过于频繁，显示的是剩余等待时间）' : ''
        }`,
      )
    }
  } catch (e) {
    formError.value = errMsg(e, '发送失败，请稍后再试')
  } finally {
    sending.value = false
  }
}

async function submit() {
  formError.value = ''
  if (mode.value === 'login') {
    if (!username.value || !password.value) {
      formError.value = '请输入用户名和密码'
      return
    }
    loading.value = true
    try {
      await auth.login(username.value, password.value)
      await auth.fetchMe()
      router.push(redirect.value ?? '/learn')
    } catch (e) {
      formError.value = errMsg(e, '登录失败')
    } finally {
      loading.value = false
    }
    return
  }
  // 找回账号：历史邮箱/手机号验证码重置密码，成功回显账号，不自动登录。
  if (!validIdentifier(identifier.value)) {
    formError.value = '请输入有效邮箱或 11 位大陆手机号'
    return
  }
  if (!/^\d{6}$/.test(code.value)) {
    formError.value = '验证码为 6 位数字'
    return
  }
  if (password.value.length < 6) {
    formError.value = '新密码至少 6 位'
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
    formError.value = errMsg(e, '重置失败，请稍后再试')
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <PublicShell>
    <template #actions>
      <RouterLink to="/" class="login-back">
        <ArrowLeft aria-hidden="true" />
        <span>返回首页</span>
      </RouterLink>
    </template>

    <div class="login-grid">
      <section class="login-story" aria-labelledby="auth-story-title">
        <p class="login-story__badge">YAK ACADEMY</p>
        <h1 id="auth-story-title" class="login-story__title">
          让每一次动手，<br>
          <span>都成为下一步的底气。</span>
        </h1>
        <p class="login-story__text">
          从原理到实践，从学习到挑战。在 Yak Academy，积累属于你的安全能力。
        </p>
        <ul class="login-story__points">
          <li><BookOpen aria-hidden="true" />教程与视频</li>
          <li><ScanLine aria-hidden="true" />独立实验环境</li>
          <li><Flag aria-hidden="true" />进度与徽章</li>
        </ul>
      </section>

      <section class="login-form-area" aria-labelledby="auth-form-title">
        <form class="login-form" novalidate @submit.prevent="submit">
          <h2 id="auth-form-title" class="login-form__title">{{ pageTitle }}</h2>
          <p class="login-form__description">
            {{ recovered ? '请使用新密码，继续你的学习之旅。' : mode === 'login' ? '微信扫码注册或登录；管理员与历史账号可使用用户名密码。' : '仅历史邮箱/手机号账号可重置密码；微信注册账号无需密码。' }}
          </p>

          <div v-if="formError" class="login-form__error" role="alert">
            {{ formError }}
          </div>

          <div v-if="recovered" class="login-recovered" role="status">
            <Check class="login-recovered__icon" aria-hidden="true" />
            <p>账号找回成功，原登录状态已全部失效。</p>
            <dl class="login-recovered__list">
              <dt>用户名</dt>
              <dd>{{ recovered.username }}</dd>
              <dt>账号编码</dt>
              <dd>{{ recovered.account_code }}</dd>
            </dl>
            <Button class="login-submit" type="button" @click="switchMode('login')">
              返回登录
              <ArrowRight aria-hidden="true" />
            </Button>
          </div>

          <template v-else>
            <div v-if="mode === 'login'" class="login-wechat">
              <Button
                type="button"
                class="login-submit login-wechat__button"
                :disabled="wechatChecking || wechatLoading || wechatCallbackPending || !wechatAvailable"
                :aria-busy="wechatLoading || wechatCallbackPending"
                @click="startWechat"
              >
                <LoaderCircle v-if="wechatLoading || wechatCallbackPending" class="spin" aria-hidden="true" />
                <QrCode v-else aria-hidden="true" />
                {{ wechatCallbackPending ? '正在完成微信登录…' : wechatChecking ? '正在检查微信登录…' : wechatLoading ? '正在跳转微信…' : '微信扫码注册 / 登录' }}
              </Button>
              <p v-if="!wechatChecking && !wechatAvailable" class="login-wechat__hint" role="status">
                微信登录暂未配置，请联系管理员。
              </p>
              <p v-else class="login-wechat__hint">
                首次使用微信会自动创建账号，无需邮箱/手机号或密码。
              </p>
              <div class="login-divider"><span>或使用账号密码</span></div>
            </div>

            <FormField
              v-if="mode !== 'login'"
              label="邮箱或手机号"
              html-for="auth-identifier"
              hint="历史账号绑定的邮箱或手机号"
            >
              <template #default="{ id, describedby, invalid }">
                <Input
                  :id="id"
                  v-model="identifier"
                  type="text"
                  autocomplete="username"
                  maxlength="254"
                  placeholder="历史账号绑定的邮箱或手机号"
                  :aria-describedby="describedby"
                  :aria-invalid="invalid || undefined"
                />
              </template>
            </FormField>

            <FormField
              v-if="mode !== 'login'"
              :label="codeLabel"
              html-for="auth-code"
              hint="验证码为 6 位数字，5 分钟内有效"
            >
              <template #default="{ id, describedby, invalid }">
                <div class="login-code-row">
                  <Input
                    :id="id"
                    v-model="code"
                    placeholder="验证码"
                    maxlength="6"
                    inputmode="numeric"
                    autocomplete="one-time-code"
                    :aria-describedby="describedby"
                    :aria-invalid="invalid || undefined"
                  />
                  <Button type="button" variant="outline" :disabled="countdown > 0 || sending" @click="sendCode">
                    <LoaderCircle v-if="sending" class="spin" aria-hidden="true" />
                    {{ countdown > 0 ? `${countdown}s` : '获取验证码' }}
                  </Button>
                </div>
              </template>
            </FormField>
            <p v-if="sentCode" class="login-hint" role="status" data-testid="verification-code">
              验证码：<b>{{ sentCode }}</b>（5 分钟内有效，可直接复制填入）
            </p>

            <FormField
              v-if="mode === 'login'"
              label="用户名"
              html-for="auth-username"
            >
              <template #default="{ id }">
                <Input
                  :id="id"
                  v-model="username"
                  autocomplete="username"
                  placeholder="请输入用户名"
                />
              </template>
            </FormField>

            <div class="login-field">
              <div class="login-label-row">
                <Label for="auth-password">{{ mode === 'forgot' ? '新密码' : '密码' }}</Label>
                <button
                  v-if="mode === 'login'"
                  type="button"
                  class="login-text-button"
                  @click="switchMode('forgot')"
                >
                  找回账号
                </button>
              </div>
              <div class="login-password">
                <Input
                  id="auth-password"
                  v-model="password"
                  :type="showPassword ? 'text' : 'password'"
                  :autocomplete="mode === 'login' ? 'current-password' : 'new-password'"
                  :placeholder="mode === 'login' ? '请输入密码' : '新密码（至少 6 位）'"
                />
                <button
                  type="button"
                  class="login-password__toggle"
                  :aria-label="showPassword ? '隐藏密码' : '显示密码'"
                  :aria-pressed="showPassword"
                  @click="showPassword = !showPassword"
                >
                  <EyeOff v-if="showPassword" aria-hidden="true" />
                  <Eye v-else aria-hidden="true" />
                </button>
              </div>
            </div>

            <Button type="submit" class="login-submit" :disabled="loading" :aria-busy="loading">
              <LoaderCircle v-if="loading" class="spin" aria-hidden="true" />
              {{ mode === 'login' ? '登录' : '重置密码并找回账号' }}
              <ArrowRight v-if="!loading" aria-hidden="true" />
            </Button>

            <p v-if="mode === 'login'" class="login-switch">
              新用户请使用上方微信注册，自动完成建号与登录。
            </p>
            <p v-else class="login-switch">
              已想起账号？
              <button type="button" class="login-text-button" @click="switchMode('login')">返回登录</button>
            </p>

            <p class="login-support">
              {{ mode === 'forgot' ? '微信注册账号无需密码；历史邮箱/手机号账号可在此重置，竞赛批量建号请联系管理员。' : '首次参加邀请赛？请使用组织方提供的账号登录。' }}
            </p>
          </template>
        </form>
      </section>
    </div>
  </PublicShell>
</template>

<style scoped>
.login-back {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  color: var(--text-secondary);
  font-size: var(--text-caption);
}

.login-back:hover {
  color: var(--text-primary);
}

.login-back :deep(svg) {
  width: 16px;
  height: 16px;
}

.login-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  min-height: 560px;
  border: 1px solid var(--border);
  border-radius: var(--radius-overlay);
  overflow: clip;
}

.login-story {
  display: flex;
  flex-direction: column;
  justify-content: center;
  gap: var(--space-4);
  padding: var(--space-12);
  background: var(--bg-subtle);
}

.login-story__badge {
  color: var(--text-tertiary);
  font-size: var(--text-meta);
  font-weight: 500;
  letter-spacing: 0.06em;
}

.login-story__title {
  color: var(--text-primary);
  font-size: var(--text-display);
  font-weight: 600;
  line-height: 1.35;
  letter-spacing: 0;
  text-wrap: balance;
}

.login-story__title span {
  color: var(--action-primary);
}

.login-story__text {
  max-width: 34ch;
  color: var(--text-secondary);
  font-size: var(--text-body);
  line-height: var(--leading-relaxed);
}

.login-story__points {
  display: grid;
  gap: var(--space-3);
  margin-top: var(--space-4);
  padding: 0;
  list-style: none;
  color: var(--text-secondary);
  font-size: var(--text-caption);
}

.login-story__points li {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

.login-story__points :deep(svg) {
  width: 16px;
  height: 16px;
  color: var(--action-primary);
}

.login-form-area {
  display: flex;
  align-items: center;
  justify-content: center;
  padding: var(--space-10) var(--space-8);
  background: var(--bg-surface);
}

.login-form {
  display: grid;
  gap: var(--space-4);
  width: 100%;
  max-width: 360px;
}

.login-form__title {
  color: var(--text-primary);
  font-size: var(--text-page);
  font-weight: 600;
  line-height: var(--leading-tight);
  letter-spacing: 0;
  text-wrap: balance;
}

.login-form__description {
  color: var(--text-secondary);
  font-size: var(--text-caption);
  line-height: var(--leading-relaxed);
}

.login-form__error {
  padding: var(--space-3);
  border: 1px solid color-mix(in srgb, var(--status-danger) 32%, transparent);
  border-radius: var(--radius-control);
  background: color-mix(in srgb, var(--status-danger) 8%, var(--bg-surface));
  color: var(--status-danger);
  font-size: var(--text-caption);
  line-height: var(--leading-normal);
}

.login-field {
  display: grid;
  gap: var(--space-2);
  min-width: 0;
}

.login-label-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
}

.login-text-button {
  color: var(--action-subtle-fg);
  font-size: var(--text-caption);
  text-underline-offset: 4px;
}

.login-text-button:hover {
  text-decoration: underline;
}

.login-password {
  position: relative;
}

.login-password :deep(input) {
  padding-right: var(--space-12);
}

.login-password__toggle {
  position: absolute;
  top: 0;
  right: 0;
  display: grid;
  place-items: center;
  width: 44px;
  height: 44px;
  color: var(--text-secondary);
}

.login-password__toggle :deep(svg) {
  width: 17px;
  height: 17px;
}

.login-code-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: var(--space-2);
}

.login-submit {
  width: 100%;
  min-height: 44px;
  margin-top: var(--space-2);
  justify-content: center;
  gap: var(--space-2);
}

.login-wechat {
  display: grid;
  gap: var(--space-2);
}

.login-wechat__button {
  margin-top: 0;
}

.login-wechat__hint {
  color: var(--text-tertiary);
  font-size: var(--text-meta);
  line-height: var(--leading-normal);
}

.login-divider {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  margin-top: var(--space-2);
  color: var(--text-tertiary);
  font-size: var(--text-meta);
}

.login-divider::before,
.login-divider::after {
  content: '';
  height: 1px;
  flex: 1;
  background: var(--border);
}

.login-hint {
  padding: var(--space-3);
  border-radius: var(--radius-control);
  background: var(--action-subtle);
  color: var(--action-subtle-fg);
  font-size: var(--text-meta);
  overflow-wrap: anywhere;
}

.login-switch {
  color: var(--text-secondary);
  font-size: var(--text-caption);
  text-align: center;
}

.login-support {
  padding-top: var(--space-4);
  border-top: 1px solid var(--border);
  color: var(--text-tertiary);
  font-size: var(--text-meta);
  line-height: var(--leading-relaxed);
}

.login-recovered {
  display: grid;
  gap: var(--space-3);
  padding-block: var(--space-4);
  overflow-wrap: anywhere;
}

.login-recovered__icon {
  width: 28px;
  height: 28px;
  color: var(--status-success);
}

.login-recovered__list {
  display: grid;
  gap: var(--space-2);
}

.login-recovered__list dt {
  color: var(--text-tertiary);
  font-size: var(--text-meta);
}

.login-recovered__list dd {
  color: var(--text-primary);
  font-size: var(--text-body);
  font-weight: 500;
}

@media (max-width: 1023px) {
  .login-grid {
    grid-template-columns: minmax(0, 1fr);
    min-height: 0;
  }

  .login-story {
    padding: var(--space-8);
  }

  .login-form-area {
    padding: var(--space-8);
  }
}

@media (max-width: 767px) {
  .login-story {
    gap: var(--space-3);
    padding: var(--space-5);
  }

  .login-story__points {
    display: none;
  }

  .login-form-area {
    padding: var(--space-6) var(--space-5);
  }

  .login-back span {
    display: none;
  }
}
</style>

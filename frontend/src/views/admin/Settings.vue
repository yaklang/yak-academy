<script setup lang="ts">
// 管理面板：系统设置。运行期可热更新的服务参数统一在此管理；当前为 SMTP
// 邮件投递配置。环境变量只作为首次启动默认值，保存后以本页配置为准；
// 密码只写不读，保存即生效（后端 DynamicEmailSender 热替换）。
import { computed, onMounted, reactive, ref } from 'vue'
import { notify } from '@/lib/notifications'
import { formatDateTime } from '@/lib/format'
import { adminSettingsApi, type AdminSMTPSettings, type SMTPTLSMode } from '../../api'

const loading = ref(true)
const loadError = ref('')
const saving = ref(false)
const testing = ref(false)
const current = ref<AdminSMTPSettings | null>(null)
const testTo = ref('')

const form = reactive({
  enabled: false,
  addr: '',
  username: '',
  password: '',
  from: '',
  tls: 'starttls' as SMTPTLSMode,
})

const sourceLabel = computed(() => {
  switch (current.value?.source) {
    case 'database': return '管理端配置'
    case 'environment': return '环境变量默认值'
    default: return '未配置'
  }
})

const sourceTone = computed(() => {
  switch (current.value?.source) {
    case 'database': return 'info'
    case 'environment': return 'warning'
    default: return 'danger'
  }
})

const sourceNote = computed(() => {
  const value = current.value
  if (!value) return ''
  if (value.source === 'database') {
    const by = value.updated_by ? `${value.updated_by} 于 ${value.updated_at ? formatDateTime(value.updated_at) : '未知时间'} 保存` : '已保存'
    return `当前生效：管理端配置（${by}）。保存后立即热加载，无需重启服务。`
  }
  if (value.source === 'environment') {
    return '当前生效：环境变量默认值。保存后由管理端配置接管；环境变量仍可作为新部署的初始值。'
  }
  return '当前没有可用的 SMTP 配置：严格模式下邮箱验证码不可用，本地非严格模式会把验证码写入服务日志。'
})

const passwordPlaceholder = computed(() => {
  if (current.value?.password_error) return '密码无法解密，请输入新密码'
  if (current.value?.has_password) return '已保存，留空保持不变'
  return 'SMTP 密码 / API Key（无认证中继可留空）'
})

const canTest = computed(() => Boolean(
  current.value?.enabled && !current.value.config_error && !current.value.password_error,
))

function apply(value: AdminSMTPSettings) {
  current.value = value
  form.enabled = value.enabled
  form.addr = value.addr
  form.username = value.username
  form.password = ''
  form.from = value.from
  form.tls = value.tls
}

function resetForm() {
  if (current.value) apply(current.value)
}

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    apply(await adminSettingsApi.getSMTP())
  } catch (e) {
    loadError.value = e instanceof Error ? e.message : '设置加载失败，请稍后重试'
  } finally {
    loading.value = false
  }
}

async function save() {
  if (form.enabled && !form.addr.trim()) {
    notify.error('请填写 SMTP 服务器地址')
    return
  }
  if (form.enabled && !form.from.trim() && !form.username.trim()) {
    notify.error('请填写发件人地址，或填写 SMTP 用户名作为默认发件人')
    return
  }
  saving.value = true
  try {
    const value = await adminSettingsApi.updateSMTP({
      enabled: form.enabled,
      addr: form.addr.trim(),
      username: form.username.trim(),
      password: form.password,
      from: form.from.trim(),
      tls: form.tls,
    })
    apply(value)
    notify.success(value.enabled ? 'SMTP 配置已保存并立即生效' : '已停用 SMTP 投递，配置已保存')
  } catch (e) {
    notify.error(`保存失败：${(e as Error).message}`)
  } finally {
    saving.value = false
  }
}

async function sendTest() {
  if (!testTo.value.trim()) {
    notify.error('请填写测试收件邮箱')
    return
  }
  testing.value = true
  try {
    await adminSettingsApi.testSMTP(testTo.value.trim())
    notify.success(`测试邮件已发送至 ${testTo.value.trim()}，请检查收件箱与垃圾邮件`)
  } catch (e) {
    notify.error(`测试失败：${(e as Error).message}`)
  } finally {
    testing.value = false
  }
}

onMounted(load)
</script>

<template>
  <header class="page-head">
    <div>
      <h2 class="page-title">系统<span class="accent">设置</span></h2>
      <p class="page-sub">
        集中管理运行期可热更新的服务参数。环境变量仅作为首次启动默认值，
        管理端保存后立即生效；SMTP 密码加密存储且永不回显。
      </p>
    </div>
  </header>

  <UiLoadError v-if="loadError" compact :message="loadError" @retry="load" class="block" />
  <UiSkeleton v-else-if="loading" :rows="7" class="block" />
  <template v-else-if="current">
    <UiAlert
      v-if="current.config_error"
      type="error"
      show-icon
      title="已保存的 SMTP 配置损坏"
      description="请重新填写并保存；当前配置未生效，邮箱验证码不可用。"
      :closable="false"
      class="block"
    />
    <UiAlert
      v-else-if="current.password_error"
      type="warning"
      show-icon
      title="已保存的 SMTP 密码无法解密"
      description="服务器 JWT secret 可能已更换。请重新填写密码并保存，否则邮件投递不可用。"
      :closable="false"
      class="block"
    />

    <UiCard class="block" shadow="never">
      <template #header>
        <div class="card-head">
          <span>邮件服务（SMTP）</span>
          <UiTag :type="sourceTone" size="small">{{ sourceLabel }}</UiTag>
        </div>
      </template>

      <p class="source-note">{{ sourceNote }}</p>

      <form class="settings-form" @submit.prevent="save">
        <div class="setting-row">
          <div>
            <p class="setting-title">启用邮箱验证码投递</p>
            <p class="setting-help">注册与找回密码的主通道。关闭后邮箱发码接口将不可用。</p>
          </div>
          <UiSwitch v-model="form.enabled" active-text="启用邮箱验证码投递" />
        </div>

        <div class="form-grid">
          <UiFormItem label="SMTP 服务器" required>
            <UiInput v-model="form.addr" placeholder="smtp.example.com:587" autocomplete="off" />
          </UiFormItem>
          <UiFormItem label="TLS 模式" required>
            <UiSelect v-model="form.tls" placeholder="选择 TLS 模式">
              <UiOption label="STARTTLS（587 / 25）" value="starttls" />
              <UiOption label="隐式 TLS（465）" value="implicit" />
              <UiOption label="不加密（仅限可信内网）" value="none" />
            </UiSelect>
          </UiFormItem>
          <UiFormItem label="SMTP 用户名">
            <UiInput v-model="form.username" placeholder="no-reply@example.com" autocomplete="off" />
          </UiFormItem>
          <UiFormItem label="SMTP 密码 / API Key">
            <UiInput
              v-model="form.password"
              type="password"
              show-password
              :placeholder="passwordPlaceholder"
              autocomplete="new-password"
            />
          </UiFormItem>
          <UiFormItem label="发件人" class="span-2">
            <UiInput v-model="form.from" placeholder="Yak Academy <no-reply@example.com>" autocomplete="off" />
          </UiFormItem>
        </div>

        <p class="form-help">
          地址格式为 host:port，省略端口按 587；发件人留空时使用 SMTP 用户名。
          选择「不加密」时凭据会明文传输，仅适用于本机或可信内网中继。
        </p>

        <div class="form-actions">
          <UiButton :disabled="saving" @click="resetForm">重置</UiButton>
          <UiButton type="primary" native-type="submit" :loading="saving">保存配置</UiButton>
        </div>
      </form>
    </UiCard>

    <UiCard class="block" shadow="never">
      <template #header>
        <div class="card-head">
          <span>投递测试</span>
          <UiTag size="small" :type="canTest ? 'info' : 'warning'">{{ canTest ? '可测试' : '需先保存并启用' }}</UiTag>
        </div>
      </template>
      <p class="source-note">使用当前已保存并生效的配置发送一封测试邮件，不会创建验证码。</p>
      <div class="test-row">
        <UiInput
          v-model="testTo"
          type="email"
          placeholder="收件邮箱，例如 admin@example.com"
          autocomplete="email"
          class="test-input"
        />
        <UiButton type="primary" :loading="testing" :disabled="!canTest || testing" @click="sendTest">
          发送测试邮件
        </UiButton>
      </div>
      <p v-if="!canTest" class="form-help">请先保存并启用 SMTP；若密码无法解密，需重新填写密码后再测试。</p>
    </UiCard>
  </template>
</template>

<style scoped>
.block { margin-bottom: var(--space-5); }
.card-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  flex-wrap: wrap;
}
.source-note {
  max-width: 72ch;
  margin: 0 0 var(--space-5);
  color: var(--muted-foreground);
  font-size: var(--text-sm);
  line-height: var(--leading-normal);
}
.settings-form { max-width: 100%; }
.setting-row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--space-4);
  margin-bottom: var(--space-5);
  padding-bottom: var(--space-5);
  border-bottom: 1px solid var(--border);
}
.setting-title { margin: 0; font-size: var(--text-base); font-weight: 500; }
.setting-help {
  margin: var(--space-1) 0 0;
  color: var(--muted-foreground);
  font-size: var(--text-sm);
  line-height: var(--leading-normal);
}
.form-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: var(--space-4);
}
.span-2 { grid-column: span 2; }
.form-help {
  max-width: 72ch;
  margin: var(--space-4) 0 0;
  color: var(--muted-foreground);
  font-size: var(--text-sm);
  line-height: var(--leading-normal);
}
.form-actions {
  display: flex;
  justify-content: flex-end;
  gap: var(--space-2);
  margin-top: var(--space-5);
}
.test-row { display: flex; flex-wrap: wrap; gap: var(--space-2); align-items: flex-start; }
.test-input { width: 360px; max-width: 100%; }
@media (max-width: 720px) {
  .setting-row { flex-direction: column; }
  .form-grid { grid-template-columns: 1fr; }
  .span-2 { grid-column: auto; }
}
</style>

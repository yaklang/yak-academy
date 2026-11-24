<script setup lang="ts">
// 管理面板：系统设置。运行期可热更新的服务参数统一在此管理；当前为 SMTP
// 邮件投递配置。环境变量只作为首次启动默认值，保存后以本页配置为准；
// 密码只写不读，保存即生效。
import { computed, ref, reactive } from 'vue'
import { CircleAlert } from '@lucide/vue'
import { notify } from '@/lib/notifications'
import { formatDateTime } from '@/lib/format'
import PageHeader from '@/components/layout/PageHeader.vue'
import FormSection from '@/components/layout/FormSection.vue'
import FormField from '@/components/layout/FormField.vue'
import EmptyState from '@/components/layout/EmptyState.vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { adminSettingsApi, type AdminSMTPSettings, type SMTPTLSMode } from '../../api'

const loading = ref(true)
const loadError = ref('')
const saving = ref(false)
const testing = ref(false)
const current = ref<AdminSMTPSettings | null>(null)
const testTo = ref('')
const formError = ref('')
const testError = ref('')

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
  formError.value = ''
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
  formError.value = ''
  if (form.enabled && !form.addr.trim()) {
    formError.value = '请填写 SMTP 服务器地址'
    return
  }
  if (form.enabled && !form.from.trim() && !form.username.trim()) {
    formError.value = '请填写发件人地址，或填写 SMTP 用户名作为默认发件人'
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
    formError.value = e instanceof Error ? `保存失败：${e.message}` : '保存失败，请稍后重试'
  } finally {
    saving.value = false
  }
}

async function sendTest() {
  testError.value = ''
  if (!testTo.value.trim()) {
    testError.value = '请填写测试收件邮箱'
    return
  }
  testing.value = true
  try {
    await adminSettingsApi.testSMTP(testTo.value.trim())
    notify.success(`测试邮件已发送至 ${testTo.value.trim()}，请检查收件箱与垃圾邮件`)
  } catch (e) {
    testError.value = e instanceof Error ? `测试失败：${e.message}` : '测试失败，请稍后重试'
  } finally {
    testing.value = false
  }
}

load()
</script>

<template>
  <PageHeader
    title="系统设置"
    description="集中管理运行期可热更新的服务参数。环境变量只作为首次启动默认值，在此保存后立即生效。"
  >
    <template #badges>
      <span class="settings-source" :data-tone="sourceTone">{{ sourceLabel }}</span>
    </template>
  </PageHeader>

  <EmptyState
    v-if="loadError"
    tone="error"
    title="系统设置加载失败"
    :description="loadError"
    action-label="重试"
    @action="load"
  />

  <div v-else-if="loading" class="settings-loading" aria-busy="true" aria-label="正在加载系统设置">
    <Skeleton class="settings-loading__block" />
    <Skeleton class="settings-loading__block" />
  </div>

  <div v-else-if="current" class="settings-stack">
    <div v-if="current.config_error" class="settings-alert" data-tone="danger" role="alert">
      <CircleAlert aria-hidden="true" />
      <div>
        <p class="settings-alert__title">已保存的 SMTP 配置损坏</p>
        <p class="settings-alert__text">请重新填写并保存；当前配置未生效，邮箱验证码不可用。</p>
      </div>
    </div>
    <div v-else-if="current.password_error" class="settings-alert" data-tone="warning" role="alert">
      <CircleAlert aria-hidden="true" />
      <div>
        <p class="settings-alert__title">已保存的 SMTP 密码无法解密</p>
        <p class="settings-alert__text">服务器密钥可能已更换。请重新填写密码并保存，否则邮件投递不可用。</p>
      </div>
    </div>

    <form @submit.prevent="save">
      <FormSection
        title="邮件服务（SMTP）"
        description="用于发送注册验证码与找回密码邮件。密码只写不读，保存后立即生效。"
      >
        <p class="settings-note">{{ sourceNote }}</p>
        <div v-if="formError" class="settings-inline-error" role="alert">{{ formError }}</div>

        <div class="setting-row">
          <div class="setting-copy">
            <p class="setting-title">启用邮箱验证码投递</p>
            <p class="setting-help">关闭后邮箱发码接口不可用；微信注册与密码登录不受影响。</p>
          </div>
          <Switch
            id="smtp-enabled"
            v-model="form.enabled"
            aria-label="启用邮箱验证码投递"
          />
        </div>

        <div class="field-grid">
          <FormField label="SMTP 服务器" html-for="smtp-addr" required hint="格式为 host:port，省略端口按 587。">
            <template #default="{ describedby, invalid }">
              <Input
                id="smtp-addr"
                v-model="form.addr"
                placeholder="smtp.example.com:587"
                autocomplete="off"
                :aria-describedby="describedby"
                :aria-invalid="invalid"
              />
            </template>
          </FormField>
          <FormField label="TLS 模式" html-for="smtp-tls" required>
            <template #default="{ describedby, invalid }">
              <select
                id="smtp-tls"
                v-model="form.tls"
                class="settings-select"
                :aria-describedby="describedby"
                :aria-invalid="invalid"
              >
                <option value="starttls">STARTTLS（587 / 25）</option>
                <option value="implicit">隐式 TLS（465）</option>
                <option value="none">不加密（仅限可信内网）</option>
              </select>
            </template>
          </FormField>
          <FormField label="SMTP 用户名" html-for="smtp-username" hint="多数服务商要求使用完整邮箱地址。">
            <template #default="{ describedby, invalid }">
              <Input
                id="smtp-username"
                v-model="form.username"
                placeholder="no-reply@example.com"
                autocomplete="off"
                :aria-describedby="describedby"
                :aria-invalid="invalid"
              />
            </template>
          </FormField>
          <FormField label="SMTP 密码 / API Key" html-for="smtp-password">
            <template #default="{ describedby, invalid }">
              <Input
                id="smtp-password"
                v-model="form.password"
                type="password"
                :placeholder="passwordPlaceholder"
                autocomplete="new-password"
                :aria-describedby="describedby"
                :aria-invalid="invalid"
              />
            </template>
          </FormField>
          <FormField
            label="发件人"
            html-for="smtp-from"
            class="field-span-2"
            hint="留空时使用 SMTP 用户名；选择「不加密」时凭据会明文传输，仅适用于本机或可信内网中继。"
          >
            <template #default="{ describedby, invalid }">
              <Input
                id="smtp-from"
                v-model="form.from"
                placeholder="Yak Academy <no-reply@example.com>"
                autocomplete="off"
                :aria-describedby="describedby"
                :aria-invalid="invalid"
              />
            </template>
          </FormField>
        </div>

        <template #footer>
          <Button type="button" variant="outline" :disabled="saving" @click="resetForm">重置</Button>
          <Button type="submit" :disabled="saving">{{ saving ? '正在保存…' : '保存配置' }}</Button>
        </template>
      </FormSection>
    </form>

    <FormSection
      title="投递测试"
      description="使用当前已保存并生效的配置发送一封测试邮件，不会创建验证码。"
    >
      <form class="test-row" @submit.prevent="sendTest">
        <FormField label="测试收件邮箱" html-for="smtp-test-to">
          <template #default="{ describedby, invalid }">
            <Input
              id="smtp-test-to"
              v-model="testTo"
              type="email"
              placeholder="admin@example.com"
              autocomplete="email"
              :aria-describedby="describedby"
              :aria-invalid="invalid"
            />
          </template>
        </FormField>
        <Button type="submit" :disabled="!canTest || testing">
          {{ testing ? '正在发送…' : '发送测试邮件' }}
        </Button>
      </form>
      <div v-if="testError" class="settings-inline-error" role="alert">{{ testError }}</div>
      <p v-if="!canTest" class="settings-note">
        请先保存并启用 SMTP；若密码无法解密，需重新填写密码后再测试。
      </p>
    </FormSection>
  </div>
</template>

<style scoped>
.settings-source {
  display: inline-flex;
  align-items: center;
  height: 24px;
  padding-inline: var(--space-2);
  border: 1px solid var(--border);
  border-radius: var(--radius-control);
  background: var(--bg-subtle);
  color: var(--text-secondary);
  font-size: var(--text-meta);
  font-weight: 500;
  white-space: nowrap;
}

.settings-source[data-tone='info'] {
  background: var(--action-subtle);
  color: var(--action-subtle-fg);
}

.settings-source[data-tone='warning'] {
  color: var(--status-warning);
}

.settings-source[data-tone='danger'] {
  color: var(--status-danger);
}

.settings-stack {
  max-width: 880px;
}

.settings-loading {
  display: grid;
  gap: var(--space-4);
}

.settings-loading__block {
  width: 100%;
  height: 240px;
  border-radius: var(--radius-card);
}

.settings-alert {
  display: flex;
  align-items: flex-start;
  gap: var(--space-3);
  margin-bottom: var(--space-4);
  padding: var(--space-3) var(--space-4);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  background: var(--bg-surface);
}

.settings-alert[data-tone='danger'] {
  border-color: color-mix(in srgb, var(--status-danger) 30%, transparent);
  background: color-mix(in srgb, var(--status-danger) 6%, var(--bg-surface));
}

.settings-alert[data-tone='warning'] {
  border-color: color-mix(in srgb, var(--status-warning) 30%, transparent);
  background: color-mix(in srgb, var(--status-warning) 8%, var(--bg-surface));
}

.settings-alert :deep(svg) {
  width: 16px;
  height: 16px;
  flex-shrink: 0;
  margin-top: var(--space-1);
}

.settings-alert[data-tone='danger'] :deep(svg) {
  color: var(--status-danger);
}

.settings-alert[data-tone='warning'] :deep(svg) {
  color: var(--status-warning);
}

.settings-alert__title {
  color: var(--text-primary);
  font-size: var(--text-caption);
  font-weight: 600;
}

.settings-alert__text {
  margin-top: var(--space-1);
  color: var(--text-secondary);
  font-size: var(--text-meta);
  line-height: var(--leading-normal);
}

.settings-note {
  max-width: 72ch;
  color: var(--text-secondary);
  font-size: var(--text-meta);
  line-height: var(--leading-relaxed);
}

.settings-inline-error {
  padding: var(--space-2) var(--space-3);
  border: 1px solid color-mix(in srgb, var(--status-danger) 30%, transparent);
  border-radius: var(--radius-control);
  background: color-mix(in srgb, var(--status-danger) 6%, var(--bg-surface));
  color: var(--status-danger);
  font-size: var(--text-meta);
  line-height: var(--leading-normal);
}

.setting-row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--space-4);
  padding-block: var(--space-4);
  border-top: 1px solid var(--border);
  border-bottom: 1px solid var(--border);
}

.setting-copy {
  min-width: 0;
}

.setting-title {
  color: var(--text-primary);
  font-size: var(--text-body);
  font-weight: 500;
}

.setting-help {
  margin-top: var(--space-1);
  color: var(--text-tertiary);
  font-size: var(--text-meta);
  line-height: var(--leading-normal);
}

.field-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: var(--space-4);
}

.field-span-2 {
  grid-column: span 2;
}

.settings-select {
  width: 100%;
  min-height: 44px;
  padding-inline: var(--space-2);
  border: 1px solid var(--input-border);
  border-radius: var(--radius-control);
  background: var(--bg-surface);
  color: var(--text-primary);
  font-size: var(--text-caption);
}

.test-row {
  display: flex;
  align-items: flex-end;
  gap: var(--space-3);
  flex-wrap: wrap;
}

.test-row :deep(.form-field) {
  flex: 1 1 280px;
}

@media (max-width: 720px) {
  .setting-row {
    flex-direction: column;
  }

  .field-grid {
    grid-template-columns: 1fr;
  }

  .field-span-2 {
    grid-column: auto;
  }

  .test-row {
    align-items: stretch;
  }
}
</style>
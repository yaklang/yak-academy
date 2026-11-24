<script setup lang="ts">
// 账号信息面板：身份卡（头像/资料/简介）+ 能力画像（按题目分类的通关覆盖）
// + 账号安全。总得分来自事件派生读模型，本页只读展示。
// 简介可编辑（≤500 字）；头像 png/jpg/gif/webp ≤2MB，重复上传覆盖。
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { CalendarDays, Copy, KeyRound, Mail, ShieldCheck, UserRound } from '@lucide/vue'
import { notify } from '@/lib/notifications'
import { authApi, profileApi, type CapabilityProfile, type UserProfile } from '../api'
import { useAuthStore } from '../stores/auth'
import CapabilityRadar from '@/components/CapabilityRadar.vue'
import PageHeader from '@/components/layout/PageHeader.vue'
import EmptyState from '@/components/layout/EmptyState.vue'
import StatusTag from '@/components/layout/StatusTag.vue'
import MetricStrip from '@/components/layout/MetricStrip.vue'
import StatCard from '@/components/layout/StatCard.vue'
import FormField from '@/components/layout/FormField.vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

const auth = useAuthStore()
const router = useRouter()
const profile = ref<UserProfile | null>(null)
const capabilities = ref<CapabilityProfile | null>(null)
const loading = ref(true)
const loadError = ref('')
const capabilitiesLoading = ref(false)
const capabilitiesError = ref('')
const bio = ref('')
const editing = ref(false)
const bioLoading = ref(false)
const avatarUploading = ref(false)
const avatarUrl = ref<string | null>(null)

const pwdVisible = ref(false)
const pwdForm = reactive({ current: '', next: '', confirm: '' })
const pwdLoading = ref(false)
const pwdError = ref('')

// 微信注册账号没有密码（password_set=false）：隐藏改密入口，避免必然失败的请求；
// 管理员重置密码后 /me 会返回 true，入口自动恢复。
const canChangePassword = computed(() => auth.user?.password_set !== false)
const roleLabel = (role: UserProfile['global_role']) => (role === 'admin' ? '管理员' : '学员')
const formatDate = (iso: string) => new Date(iso).toLocaleDateString('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit' })
// 总数与雷达图同一口径（当前可见题目），避免卡片内部出现
// “累计通关 167 题”与各分类 0% 并存的自相矛盾。
const visibleSolved = computed(() => capabilities.value?.solved ?? 0)
const visibleTotal = computed(() => capabilities.value?.total ?? 0)
// 覆盖率按「已有通关的分类 / 已发布分类」呈现：题目总数为 0 时同样成立。
const masteredCategories = () => capabilities.value?.dimensions.filter((d) => d.solved > 0).length ?? 0
const categoryCount = () => capabilities.value?.dimensions.length ?? 0

async function loadCapabilities() {
  capabilitiesLoading.value = true
  capabilitiesError.value = ''
  try {
    capabilities.value = await profileApi.capabilities()
  } catch (e) {
    capabilities.value = null
    capabilitiesError.value = (e as Error).message
  } finally {
    capabilitiesLoading.value = false
  }
}

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    profile.value = await profileApi.profile()
    bio.value = profile.value.bio
    // 头像需带 JWT 拉 blob（<img> 直连无法带头），本地 URL 展示。
    if (profile.value.avatar) {
      try {
        const blob = await profileApi.avatarBlob()
        avatarUrl.value = URL.createObjectURL(blob)
      } catch {
        avatarUrl.value = null
      }
    } else {
      avatarUrl.value = null
    }
  } catch (e) {
    profile.value = null
    loadError.value = (e as Error).message
  } finally {
    loading.value = false
  }
}

async function copyAccountID() {
  if (!profile.value) return
  try {
    await navigator.clipboard.writeText(String(profile.value.id))
    notify.success('账号编码已复制')
  } catch {
    notify.warning('浏览器未授权剪贴板，请手动选择复制')
  }
}

async function saveBio() {
  if (bio.value.length > 500) {
    notify.warning('简介不能超过 500 字')
    return
  }
  bioLoading.value = true
  try {
    profile.value = await profileApi.updateBio(bio.value)
    editing.value = false
    notify.success('简介已更新')
  } catch (e) {
    notify.error((e as Error).message)
  } finally {
    bioLoading.value = false
  }
}

async function onAvatarChange(file: File) {
  const okTypes = ['image/png', 'image/jpeg', 'image/gif', 'image/webp']
  if (!okTypes.includes(file.type)) {
    notify.warning('仅支持 png / jpg / gif / webp 图片')
    return
  }
  if (file.size > 2 * 1024 * 1024) {
    notify.warning('头像不能超过 2MB')
    return
  }
  avatarUploading.value = true
  try {
    await profileApi.uploadAvatar(file)
    notify.success('头像已更新')
    await load()
  } catch (e) {
    notify.error((e as Error).message)
  } finally {
    avatarUploading.value = false
  }
}

async function openPwd() {
  pwdError.value = ''
  pwdVisible.value = true
}

async function submitChangePassword() {
  pwdError.value = ''
  if (!pwdForm.current || pwdForm.next.length < 6) {
    pwdError.value = '请填写当前密码，且新密码至少 6 位'
    return
  }
  if (pwdForm.next !== pwdForm.confirm) {
    pwdError.value = '两次输入的新密码不一致'
    return
  }
  pwdLoading.value = true
  try {
    await authApi.changePassword(pwdForm.current, pwdForm.next)
    pwdVisible.value = false
    notify.success('密码已修改，请重新登录')
    // 改密后全部 token 失效：清本地态回登录页
    await auth.logout()
    router.push('/login')
  } catch (e) {
    pwdError.value = (e as Error).message
  } finally {
    pwdLoading.value = false
  }
}

onMounted(() => {
  load()
  loadCapabilities()
})
</script>

<template>
  <PageHeader
    title="账号信息"
    description="查看身份资料与能力画像，管理头像、简介和登录密码。"
  >
    <template #actions>
      <Button variant="outline" as-child>
        <RouterLink to="/mailbox">
          <Mail aria-hidden="true" />
          站内邮箱
        </RouterLink>
      </Button>
    </template>
  </PageHeader>

  <div v-if="loading" class="profile-grid" role="status" aria-label="正在加载账号信息">
    <div v-for="column in 2" :key="column" class="profile-column" aria-hidden="true">
      <div class="profile-panel">
        <span class="sk-line sk-line--title" />
        <span class="sk-line" />
        <span class="sk-line sk-line--short" />
      </div>
      <div class="profile-panel">
        <span class="sk-line sk-line--title" />
        <span class="sk-line" />
      </div>
    </div>
  </div>
  <EmptyState
    v-else-if="loadError"
    tone="error"
    title="账号信息加载失败"
    :description="loadError"
    action-label="重试"
    @action="load"
  />
  <div v-else-if="profile" class="profile-grid">
    <div class="profile-column">
      <section class="profile-panel" aria-labelledby="identity-title">
        <header class="profile-panel__head">
          <div>
            <h2 id="identity-title" class="profile-panel__title">身份资料</h2>
            <p class="profile-panel__hint">账号编码与注册信息由平台分配，不可修改。</p>
          </div>
          <StatusTag kind="role" :value="profile.global_role" />
        </header>

        <div class="identity">
          <div class="avatar-frame">
            <img v-if="avatarUrl" :src="avatarUrl" alt="当前头像" class="avatar-img" />
            <span v-else class="avatar-initial" aria-hidden="true">{{ profile.username.slice(0, 1).toUpperCase() }}</span>
            <label class="avatar-upload" :data-busy="avatarUploading ? 'true' : undefined">
              {{ avatarUploading ? '上传中…' : '更换头像' }}
              <input
                type="file"
                accept="image/png,image/jpeg,image/gif,image/webp"
                :disabled="avatarUploading"
                @change="(event) => {
                  const file = (event.target as HTMLInputElement).files?.[0]
                  if (file) onAvatarChange(file)
                }"
              />
            </label>
          </div>

          <div class="identity-copy">
            <p class="identity-name">{{ profile.username }}</p>
            <dl class="facts">
              <div class="fact">
                <dt><UserRound aria-hidden="true" />账号编码</dt>
                <dd>
                  <span class="fact__value">{{ profile.id }}</span>
                  <button type="button" class="icon-button" aria-label="复制账号编码" @click="copyAccountID">
                    <Copy aria-hidden="true" />
                  </button>
                </dd>
              </div>
              <div class="fact">
                <dt><CalendarDays aria-hidden="true" />注册时间</dt>
                <dd>{{ formatDate(profile.created_at) }}</dd>
              </div>
            </dl>
          </div>
        </div>

        <div class="profile-bio">
          <div class="profile-bio__head">
            <h3 class="profile-bio__title">简介</h3>
            <Button v-if="!editing" size="sm" variant="outline" @click="editing = true">编辑</Button>
            <div v-else class="profile-bio__actions">
              <Button size="sm" :loading="bioLoading" @click="saveBio">保存</Button>
              <Button
                size="sm"
                variant="outline"
                @click="() => { editing = false; bio = profile?.bio ?? '' }"
              >取消</Button>
            </div>
          </div>
          <textarea
            v-if="editing"
            v-model="bio"
            class="profile-bio__input"
            rows="4"
            maxlength="500"
            placeholder="介绍一下自己（≤500 字）"
          />
          <p v-else class="profile-bio__text">{{ profile.bio || '暂无简介，写一句介绍自己吧。' }}</p>
        </div>
      </section>

      <section class="profile-panel" aria-labelledby="security-title">
        <header class="profile-panel__head">
          <div>
            <h2 id="security-title" class="profile-panel__title">账号安全</h2>
            <p class="profile-panel__hint">
              {{ canChangePassword ? '修改密码后全部已登录会话失效，需要重新登录。' : '微信注册账号通过微信登录，不设置平台密码。' }}
            </p>
          </div>
        </header>
        <div class="security-row">
          <span class="security-icon"><KeyRound aria-hidden="true" /></span>
          <div class="security-copy">
            <p>登录密码</p>
            <span>{{ canChangePassword ? '建议使用与其他平台不同的密码。' : '如需使用密码登录，请联系管理员重置密码。' }}</span>
          </div>
          <Button v-if="canChangePassword" size="sm" variant="outline" @click="openPwd">修改密码</Button>
          <StatusTag v-else kind="role" value="student" label="微信登录" />
        </div>
      </section>
    </div>

    <div class="profile-column">
      <section class="profile-panel" data-testid="capability-card" aria-labelledby="capability-title">
        <header class="profile-panel__head">
          <div>
            <h2 id="capability-title" class="profile-panel__title">能力画像</h2>
            <p class="profile-panel__hint">按题目分类统计通关覆盖，只计当前可见题目。</p>
          </div>
          <ShieldCheck class="profile-panel__icon" aria-hidden="true" />
        </header>

        <MetricStrip :columns="3">
          <StatCard label="总得分" :value="profile.score" tone="primary" />
          <StatCard label="可见题通关" :value="`${visibleSolved}/${visibleTotal}`" />
          <StatCard label="已掌握分类" :value="`${masteredCategories()}/${categoryCount()}`" />
        </MetricStrip>

        <div v-if="capabilitiesLoading" class="capability-state" role="status" aria-label="正在加载能力画像">
          <span class="sk-radar" aria-hidden="true" />
        </div>
        <EmptyState
          v-else-if="capabilitiesError"
          compact
          tone="error"
          title="能力画像加载失败"
          :description="capabilitiesError"
          action-label="重试"
          @action="loadCapabilities"
        />
        <CapabilityRadar v-else-if="capabilities" :dimensions="capabilities.dimensions" />

        <p v-if="capabilities && capabilities.dimensions.length" class="capability-note">
          总得分为历史累计；覆盖率 = 该分类已通关题数 / 该类可见题数，题库更新后自动重算。
        </p>
      </section>
    </div>
  </div>

  <UiDialog v-model="pwdVisible" title="修改密码" width="420px">
    <div class="pwd-form">
      <p v-if="pwdError" class="pwd-error" role="alert">{{ pwdError }}</p>
      <FormField label="当前密码" html-for="pwd-current">
        <template #default="{ id }">
          <Input :id="id" v-model="pwdForm.current" type="password" autocomplete="current-password" />
        </template>
      </FormField>
      <FormField label="新密码" html-for="pwd-next" hint="至少 6 位">
        <template #default="{ id, describedby }">
          <Input :id="id" v-model="pwdForm.next" type="password" autocomplete="new-password" placeholder="至少 6 位" :aria-describedby="describedby" />
        </template>
      </FormField>
      <FormField label="确认新密码" html-for="pwd-confirm">
        <template #default="{ id }">
          <Input :id="id" v-model="pwdForm.confirm" type="password" autocomplete="new-password" />
        </template>
      </FormField>
    </div>
    <template #footer>
      <Button variant="outline" @click="pwdVisible = false">取消</Button>
      <Button :loading="pwdLoading" @click="submitChangePassword">确认修改</Button>
    </template>
  </UiDialog>
</template>

<style scoped>
.profile-grid {
  display: grid;
  grid-template-columns: minmax(0, 1.05fr) minmax(0, 0.95fr);
  align-items: start;
  gap: var(--space-4);
}

.profile-column {
  display: grid;
  gap: var(--space-4);
  min-width: 0;
}

.profile-panel {
  min-width: 0;
  padding: var(--space-5);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  background: var(--bg-surface);
}

.profile-panel__head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--space-3);
  margin-bottom: var(--space-4);
}

.profile-panel__title {
  color: var(--text-primary);
  font-size: var(--text-section);
  font-weight: 600;
}

.profile-panel__hint {
  margin-top: var(--space-1);
  color: var(--text-tertiary);
  font-size: var(--text-meta);
  line-height: var(--leading-normal);
}

.profile-panel__icon {
  width: 18px;
  height: 18px;
  color: var(--text-tertiary);
  flex-shrink: 0;
}

.identity {
  display: flex;
  align-items: center;
  gap: var(--space-5);
  margin-bottom: var(--space-5);
}

.avatar-frame {
  display: grid;
  justify-items: center;
  gap: var(--space-2);
  flex-shrink: 0;
}

.avatar-img,
.avatar-initial {
  display: grid;
  place-items: center;
  width: 80px;
  height: 80px;
  border-radius: 50%;
  border: 1px solid var(--border);
  background: var(--action-subtle);
  color: var(--action-subtle-fg);
  font-size: var(--text-display);
  font-weight: 600;
  object-fit: cover;
}

.avatar-upload {
  position: relative;
  overflow: hidden;
  padding: var(--space-1) var(--space-2);
  border: 1px solid var(--border);
  border-radius: var(--radius-control);
  background: var(--bg-surface);
  color: var(--text-secondary);
  font-size: var(--text-micro);
  cursor: pointer;
}

.avatar-upload:hover {
  background: var(--bg-hover);
}

.avatar-upload[data-busy='true'] {
  opacity: 0.6;
  cursor: wait;
}

.avatar-upload input {
  position: absolute;
  inset: 0;
  opacity: 0;
  cursor: pointer;
}

.identity-copy {
  display: grid;
  gap: var(--space-3);
  min-width: 0;
}

.identity-name {
  color: var(--text-primary);
  font-size: var(--text-section);
  font-weight: 600;
  overflow-wrap: anywhere;
}

.facts {
  display: grid;
  gap: var(--space-2);
}

.fact {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  font-size: var(--text-caption);
}

.fact dt {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  min-width: 88px;
  color: var(--text-tertiary);
}

.fact dt :deep(svg) {
  width: 14px;
  height: 14px;
}

.fact dd {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  color: var(--text-primary);
}

.fact__value {
  font-family: var(--font-mono);
  font-size: var(--text-meta);
}

.icon-button {
  display: inline-grid;
  width: 24px;
  height: 24px;
  place-items: center;
  border-radius: var(--radius-control);
  color: var(--text-tertiary);
}

.icon-button:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.icon-button :deep(svg) {
  width: 14px;
  height: 14px;
}

.profile-bio {
  display: grid;
  gap: var(--space-2);
  padding-top: var(--space-4);
  border-top: 1px solid var(--border);
}

.profile-bio__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
}

.profile-bio__title {
  color: var(--text-primary);
  font-size: var(--text-caption);
  font-weight: 600;
}

.profile-bio__actions {
  display: flex;
  gap: var(--space-2);
}

.profile-bio__input {
  width: 100%;
  padding: var(--space-2) var(--space-3);
  border: 1px solid var(--border);
  border-radius: var(--radius-control);
  background: var(--bg-surface);
  color: var(--text-primary);
  font-size: var(--text-caption);
  line-height: var(--leading-relaxed);
  resize: vertical;
}

.profile-bio__text {
  color: var(--text-secondary);
  font-size: var(--text-caption);
  line-height: var(--leading-relaxed);
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

.security-row {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex-wrap: wrap;
}

.security-icon {
  display: inline-grid;
  width: 34px;
  height: 34px;
  flex-shrink: 0;
  place-items: center;
  border: 1px solid var(--border);
  border-radius: var(--radius-control);
  background: var(--bg-subtle);
  color: var(--text-secondary);
}

.security-icon :deep(svg) {
  width: 16px;
  height: 16px;
}

.security-copy {
  flex: 1;
  min-width: 160px;
}

.security-copy p {
  color: var(--text-primary);
  font-size: var(--text-caption);
  font-weight: 500;
}

.security-copy span {
  color: var(--text-tertiary);
  font-size: var(--text-meta);
}

.capability-state {
  display: grid;
  justify-items: center;
  align-content: center;
  gap: var(--space-3);
  min-height: 220px;
}

.sk-radar {
  display: block;
  width: min(100%, 260px);
  aspect-ratio: 1;
  border-radius: 50%;
  background: var(--bg-subtle);
}

.capability-note {
  margin-top: var(--space-3);
  color: var(--text-tertiary);
  font-size: var(--text-meta);
  line-height: var(--leading-normal);
}

.sk-line {
  display: block;
  width: 100%;
  height: 12px;
  border-radius: var(--radius-pill);
  background: var(--bg-subtle);
}

.sk-line--title {
  width: 42%;
  height: 16px;
}

.sk-line--short {
  width: 28%;
}

.pwd-form {
  display: grid;
  gap: var(--space-4);
}

.pwd-error {
  padding: var(--space-3);
  border-radius: var(--radius-control);
  background: color-mix(in srgb, var(--status-danger) 10%, var(--bg-surface));
  color: var(--status-danger);
  font-size: var(--text-caption);
}

@media (max-width: 1023px) {
  .profile-grid {
    grid-template-columns: minmax(0, 1fr);
  }
}

@media (max-width: 560px) {
  .identity {
    align-items: flex-start;
    gap: var(--space-4);
  }

  .fact {
    flex-wrap: wrap;
    row-gap: var(--space-1);
  }

  .profile-panel {
    padding: var(--space-4);
  }
}
</style>
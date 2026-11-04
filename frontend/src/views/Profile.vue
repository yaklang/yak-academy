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
    notify.success('账号 ID 已复制')
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

async function submitChangePassword() {
  if (!pwdForm.current || pwdForm.next.length < 6) {
    notify.warning('请填写当前密码，且新密码至少 6 位')
    return
  }
  if (pwdForm.next !== pwdForm.confirm) {
    notify.warning('两次输入的新密码不一致')
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
    notify.error((e as Error).message)
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
  <header class="page-head">
    <div>
      <h2 class="page-title">账号<span class="accent">信息</span></h2>
      <p class="page-sub">查看身份资料与能力画像，管理头像、简介和登录密码。</p>
    </div>
    <div class="page-actions">
      <router-link to="/mailbox" class="head-link"><Mail :size="15" aria-hidden="true" />站内邮箱</router-link>
    </div>
  </header>

  <UiSkeleton v-if="loading" :rows="6" animated />
  <UiCard v-else-if="loadError" class="card">
    <div class="load-error" role="alert">
      <p>{{ loadError }}</p>
      <UiButton size="small" @click="load">重试</UiButton>
    </div>
  </UiCard>
  <div v-else-if="profile" class="profile-grid">
    <div class="column">
      <UiCard class="card">
        <template #header>
          <div class="card-head">
            <div>
              <h3 class="card-title">身份资料</h3>
              <p class="card-hint">账号编码与注册信息由平台分配，不可修改。</p>
            </div>
            <UiTag size="small" effect="plain">{{ roleLabel(profile.global_role) }}</UiTag>
          </div>
        </template>

        <div class="identity">
          <div class="avatar-frame">
            <UiAvatar :size="88" :src="avatarUrl ?? undefined" class="avatar">
              {{ profile.username.slice(0, 1).toUpperCase() }}
            </UiAvatar>
            <label class="upload" :class="{ disabled: avatarUploading }">
              {{ avatarUploading ? '上传中…' : '更换头像' }}
              <input
                type="file"
                accept="image/png,image/jpeg,image/gif,image/webp"
                :disabled="avatarUploading"
                @change="(e) => {
                  const f = (e.target as HTMLInputElement).files?.[0]
                  if (f) onAvatarChange(f)
                }"
              />
            </label>
          </div>

          <div class="identity-copy">
            <p class="identity-name">{{ profile.username }}</p>
            <dl class="facts">
              <div class="fact">
                <dt><UserRound :size="14" aria-hidden="true" />账号 ID</dt>
                <dd>
                  <span class="mono">{{ profile.id }}</span>
                  <button type="button" class="icon-button" aria-label="复制账号 ID" @click="copyAccountID">
                    <Copy :size="13" aria-hidden="true" />
                  </button>
                </dd>
              </div>
              <div class="fact">
                <dt><CalendarDays :size="14" aria-hidden="true" />注册时间</dt>
                <dd>{{ formatDate(profile.created_at) }}</dd>
              </div>
            </dl>
          </div>
        </div>

        <div class="bio-block">
          <div class="bio-head">
            <h4>描述简介</h4>
            <UiButton v-if="!editing" size="small" @click="editing = true">编辑</UiButton>
            <div v-else class="bio-actions">
              <UiButton size="small" :loading="bioLoading" @click="saveBio">保存</UiButton>
              <UiButton
                size="small"
                @click="
                  () => {
                    editing = false
                    bio = profile?.bio ?? ''
                  }
                "
              >取消</UiButton>
            </div>
          </div>
          <UiInput
            v-if="editing"
            v-model="bio"
            type="textarea"
            :rows="4"
            maxlength="500"
            show-word-limit
            placeholder="介绍一下自己（≤500 字）"
          />
          <p v-else class="bio-text">{{ profile.bio || '暂无简介，写一句介绍自己吧。' }}</p>
        </div>
      </UiCard>

      <UiCard class="card">
        <template #header>
          <div class="card-head">
            <div>
              <h3 class="card-title">账号安全</h3>
              <p class="card-hint">修改密码后全部已登录会话失效，需要重新登录。</p>
            </div>
          </div>
        </template>
        <div class="security-row">
          <span class="security-icon"><KeyRound :size="16" aria-hidden="true" /></span>
          <div class="security-copy">
            <p>登录密码</p>
            <span>建议使用与其他平台不同的密码。</span>
          </div>
          <UiButton size="small" @click="pwdVisible = true">修改密码</UiButton>
        </div>
      </UiCard>
    </div>

    <div class="column">
      <UiCard class="card" data-testid="capability-card">
        <template #header>
          <div class="card-head">
            <div>
              <h3 class="card-title">能力画像</h3>
              <p class="card-hint">按题目分类统计通关覆盖，只计当前可见题目。</p>
            </div>
            <ShieldCheck class="head-icon" :size="18" aria-hidden="true" />
          </div>
        </template>

        <div class="score-strip">
          <div class="score-cell">
            <span class="score-value mono">{{ profile.score }}</span>
            <span class="score-label">总得分</span>
          </div>
          <div class="score-cell">
            <span class="score-value mono">{{ visibleSolved }}<span class="score-total">/{{ visibleTotal }}</span></span>
            <span class="score-label">可见题通关</span>
          </div>
          <div class="score-cell">
            <span class="score-value mono">{{ masteredCategories() }}<span class="score-total">/{{ categoryCount() }}</span></span>
            <span class="score-label">已掌握分类</span>
          </div>
        </div>

        <div v-if="capabilitiesLoading" class="capability-state" role="status" aria-label="正在加载能力画像">
          <UiSkeleton :rows="3" animated />
        </div>
        <div v-else-if="capabilitiesError" class="capability-state" role="alert">
          <p>{{ capabilitiesError }}</p>
          <UiButton size="small" @click="loadCapabilities">重试</UiButton>
        </div>
        <CapabilityRadar v-else-if="capabilities" :dimensions="capabilities.dimensions" />

        <p v-if="capabilities && capabilities.dimensions.length" class="capability-note">
          总得分为历史累计；覆盖率 = 该分类已通关题数 / 该类可见题数，题库更新后自动重算。
        </p>
      </UiCard>
    </div>
  </div>

  <UiDialog v-model="pwdVisible" title="修改密码" width="420px">
    <UiForm label-width="90px">
      <UiFormItem label="当前密码">
        <UiInput v-model="pwdForm.current" type="password" show-password />
      </UiFormItem>
      <UiFormItem label="新密码">
        <UiInput v-model="pwdForm.next" type="password" show-password placeholder="至少 6 位" />
      </UiFormItem>
      <UiFormItem label="确认新密码">
        <UiInput v-model="pwdForm.confirm" type="password" show-password />
      </UiFormItem>
    </UiForm>
    <template #footer>
      <UiButton @click="pwdVisible = false">取消</UiButton>
      <UiButton type="primary" :loading="pwdLoading" @click="submitChangePassword">确认修改</UiButton>
    </template>
  </UiDialog>
</template>

<style scoped>
.head-link {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  color: var(--accent-foreground);
  font-size: var(--text-sm);
  text-decoration: none;
}
.head-link:hover { text-decoration: underline; text-underline-offset: 3px; }

.profile-grid {
  display: grid;
  grid-template-columns: minmax(0, 1.05fr) minmax(0, 0.95fr);
  align-items: start;
  gap: var(--space-5);
}
.column { display: grid; gap: var(--space-5); min-width: 0; }

.card-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--space-3);
}
.card-title { margin: 0; color: var(--foreground); font-size: var(--text-md); font-weight: 600; letter-spacing: 0; }
.card-hint { margin: var(--space-1) 0 0; color: var(--muted-foreground); font-size: var(--text-xs); line-height: var(--leading-normal); }
.head-icon { color: var(--accent-foreground); flex-shrink: 0; }

.identity { display: flex; align-items: center; gap: var(--space-5); margin-bottom: var(--space-5); }
.avatar-frame { display: grid; justify-items: center; gap: var(--space-2); flex-shrink: 0; }
.avatar { font-size: 30px; border-color: var(--border); }
.upload {
  position: relative;
  overflow: hidden;
  padding: 4px 10px;
  border: 1px solid var(--border);
  border-radius: var(--radius-control);
  background: var(--card);
  color: var(--foreground);
  font-size: var(--text-xs);
  cursor: pointer;
  transition: border-color 150ms ease, background-color 150ms ease;
}
.upload:hover { border-color: color-mix(in srgb, var(--primary) 45%, var(--border)); background: var(--accent); }
.upload.disabled { opacity: .55; cursor: wait; }
.upload input { position: absolute; inset: 0; opacity: 0; cursor: pointer; }

.identity-copy { display: grid; gap: var(--space-3); min-width: 0; }
.identity-name { margin: 0; color: var(--foreground); font-size: var(--text-lg); font-weight: 600; letter-spacing: 0; overflow-wrap: anywhere; }
.facts { display: grid; gap: var(--space-2); margin: 0; }
.fact { display: flex; align-items: center; gap: var(--space-3); font-size: var(--text-sm); }
.fact dt { display: inline-flex; align-items: center; gap: var(--space-1); color: var(--muted-foreground); min-width: 84px; }
.fact dd { display: inline-flex; align-items: center; gap: var(--space-1); margin: 0; color: var(--foreground); }
.icon-button {
  display: inline-grid;
  width: 24px;
  height: 24px;
  place-items: center;
  border: 0;
  border-radius: var(--radius-control);
  background: transparent;
  color: var(--muted-foreground);
}
.icon-button:hover { background: var(--accent); color: var(--accent-foreground); }

.bio-block { display: grid; gap: var(--space-2); padding-top: var(--space-4); border-top: 1px solid var(--border); }
.bio-head { display: flex; align-items: center; justify-content: space-between; gap: var(--space-3); }
.bio-head h4 { margin: 0; color: var(--foreground); font-size: var(--text-sm); font-weight: 600; }
.bio-actions { display: flex; gap: var(--space-2); }
.bio-text { margin: 0; color: var(--foreground); font-size: var(--text-sm); line-height: var(--leading-relaxed); white-space: pre-wrap; overflow-wrap: anywhere; }

.security-row { display: flex; align-items: center; gap: var(--space-3); }
.security-icon {
  display: inline-grid;
  width: 34px;
  height: 34px;
  flex-shrink: 0;
  place-items: center;
  border: 1px solid var(--border);
  border-radius: var(--radius-control);
  background: var(--card);
  color: var(--accent-foreground);
}
.security-copy { flex: 1; min-width: 0; }
.security-copy p { margin: 0; color: var(--foreground); font-size: var(--text-sm); font-weight: 500; }
.security-copy span { color: var(--muted-foreground); font-size: var(--text-xs); }

.score-strip {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: var(--space-3);
  margin-bottom: var(--space-5);
  padding-bottom: var(--space-5);
  border-bottom: 1px solid var(--border);
}
.score-cell { display: grid; gap: 2px; min-width: 0; }
.score-value { color: var(--primary); font-size: var(--text-xl); font-weight: 650; line-height: var(--leading-tight); }
.score-total { color: var(--muted-foreground); font-size: var(--text-sm); }
.score-label { color: var(--muted-foreground); font-size: var(--text-xs); }

.capability-state { min-height: 220px; display: grid; justify-items: center; align-content: center; gap: var(--space-3); }
.capability-state p { margin: 0; color: var(--muted-foreground); font-size: var(--text-sm); }
.capability-note { margin: var(--space-4) 0 0; color: var(--muted-foreground); font-size: var(--text-xs); line-height: var(--leading-normal); }

.load-error { display: grid; justify-items: start; gap: var(--space-3); }
.load-error p { margin: 0; color: var(--muted-foreground); font-size: var(--text-sm); }

@media (max-width: 1023px) {
  .profile-grid { grid-template-columns: minmax(0, 1fr); }
}
@media (max-width: 560px) {
  .identity { align-items: flex-start; gap: var(--space-4); }
  .fact { flex-wrap: wrap; row-gap: 2px; }
  .score-strip { gap: var(--space-2); }
  .score-value { font-size: var(--text-lg); }
}
</style>

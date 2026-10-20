<script setup lang="ts">
// 账号信息面板：内置账号 ID、账号名称、描述简介、总得分（事件派生只读）、头像上传、
// 修改密码（自布局头部移入，账号安全操作集中在账号信息内）。
// 简介可编辑（≤500 字）；头像 png/jpg/gif/webp ≤2MB，重复上传覆盖。
import { onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { authApi, profileApi, type UserProfile } from '../api'
import { useAuthStore } from '../stores/auth'

const auth = useAuthStore()
const router = useRouter()
const profile = ref<UserProfile | null>(null)
const loading = ref(false)
const bio = ref('')
const editing = ref(false)
const bioLoading = ref(false)
const avatarUploading = ref(false)
const avatarUrl = ref<string | null>(null)

const pwdVisible = ref(false)
const pwdForm = reactive({ current: '', next: '', confirm: '' })
const pwdLoading = ref(false)

async function load() {
  loading.value = true
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
    ElMessage.error((e as Error).message)
  } finally {
    loading.value = false
  }
}

async function saveBio() {
  if (bio.value.length > 500) {
    ElMessage.warning('简介不能超过 500 字')
    return
  }
  bioLoading.value = true
  try {
    profile.value = await profileApi.updateBio(bio.value)
    editing.value = false
    ElMessage.success('简介已更新')
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    bioLoading.value = false
  }
}

async function onAvatarChange(file: File) {
  const okTypes = ['image/png', 'image/jpeg', 'image/gif', 'image/webp']
  if (!okTypes.includes(file.type)) {
    ElMessage.warning('仅支持 png / jpg / gif / webp 图片')
    return
  }
  if (file.size > 2 * 1024 * 1024) {
    ElMessage.warning('头像不能超过 2MB')
    return
  }
  avatarUploading.value = true
  try {
    await profileApi.uploadAvatar(file)
    ElMessage.success('头像已更新')
    await load()
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    avatarUploading.value = false
  }
}

async function submitChangePassword() {
  if (!pwdForm.current || pwdForm.next.length < 6) {
    ElMessage.warning('请填写当前密码，且新密码至少 6 位')
    return
  }
  if (pwdForm.next !== pwdForm.confirm) {
    ElMessage.warning('两次输入的新密码不一致')
    return
  }
  pwdLoading.value = true
  try {
    await authApi.changePassword(pwdForm.current, pwdForm.next)
    pwdVisible.value = false
    ElMessage.success('密码已修改，请重新登录')
    // 改密后全部 token 失效：清本地态回登录页
    await auth.logout()
    router.push('/login')
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    pwdLoading.value = false
  }
}

onMounted(load)
</script>

<template>
  <h2 class="page-title">账号<span class="accent">信息</span></h2>
  <p class="hint">账号 ID、名称、简介与总得分；头像支持 png / jpg / gif / webp（≤2MB）。</p>

  <el-skeleton v-if="loading" :rows="4" animated />
  <div v-else-if="profile" class="panel">
    <div class="avatar-wrap">
      <el-avatar :size="96" :src="avatarUrl ?? undefined" class="avatar">
        {{ profile.username.slice(0, 1).toUpperCase() }}
      </el-avatar>
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

    <el-descriptions :column="2" border class="desc">
      <el-descriptions-item label="账号 ID">{{ profile.id }}</el-descriptions-item>
      <el-descriptions-item label="账号名称">{{ profile.username }}</el-descriptions-item>
      <el-descriptions-item label="角色">
        {{ profile.global_role === 'admin' ? '管理员' : '普通人' }}
      </el-descriptions-item>
      <el-descriptions-item label="总得分">{{ profile.score }}（解题 {{ profile.solve_count }}）</el-descriptions-item>
      <el-descriptions-item label="注册时间">
        {{ new Date(profile.created_at).toLocaleString() }}
      </el-descriptions-item>
      <el-descriptions-item label="邮箱">
        <router-link to="/mailbox">站内邮箱</router-link>
      </el-descriptions-item>
    </el-descriptions>

    <div class="bio-block">
      <div class="bio-head">
        <span>描述简介</span>
        <el-button v-if="!editing" size="small" @click="editing = true">编辑</el-button>
        <template v-else>
          <el-button size="small" :loading="bioLoading" @click="saveBio">保存</el-button>
          <el-button
            size="small"
            @click="
              () => {
                editing = false
                bio = profile?.bio ?? ''
              }
            "
          >取消</el-button>
        </template>
      </div>
      <el-input
        v-if="editing"
        v-model="bio"
        type="textarea"
        :rows="4"
        maxlength="500"
        show-word-limit
        placeholder="介绍一下自己（≤500 字）"
      />
      <p v-else class="bio-text">{{ profile.bio || '暂无简介' }}</p>
    </div>

    <div class="bio-block">
      <div class="bio-head">
        <span>账号安全</span>
        <el-button size="small" @click="pwdVisible = true">修改密码</el-button>
      </div>
      <p class="bio-text">修改后需重新登录（全部已登录会话失效）。</p>
    </div>
  </div>

  <el-dialog v-model="pwdVisible" title="修改密码" width="420px">
    <el-form label-width="90px">
      <el-form-item label="当前密码">
        <el-input v-model="pwdForm.current" type="password" show-password />
      </el-form-item>
      <el-form-item label="新密码">
        <el-input v-model="pwdForm.next" type="password" show-password placeholder="至少 6 位" />
      </el-form-item>
      <el-form-item label="确认新密码">
        <el-input v-model="pwdForm.confirm" type="password" show-password />
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="pwdVisible = false">取消</el-button>
      <el-button type="primary" :loading="pwdLoading" @click="submitChangePassword">确认修改</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.hint { color: var(--el-text-color-secondary); font-size: 0.9rem; margin-top: 0; }
.panel { max-width: 760px; display: flex; flex-direction: column; gap: 1.2rem; }
.avatar-wrap { display: flex; align-items: center; gap: 1rem; }
.avatar { font-size: 2rem; }
.upload {
  position: relative;
  overflow: hidden;
  padding: 4px 12px;
  border: 1px solid var(--el-border-color);
  border-radius: 4px;
  cursor: pointer;
  font-size: 0.85rem;
  color: var(--el-text-color-regular);
}
.upload.disabled { opacity: 0.5; cursor: wait; }
.upload input { position: absolute; inset: 0; opacity: 0; cursor: pointer; }
.desc { width: 100%; }
.bio-block { display: flex; flex-direction: column; gap: 0.5rem; }
.bio-head { display: flex; align-items: center; justify-content: space-between; font-weight: 600; }
.bio-text { color: var(--el-text-color-regular); white-space: pre-wrap; margin: 0; }
</style>
<script setup lang="ts">
// 管理端：用户与两档角色（普通人 student / 管理员 admin）。
import { onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { adminApi, type User } from '../../api'

const users = ref<User[]>([])
const total = ref(0)
const page = ref(1)
const loading = ref(false)
const createVisible = ref(false)
const createForm = reactive({ username: '', password: '', global_role: 'student' })

const roleLabel: Record<string, string> = {
  student: '普通人',
  admin: '管理员',
}

async function load() {
  loading.value = true
  try {
    const resp = await adminApi.listUsers(page.value)
    users.value = resp.items
    total.value = resp.total
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    loading.value = false
  }
}

async function createUser() {
  try {
    await adminApi.createUser(createForm.username, createForm.password, createForm.global_role)
    ElMessage.success('创建成功')
    createVisible.value = false
    createForm.username = ''
    createForm.password = ''
    createForm.global_role = 'student'
    await load()
  } catch (e) {
    ElMessage.error(`创建失败：${(e as Error).message}`)
  }
}

async function changeGlobalRole(user: User, role: string) {
  try {
    await adminApi.updateRoles(user.id, role)
    user.global_role = role as User['global_role']
    ElMessage.success(`已将 ${user.username} 设为${roleLabel[role] ?? role}`)
  } catch (e) {
    ElMessage.error((e as Error).message)
    await load()
  }
}

onMounted(load)
</script>

<template>
  <h2>用户与角色</h2>
  <p class="hint">仅两档：普通人（学习端）与管理员（管理端全部能力）。普通人不会看到管理入口。</p>
  <div class="toolbar">
    <el-button type="primary" @click="createVisible = true">新建用户</el-button>
  </div>
  <el-table :data="users" v-loading="loading">
    <el-table-column prop="id" label="ID" width="60" />
    <el-table-column prop="username" label="用户名" />
    <el-table-column label="角色" width="200">
      <template #default="{ row }">
        <el-select :model-value="row.global_role" @change="(v: string) => changeGlobalRole(row, v)">
          <el-option value="student" label="普通人" />
          <el-option value="admin" label="管理员" />
        </el-select>
      </template>
    </el-table-column>
  </el-table>
  <el-pagination
    v-if="total > 20"
    layout="prev, pager, next"
    :total="total"
    :page-size="20"
    v-model:current-page="page"
    @current-change="load"
  />

  <el-dialog v-model="createVisible" title="新建用户" width="420px">
    <el-form label-width="80px">
      <el-form-item label="用户名">
        <el-input v-model="createForm.username" />
      </el-form-item>
      <el-form-item label="密码">
        <el-input v-model="createForm.password" type="password" show-password />
      </el-form-item>
      <el-form-item label="角色">
        <el-select v-model="createForm.global_role">
          <el-option value="student" label="普通人" />
          <el-option value="admin" label="管理员" />
        </el-select>
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="createVisible = false">取消</el-button>
      <el-button type="primary" @click="createUser">创建</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.hint { color: var(--el-text-color-secondary); font-size: 0.9rem; margin-top: 0; }
.toolbar { margin-bottom: 1rem; }
</style>

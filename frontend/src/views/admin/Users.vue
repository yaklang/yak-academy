<script setup lang="ts">
// 管理端：用户与全局角色管理 + 课程级角色（双层 RBAC）。
import { onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { adminApi, type CourseRole, type User } from '../../api'

const users = ref<User[]>([])
const total = ref(0)
const page = ref(1)
const loading = ref(false)
const createVisible = ref(false)
const createForm = reactive({ username: '', password: '', global_role: 'student' })
const roleVisible = ref(false)
const editing = ref<User | null>(null)
const courseRoles = ref<CourseRole[]>([])

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
    await load()
  } catch (e) {
    ElMessage.error(`创建失败：${(e as Error).message}`)
  }
}

async function changeGlobalRole(user: User, role: string) {
  try {
    await adminApi.updateRoles(user.id, role, [])
    ElMessage.success(`已将 ${user.username} 全局角色改为 ${role}`)
  } catch (e) {
    ElMessage.error((e as Error).message)
  }
}

function openRoles(user: User) {
  editing.value = user
  courseRoles.value = []
  roleVisible.value = true
}

async function saveRoles() {
  if (!editing.value) return
  try {
    // 增量 upsert：列表里的行全部提交；被删掉的行单独调删除接口。
    if (courseRoles.value.length > 0) {
      await adminApi.updateRoles(editing.value.id, '', courseRoles.value)
    }
    // 对比初次打开时删除的行——简化：删除操作在行内按钮即时执行（见 removeRow）。
    ElMessage.success('课程级角色已保存')
    roleVisible.value = false
  } catch (e) {
    ElMessage.error((e as Error).message)
  }
}

async function removeRow(index: number) {
  if (!editing.value) return
  const row = courseRoles.value[index]
  try {
    await adminApi.deleteCourseRole(editing.value.id, row.topic_slug)
    courseRoles.value.splice(index, 1)
  } catch (e) {
    ElMessage.error((e as Error).message)
  }
}

onMounted(load)
</script>

<template>
  <h2>用户与角色</h2>
  <div class="toolbar">
    <el-button type="primary" @click="createVisible = true">新建用户</el-button>
  </div>
  <el-table :data="users" v-loading="loading">
    <el-table-column prop="id" label="ID" width="60" />
    <el-table-column prop="username" label="用户名" />
    <el-table-column label="全局角色" width="180">
      <template #default="{ row }">
        <el-select :model-value="row.global_role" @change="(v: string) => changeGlobalRole(row, v)">
          <el-option value="student" label="student" />
          <el-option value="teacher" label="teacher" />
          <el-option value="admin" label="admin" />
        </el-select>
      </template>
    </el-table-column>
    <el-table-column label="操作" width="140">
      <template #default="{ row }">
        <el-button size="small" @click="openRoles(row)">课程级角色</el-button>
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
          <el-option value="student" label="student" />
          <el-option value="teacher" label="teacher" />
          <el-option value="admin" label="admin" />
        </el-select>
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="createVisible = false">取消</el-button>
      <el-button type="primary" @click="createUser">创建</el-button>
    </template>
  </el-dialog>

  <el-dialog v-model="roleVisible" :title="`课程级角色：${editing?.username ?? ''}`" width="560px">
    <div v-for="(row, i) in courseRoles" :key="i" class="role-row">
      <el-input v-model="row.topic_slug" placeholder="topic_slug" />
      <el-select v-model="row.role">
        <el-option value="teacher" label="teacher" />
        <el-option value="student" label="student" />
      </el-select>
      <el-button type="danger" link @click="removeRow(i)">删除</el-button>
    </div>
    <el-button
      link
      type="primary"
      @click="courseRoles.push({ topic_slug: '', role: 'teacher' })"
    >+ 添加课程级角色</el-button>
    <template #footer>
      <el-button @click="roleVisible = false">关闭</el-button>
      <el-button type="primary" @click="saveRoles">保存</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.toolbar { margin-bottom: 1rem; }
.role-row { display: flex; gap: 0.5rem; margin-bottom: 0.5rem; align-items: center; }
</style>
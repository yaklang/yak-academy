<script setup lang="ts">
// 管理端：学习路径管理。Path 只引用 room_slug 与顺序，禁止内联 Room 内容
// （teaching-integration skill）；创建时后端校验 room_slug 存在。
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { pathApi, adminPathApi, type LearningPath } from '../../api'

const paths = ref<LearningPath[]>([])
const loading = ref(false)
const createVisible = ref(false)
const createForm = reactive<{ title: string; items: { room_slug: string }[] }>({
  title: '',
  items: [{ room_slug: '' }],
})

async function load() {
  loading.value = true
  try {
    paths.value = await pathApi.list()
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    loading.value = false
  }
}

function addItem() {
  createForm.items.push({ room_slug: '' })
}

function removeItem(i: number) {
  createForm.items.splice(i, 1)
}

async function createPath() {
  // 顺序按编辑行序生成（order 在 path 内唯一）。
  const items = createForm.items
    .filter((it) => it.room_slug.trim())
    .map((it, idx) => ({ order: idx + 1, room_slug: it.room_slug.trim() }))
  if (!createForm.title.trim() || !items.length) {
    ElMessage.warning('标题与至少一个 room_slug 必填')
    return
  }
  try {
    await adminPathApi.create(createForm.title.trim(), items)
    ElMessage.success('创建成功')
    createVisible.value = false
    createForm.title = ''
    createForm.items = [{ room_slug: '' }]
    await load()
  } catch (e) {
    ElMessage.error(`创建失败：${(e as Error).message}`)
  }
}

async function removePath(p: LearningPath) {
  try {
    await ElMessageBox.confirm(`删除路径「${p.title}」？`, '删除确认', { type: 'warning' })
  } catch {
    return
  }
  try {
    await adminPathApi.remove(p.id)
    ElMessage.success('已删除')
    await load()
  } catch (e) {
    ElMessage.error((e as Error).message)
  }
}

onMounted(load)
</script>

<template>
  <h2>路径管理</h2>
  <p class="hint">路径是教程 Room 的有序引用（只存 room_slug 与顺序，不复制内容）；删除路径不影响 Room 本体。</p>
  <div class="toolbar">
    <el-button type="primary" @click="createVisible = true">新建路径</el-button>
  </div>
  <el-table :data="paths" v-loading="loading">
    <el-table-column prop="id" label="ID" width="60" />
    <el-table-column prop="title" label="标题" />
    <el-table-column label="节点" width="120">
      <template #default="{ row }">{{ row.items.length }}</template>
    </el-table-column>
    <el-table-column label="操作" width="100">
      <template #default="{ row }">
        <el-button link type="danger" @click="removePath(row)">删除</el-button>
      </template>
    </el-table-column>
  </el-table>

  <el-dialog v-model="createVisible" title="新建路径" width="560px">
    <el-form label-width="90px">
      <el-form-item label="标题">
        <el-input v-model="createForm.title" placeholder="如：Web 安全入门" />
      </el-form-item>
      <el-form-item label="Room 列表">
        <div class="items-editor">
          <div v-for="(it, i) in createForm.items" :key="i" class="item-row">
            <span class="order">#{{ i + 1 }}</span>
            <el-input v-model="it.room_slug" placeholder="room slug（教程包内 Room 的标识）" />
            <el-button
              link
              type="danger"
              :disabled="createForm.items.length <= 1"
              @click="removeItem(i)"
            >移除</el-button>
          </div>
          <el-button link type="primary" @click="addItem">+ 添加节点</el-button>
        </div>
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="createVisible = false">取消</el-button>
      <el-button type="primary" @click="createPath">创建</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.hint { color: var(--el-text-color-secondary); font-size: 0.9rem; margin-top: 0; }
.toolbar { margin-bottom: 1rem; }
.items-editor { width: 100%; }
.item-row { display: flex; align-items: center; gap: 0.5rem; margin-bottom: 0.5rem; }
.order { color: var(--el-text-color-secondary); font-size: 0.85rem; width: 2rem; flex-shrink: 0; }
</style>

<script setup lang="ts">
// 代码编辑器（CodeMirror 6）：ACM 编程题源码提交与自测运行共用。
// yak 语法高度近似 Go（:= / func / for range / 注释与字符串规则），
// 用 Go 高亮做近似着色；后续如需精确 yak 文法可替换为自定义 StreamLanguage。
import { EditorView, basicSetup } from 'codemirror'
import { Compartment, EditorState } from '@codemirror/state'
import { go } from '@codemirror/lang-go'
import { onMounted, onUnmounted, ref, watch } from 'vue'

const props = withDefaults(
  defineProps<{ modelValue: string; height?: string; disabled?: boolean }>(),
  { height: '320px', disabled: false },
)
const emit = defineEmits<{ (e: 'update:modelValue', v: string): void }>()

const host = ref<HTMLDivElement>()
let view: EditorView | null = null
const editableComp = new Compartment()

onMounted(() => {
  view = new EditorView({
    state: EditorState.create({
      doc: props.modelValue,
      extensions: [
        basicSetup,
        go(),
        editableComp.of(EditorView.editable.of(!props.disabled)),
        EditorView.updateListener.of((u) => {
          if (u.docChanged) emit('update:modelValue', u.state.doc.toString())
        }),
        EditorView.theme({
          '&': { height: props.height },
          '.cm-scroller': { fontFamily: 'Consolas, Menlo, monospace', fontSize: '13px' },
        }),
      ],
    }),
    parent: host.value!,
  })
})

// 外部整体赋值（如重置）时同步进编辑器，避免与输入回环。
watch(
  () => props.modelValue,
  (v) => {
    if (view && v !== view.state.doc.toString()) {
      view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: v } })
    }
  },
)
watch(
  () => props.disabled,
  (v) => view?.dispatch({ effects: editableComp.reconfigure(EditorView.editable.of(!v)) }),
)
onUnmounted(() => view?.destroy())
</script>

<template>
  <div ref="host" class="code-editor" :class="{ 'is-disabled': disabled }" />
</template>

<style scoped>
.code-editor {
  border: 1px solid var(--el-border-color);
  border-radius: 4px;
  overflow: hidden;
}
.code-editor.is-disabled { opacity: 0.6; }
.code-editor :deep(.cm-editor) { background: var(--el-fill-color-blank); }
.code-editor :deep(.cm-editor.cm-focused) { outline: none; }
.code-editor :deep(.cm-gutters) {
  background: var(--el-fill-color-light);
  border-right: 1px solid var(--el-border-color-lighter);
  color: var(--el-text-color-secondary);
}
</style>

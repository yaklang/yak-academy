<script setup lang="ts">
// 代码编辑器（CodeMirror 6）：ACM 编程题源码提交与自测运行共用。
// yak 语法高亮为 yakit Monarch 词法的移植（yakLanguage.ts），
// 配套暗色主题与白色代码字（yakTheme.ts）。
// libPolicy 注入题目级库策略（白名单/黑名单），补全选项据此过滤——
// 与服务端受理前拦截（yak_libpolicy.go）同源。
import { basicSetup } from 'codemirror'
import { EditorView, placeholder } from '@codemirror/view'
import { Compartment, EditorState } from '@codemirror/state'
import { onMounted, onUnmounted, ref, watch } from 'vue'
import { yak } from './yakLanguage'
import { yakEditorTheme } from './yakTheme'
import { yakLibPolicy, defaultYakLibPolicy, type YakLibPolicy } from './yakCompletion'

const props = withDefaults(
  defineProps<{
    modelValue: string
    height?: string
    disabled?: boolean
    /** 题目级 yak 库策略（allowed=null 未配置；blocked 平台黑名单）。 */
    libPolicy?: YakLibPolicy | null
  }>(),
  { height: '320px', disabled: false, libPolicy: null },
)
const emit = defineEmits<{ (e: 'update:modelValue', v: string): void }>()

const host = ref<HTMLDivElement>()
let view: EditorView | null = null
const editableComp = new Compartment()
const policyComp = new Compartment()

const policyValue = () => props.libPolicy ?? defaultYakLibPolicy

onMounted(() => {
  view = new EditorView({
    state: EditorState.create({
      doc: props.modelValue,
      extensions: [
        basicSetup,
        yak(),
        yakEditorTheme,
        placeholder('在此编写 yak 源码，可先对样例自测运行…'),
        policyComp.of(yakLibPolicy.of(policyValue())),
        editableComp.of(EditorView.editable.of(!props.disabled)),
        EditorView.updateListener.of((u) => {
          if (u.docChanged) emit('update:modelValue', u.state.doc.toString())
        }),
        EditorView.theme({
          '&': { height: props.height },
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
// 题目策略变化（如详情加载完成）时热替换补全过滤。
watch(
  () => props.libPolicy,
  (v) => view?.dispatch({ effects: policyComp.reconfigure(yakLibPolicy.of(v ?? defaultYakLibPolicy)) }),
  { deep: true },
)
onUnmounted(() => view?.destroy())
</script>

<template>
  <div ref="host" class="code-editor" :class="{ 'is-disabled': disabled }" />
</template>

<style scoped>
.code-editor {
  border: 1px solid #383e46;
  border-radius: 8px;
  overflow: hidden;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.28);
  transition: border-color 0.2s, box-shadow 0.2s;
}
.code-editor:focus-within {
  border-color: #4a7db3;
  box-shadow: 0 0 0 3px rgba(64, 158, 255, 0.18);
}
.code-editor.is-disabled {
  opacity: 0.55;
  filter: saturate(0.75);
  cursor: not-allowed;
}
</style>

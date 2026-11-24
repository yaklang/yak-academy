<script setup lang="ts">
// 表单字段：标签、帮助文字、错误信息通过 aria-describedby / aria-invalid
// 绑定到控件；错误同时以 role="alert" 暴露，不依赖 toast。
import { computed, useId } from 'vue'

const props = defineProps<{
  label: string
  hint?: string
  error?: string
  required?: boolean
  htmlFor?: string
}>()

const uid = useId()
const id = computed(() => props.htmlFor || `field-${uid}`)
const hintId = computed(() => `${id.value}-hint`)
const errorId = computed(() => `${id.value}-error`)
const describedBy = computed(() =>
  [props.hint ? hintId.value : '', props.error ? errorId.value : ''].filter(Boolean).join(' ') || undefined,
)
</script>

<template>
  <div class="form-field" :data-invalid="error ? 'true' : undefined">
    <label class="form-field__label" :for="id">
      {{ label }}
      <span v-if="required" class="form-field__required" aria-hidden="true">*</span>
    </label>
    <div class="form-field__control">
      <slot :id="id" :describedby="describedBy" :invalid="!!error" />
    </div>
    <p v-if="hint" :id="hintId" class="form-field__hint">
      {{ hint }}
    </p>
    <p v-if="error" :id="errorId" class="form-field__error" role="alert">
      {{ error }}
    </p>
  </div>
</template>

<style scoped>
.form-field {
  display: grid;
  gap: var(--space-2);
  min-width: 0;
}

.form-field__label {
  color: var(--text-primary);
  font-size: var(--text-caption);
  font-weight: 500;
  line-height: var(--leading-normal);
}

.form-field__required {
  margin-left: 2px;
  color: var(--status-danger);
}

.form-field__control {
  min-width: 0;
}

.form-field__control :deep([data-slot='input']),
.form-field__control :deep(textarea) {
  width: 100%;
  min-height: 44px;
}

.form-field__hint {
  color: var(--text-tertiary);
  font-size: var(--text-meta);
  line-height: var(--leading-normal);
}

.form-field__error {
  color: var(--status-danger);
  font-size: var(--text-meta);
  line-height: var(--leading-normal);
}
</style>

<script setup lang="ts">
// 设置页/长表单的分区：标题 + 说明 + 字段组。区块标题固定 16/600，
// 页面标题只允许出现一次（由 PageHeader 提供）。
defineProps<{
  title: string
  description?: string
}>()
</script>

<template>
  <section class="form-section">
    <header class="form-section__head">
      <h2 class="form-section__title">{{ title }}</h2>
      <p v-if="description" class="form-section__description">{{ description }}</p>
    </header>
    <div class="form-section__body">
      <slot />
    </div>
    <footer v-if="$slots.footer" class="form-section__footer">
      <slot name="footer" />
    </footer>
  </section>
</template>

<style scoped>
.form-section {
  padding: var(--space-5);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  background: var(--bg-surface);
}

.form-section + .form-section {
  margin-top: var(--space-4);
}

.form-section__head {
  margin-bottom: var(--space-4);
}

.form-section__title {
  color: var(--text-primary);
  font-size: var(--text-section);
  font-weight: 600;
  line-height: var(--leading-normal);
}

.form-section__description {
  max-width: 72ch;
  margin-top: var(--space-1);
  color: var(--text-secondary);
  font-size: var(--text-caption);
  line-height: var(--leading-relaxed);
}

.form-section__body {
  display: grid;
  gap: var(--space-4);
}

.form-section__footer {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: var(--space-2);
  margin-top: var(--space-5);
  padding-top: var(--space-4);
  border-top: 1px solid var(--border);
  flex-wrap: wrap;
}
</style>

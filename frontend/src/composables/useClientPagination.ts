import { computed, ref, toValue, watch, type MaybeRefOrGetter } from 'vue'

// 管理端表格的客户端分页：适用于一次性返回的集合（活跃容器、主机容器快照）。
// 数据刷新（如性能面板 10s 自动刷新）时保留当前页；总页数变小则收敛到最后一页，
// 切换每页条数回到第 1 页。
export function useClientPagination<T>(source: MaybeRefOrGetter<readonly T[]>, initialPageSize = 20) {
  const page = ref(1)
  const pageSize = ref(initialPageSize)
  const total = computed(() => toValue(source).length)
  const pageCount = computed(() => Math.max(1, Math.ceil(total.value / pageSize.value)))
  const items = computed(() => {
    const start = (page.value - 1) * pageSize.value
    return toValue(source).slice(start, start + pageSize.value)
  })

  watch([total, pageSize], () => {
    if (page.value > pageCount.value) page.value = pageCount.value
  })
  watch(pageSize, () => { page.value = 1 })

  return { page, pageSize, total, pageCount, items }
}

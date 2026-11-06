import { nextTick, ref } from 'vue'
import { describe, expect, it } from 'vitest'
import { useClientPagination } from './useClientPagination'

function makeRows(count: number) {
  return Array.from({ length: count }, (_, index) => index + 1)
}

describe('useClientPagination', () => {
  it('默认每页 20 条，按页切片', async () => {
    const source = ref(makeRows(45))
    const { page, pageSize, total, pageCount, items } = useClientPagination(source)

    expect(pageSize.value).toBe(20)
    expect(total.value).toBe(45)
    expect(pageCount.value).toBe(3)
    expect(items.value).toEqual(makeRows(20))

    page.value = 2
    await nextTick()
    expect(items.value).toEqual(Array.from({ length: 20 }, (_, index) => index + 21))

    page.value = 3
    await nextTick()
    expect(items.value).toEqual([41, 42, 43, 44, 45])
  })

  it('切换每页条数后回到第 1 页', async () => {
    const source = ref(makeRows(45))
    const { page, pageSize, items } = useClientPagination(source)
    page.value = 3
    await nextTick()

    pageSize.value = 50
    await nextTick()
    expect(page.value).toBe(1)
    expect(items.value).toHaveLength(45)
  })

  it('数据缩小时收敛到最后有效页', async () => {
    const source = ref(makeRows(45))
    const { page, items } = useClientPagination(source)
    page.value = 3
    await nextTick()
    expect(items.value).toHaveLength(5)

    source.value = makeRows(3)
    await nextTick()
    expect(page.value).toBe(1)
    expect(items.value).toEqual([1, 2, 3])
  })
})

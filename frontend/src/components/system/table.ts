import { computed, defineComponent, h, ref, type PropType, type SlotsType, type VNode } from 'vue'
import { Inbox } from '@lucide/vue'
import { Checkbox } from '@/components/ui/checkbox'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { cn } from '@/lib/utils'
import { childComponents } from './context'

export interface TableSelectionHandle {
  toggleRowSelection(row: any, selected?: boolean): void
  clearSelection(): void
}
type ColumnSlot = (scope: { row: any; $index: number; column: Record<string, any> }) => any

export const UiTableColumn = defineComponent({
  name: 'UiTableColumn',
  props: { prop: String, label: String, type: String, width: [Number, String], minWidth: [Number, String], fixed: [String, Boolean], selectable: Function, showOverflowTooltip: Boolean, align: String },
  slots: Object as SlotsType<{ default: ColumnSlot; header: ColumnSlot }>,
  setup() { return () => null },
})

const pixels = (value: unknown) => typeof value === 'number' || /^\d+$/.test(String(value)) ? `${value}px` : value ? String(value) : undefined
function slotOf(column: VNode, name: string): ColumnSlot | undefined { return (column.children as Record<string, any> | null)?.[name] }
function cellValue(row: any, path: unknown) { return String(path || '').split('.').reduce((value, key) => value?.[key], row) ?? '' }
// VNode props 保留模板里的 kebab-case 与裸布尔原值（空字符串）：
// 读取多词列属性时按需回退，避免 min-width / show-overflow-tooltip 静默失效。
function columnSetting(settings: Record<string, any>, name: string) {
  const value = settings[name]
  if (value !== undefined) return value
  return settings[name.replace(/[A-Z]/g, (letter) => `-${letter.toLowerCase()}`)]
}
function isTruncated(settings: Record<string, any>) {
  const value = columnSetting(settings, 'showOverflowTooltip')
  return value !== undefined && value !== false && value !== null
}

export const UiTable = defineComponent({
  name: 'UiTable', inheritAttrs: false,
  props: { data: { type: Array as PropType<any[]>, default: () => [] }, rowKey: [String, Function], rowClassName: [String, Function], emptyText: String, maxHeight: [Number, String], border: Boolean, stripe: Boolean, size: String, height: [Number, String], loading: Boolean, loadingRows: { type: Number, default: 5 } },
  emits: ['selection-change', 'row-click'],
  setup(props, { attrs, slots, emit, expose }) {
    const selected = ref(new Set<unknown>())
    const keyFor = (row: any) => typeof props.rowKey === 'function' ? props.rowKey(row) : props.rowKey ? row[props.rowKey] : row
    function changeSelection(row: any, value?: boolean) {
      const next = new Set(selected.value)
      const key = keyFor(row)
      const checked = value ?? !next.has(key)
      if (checked) next.add(key); else next.delete(key)
      selected.value = next
      emit('selection-change', props.data.filter(item => next.has(keyFor(item))))
    }
    expose({ toggleRowSelection: changeSelection, clearSelection: () => { selected.value = new Set(); emit('selection-change', []) } } satisfies TableSelectionHandle)

    return () => {
      const columns = childComponents(slots.default?.(), 'UiTableColumn')
      const canSelect = (column: VNode, row: any, index: number) => column.props?.selectable ? column.props.selectable(row, index) !== false : true
      const columnStyle = (column: VNode) => {
        const settings = column.props ?? {}
        const minWidth = columnSetting(settings, 'minWidth') ?? settings.width
        return { width: pixels(settings.width), minWidth: pixels(minWidth), textAlign: settings.align }
      }
      const cellStyle = (column: VNode) => {
        const settings = column.props ?? {}
        const truncate = isTruncated(settings) && settings.type == null
        return { ...columnStyle(column), maxWidth: truncate ? pixels(settings.width ?? columnSetting(settings, 'minWidth')) : undefined }
      }
      const fixedClass = (column: VNode) => column.props?.fixed === 'right'
        ? 'sticky right-0 z-10 bg-card ui-fixed-right'
        : column.props?.fixed === 'left'
          ? 'sticky left-0 z-10 bg-card ui-fixed-left'
          : ''
      // 首次加载用与真实列数一致的骨架行，避免先闪一下“暂无数据”再跳成表格；
      // 已有数据时保留旧行，只在右上角显示刷新状态，避免筛选/翻页时整表塌陷。
      const showSkeleton = props.loading && props.data.length === 0
      const refreshing = props.loading && props.data.length > 0
      const skeletonRows = Array.from({ length: Math.max(1, props.loadingRows) }, () => h(TableRow, { class: 'ui-data-row', 'aria-hidden': 'true' }, {
        default: () => columns.map((column, index) => h(TableCell, { class: 'px-4 py-3' }, { default: () => h('span', { class: cn('table-skeleton-bar', index === 0 && 'is-narrow') }) })),
      }))
      return h('div', { ...attrs, 'aria-busy': props.loading ? 'true' : undefined, class: cn('ui-data-table relative min-w-0 max-w-full rounded-lg border border-border bg-card', attrs.class as string), style: [{ maxHeight: pixels(props.maxHeight || props.height), overflow: props.maxHeight || props.height ? 'auto' : undefined }, attrs.style as any] }, [
        props.loading ? h('span', { class: 'sr-only', role: 'status' }, props.data.length ? '正在刷新数据' : '正在加载数据') : null,
        refreshing ? h('div', { class: 'table-loading-note', role: 'status' }, '正在刷新…') : null,
        h(Table as any, { 'aria-label': attrs['aria-label'] || '数据列表', class: 'text-sm' }, {
          default: () => [
            h(TableHeader, {}, { default: () => h(TableRow, { class: 'hover:bg-transparent' }, { default: () => columns.map(column => {
              const settings = column.props ?? {}
              const selectableRows = settings.type === 'selection' ? props.data.filter((row, index) => canSelect(column, row, index)) : []
              const checkedRows = selectableRows.filter(row => selected.value.has(keyFor(row)))
              return h(TableHead, { scope: 'col', class: cn('h-11 whitespace-nowrap bg-muted/50 px-4 font-medium text-muted-foreground', fixedClass(column)), style: columnStyle(column) }, {
                default: () => settings.type === 'selection' ? h(Checkbox as any, {
                  'aria-label': '选择本页全部题目', disabled: selectableRows.length === 0,
                  modelValue: checkedRows.length === 0 ? false : checkedRows.length === selectableRows.length ? true : 'indeterminate',
                  'onUpdate:modelValue': (value: boolean | string) => {
                    const next = new Set(selected.value)
                    for (const row of selectableRows) { if (value === true) next.add(keyFor(row)); else next.delete(keyFor(row)) }
                    selected.value = next
                    emit('selection-change', props.data.filter(row => next.has(keyFor(row))))
                  },
                }) : slotOf(column, 'header')?.({ row: null, $index: -1, column: settings }) ?? settings.label,
              })
            }) }) }),
            h(TableBody, {}, { default: () => showSkeleton ? skeletonRows : props.data.length ? props.data.map((row, index) => h(TableRow, {
              key: keyFor(row) as any, 'data-state': selected.value.has(keyFor(row)) ? 'selected' : undefined,
              class: cn('ui-data-row', typeof props.rowClassName === 'function' ? props.rowClassName({ row, rowIndex: index }) : props.rowClassName),
              onClick: () => emit('row-click', row),
            }, { default: () => columns.map(column => {
              const settings = column.props ?? {}
              const truncate = isTruncated(settings) && settings.type == null
              const title = truncate ? cellValue(row, settings.prop) : ''
              return h(TableCell, { class: cn('px-4 py-3 align-middle', truncate ? 'table-cell-truncate' : 'whitespace-normal break-words', fixedClass(column)), style: cellStyle(column), title: title || undefined }, {
                default: () => settings.type === 'selection' ? h(Checkbox as any, {
                  'aria-label': `选择 ${row.title || row.username || row.slug || index + 1}`,
                  modelValue: selected.value.has(keyFor(row)), disabled: !canSelect(column, row, index),
                  'onUpdate:modelValue': (value: boolean | string) => changeSelection(row, value === true),
                }) : settings.type === 'index' ? index + 1 : slotOf(column, 'default')?.({ row, $index: index, column: settings }) ?? cellValue(row, settings.prop),
              })
            }) })) : h(TableRow, {}, { default: () => h(TableCell, { colspan: Math.max(1, columns.length), class: 'h-36 text-center' }, { default: () => h('div', { class: 'table-empty', 'data-slot': 'table-empty' }, [h(Inbox, { class: 'size-6 text-muted-foreground/60', 'aria-hidden': true }), h('p', {}, props.emptyText || '暂无数据')]) }) }) }),
          ],
        }),
      ])
    }
  },
})

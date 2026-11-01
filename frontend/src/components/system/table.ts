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

export const UiTable = defineComponent({
  name: 'UiTable', inheritAttrs: false,
  props: { data: { type: Array as PropType<any[]>, default: () => [] }, rowKey: [String, Function], rowClassName: [String, Function], emptyText: String, maxHeight: [Number, String], border: Boolean, stripe: Boolean, size: String, height: [Number, String] },
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
      const columnStyle = (column: VNode) => ({ width: pixels(column.props?.width), minWidth: pixels(column.props?.minWidth ?? column.props?.width), textAlign: column.props?.align })
      const fixedClass = (column: VNode) => column.props?.fixed === 'right' ? 'sticky right-0 z-10 bg-card' : ''
      return h('div', { ...attrs, class: cn('ui-data-table min-w-0 max-w-full rounded-lg border border-border bg-card', attrs.class as string), style: [{ maxHeight: pixels(props.maxHeight || props.height), overflow: props.maxHeight || props.height ? 'auto' : undefined }, attrs.style as any] }, [
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
            h(TableBody, {}, { default: () => props.data.length ? props.data.map((row, index) => h(TableRow, {
              key: keyFor(row) as any, 'data-state': selected.value.has(keyFor(row)) ? 'selected' : undefined,
              class: cn('ui-data-row', typeof props.rowClassName === 'function' ? props.rowClassName({ row, rowIndex: index }) : props.rowClassName),
              onClick: () => emit('row-click', row),
            }, { default: () => columns.map(column => {
              const settings = column.props ?? {}
              return h(TableCell, { class: cn('px-4 py-3 whitespace-normal break-words align-middle', fixedClass(column)), style: columnStyle(column) }, {
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

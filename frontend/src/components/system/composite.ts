import { computed, defineComponent, h, inject, nextTick, onBeforeUnmount, onMounted, provide, ref, useId, type InjectionKey, type PropType, type SlotsType } from 'vue'
import { ChevronLeft, ChevronRight, Search, X } from '@lucide/vue'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { TagsInput, TagsInputInput, TagsInputItem, TagsInputItemDelete, TagsInputItemText } from '@/components/ui/tags-input'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { childComponents, fieldContext, withoutStyle } from './context'

export interface UploadFile { raw?: File; name: string; status?: string; uid?: number }

export const UiOption = defineComponent({ name: 'UiOption', props: { label: [String, Number], value: null, disabled: Boolean }, setup() { return () => null } })
const emptyValue = '__academy_empty_value__'
const selectKey = (value: unknown) => value === '' ? emptyValue : String(value)

export const UiSelect = defineComponent({
  name: 'UiSelect', inheritAttrs: false,
  props: { modelValue: null, placeholder: String, disabled: Boolean, clearable: Boolean, multiple: Boolean, filterable: Boolean, allowCreate: Boolean, reserveKeyword: Boolean, size: String },
  emits: ['update:modelValue', 'change', 'clear', 'visible-change'],
  setup(props, { attrs, slots, emit }) {
    const field = inject(fieldContext, null)
    const generatedId = `select-${useId()}`
    const query = ref('')
    const open = ref(false)
    const update = (value: unknown) => { emit('update:modelValue', value); emit('change', value) }
    return () => {
      const options = childComponents(slots.default?.(), 'UiOption').map(node => ({ value: node.props?.value, label: String(node.props?.label ?? node.props?.value ?? ''), disabled: !!node.props?.disabled }))
      const id = attrs.id || field?.id || generatedId
      const accessible = { id, 'aria-labelledby': field?.labelId, 'aria-label': attrs['aria-label'] || (!field ? props.placeholder || '选择选项' : undefined) }
      if (props.multiple) {
        return h(TagsInput as any, { ...attrs, ...accessible, modelValue: props.modelValue || [], disabled: props.disabled, class: cn('ui-multi-select min-w-0 max-w-full', attrs.class as string), 'onUpdate:modelValue': update }, {
          default: () => [
            ...(Array.isArray(props.modelValue) ? props.modelValue : []).map((value: string) => h(TagsInputItem, { key: value, value, class: 'max-w-full' }, {
              default: () => [h(TagsInputItemText, { class: 'max-w-full break-all whitespace-normal' }), h(TagsInputItemDelete, { 'aria-label': `移除 ${value}` })],
            })),
            h(TagsInputInput, { placeholder: props.placeholder || '输入后按 Enter 添加', class: 'min-w-16 flex-1', 'aria-label': props.placeholder || '添加选项', disabled: props.disabled }),
          ],
        })
      }
      const current = options.find(option => Object.is(option.value, props.modelValue))
      const filtered = options.filter(option => !query.value || option.label.toLocaleLowerCase().includes(query.value.toLocaleLowerCase()))
      return h('div', { class: cn('ui-select relative min-w-0 max-w-full', attrs.class as string), style: attrs.style as any }, [
        h(Select as any, {
          disabled: props.disabled, open: open.value, modelValue: props.modelValue === undefined || props.modelValue === null ? '' : selectKey(props.modelValue),
          'onUpdate:open': (value: boolean) => { open.value = value; query.value = ''; emit('visible-change', value) },
          'onUpdate:modelValue': (value: string) => { const option = options.find(option => selectKey(option.value) === value); update(option?.value ?? (value === emptyValue ? '' : value)) },
        }, { default: () => [
          h(SelectTrigger as any, { ...withoutStyle(attrs), ...accessible, class: cn('h-9 w-full min-w-0', props.clearable && current && 'pr-14') }, {
            default: () => h(SelectValue, { placeholder: props.placeholder || '请选择', class: 'min-w-0 flex-1 truncate text-left' }, current ? { default: () => current.label } : undefined),
          }),
          h(SelectContent as any, { position: 'popper', align: 'start', class: 'max-w-[calc(100vw-2rem)] p-1' }, {
            default: () => [
              props.filterable ? h('div', { class: 'sticky top-0 z-10 bg-popover p-1 pb-2' }, [h(Input as any, {
                modelValue: query.value, placeholder: '搜索选项…', 'aria-label': '搜索选项', class: 'h-8',
                'onUpdate:modelValue': (value: string | number) => { query.value = String(value) },
                onKeydown: (event: KeyboardEvent) => {
                  event.stopPropagation()
                  if (event.key === 'ArrowDown') { event.preventDefault(); (event.target as HTMLElement).closest('[data-slot="select-content"]')?.querySelector<HTMLElement>('[role="option"]')?.focus() }
                },
              })]) : null,
              ...filtered.map(option => h(SelectItem, { key: selectKey(option.value), value: selectKey(option.value), disabled: option.disabled, class: 'max-w-full whitespace-normal break-words' }, { default: () => option.label })),
              filtered.length ? null : h('p', { role: 'status', class: 'px-3 py-4 text-sm text-muted-foreground' }, '没有匹配的选项'),
            ],
          }),
        ] }),
        props.clearable && current && props.modelValue !== '' ? h('button', { type: 'button', class: 'select-clear', 'aria-label': '清除选择', disabled: props.disabled, onClick: () => { update(''); emit('clear') } }, h(X, { class: 'size-3.5' })) : null,
      ])
    }
  },
})

export const UiTabPane = defineComponent({
  name: 'UiTabPane', props: { name: [String, Number], label: String, disabled: Boolean },
  slots: Object as SlotsType<{ default: () => any; label: () => any }>, setup() { return () => null },
})
export const UiTabs = defineComponent({
  name: 'UiTabs', inheritAttrs: false,
  props: { modelValue: [String, Number], type: String }, emits: ['update:modelValue', 'tab-change', 'tab-click'],
  setup(props, { attrs, slots, emit }) {
    const scrollEl = ref<HTMLDivElement | null>(null)
    const scrollLeft = ref(false)
    const scrollRight = ref(false)
    let resizeObserver: ResizeObserver | null = null

    const syncScroll = () => {
      const el = scrollEl.value
      if (!el) return
      const left = el.scrollLeft > 1
      const right = el.scrollLeft + el.clientWidth < el.scrollWidth - 1
      if (scrollLeft.value !== left) scrollLeft.value = left
      if (scrollRight.value !== right) scrollRight.value = right
    }

    onMounted(() => {
      const el = scrollEl.value
      if (!el) return
      syncScroll()
      if (typeof ResizeObserver !== 'undefined') {
        resizeObserver = new ResizeObserver(syncScroll)
        resizeObserver.observe(el)
        if (el.firstElementChild) resizeObserver.observe(el.firstElementChild)
      }
    })
    onBeforeUnmount(() => resizeObserver?.disconnect())

    return () => {
      const panes = childComponents(slots.default?.(), 'UiTabPane')
      const summary = slots.summary?.()
      const value = String(props.modelValue ?? panes[0]?.props?.name ?? '')
      return h(Tabs as any, { ...attrs, modelValue: value, class: cn('ui-tabs min-w-0 w-full gap-0', attrs.class as string), 'onUpdate:modelValue': (next: string) => { emit('update:modelValue', next); emit('tab-change', next); emit('tab-click', { paneName: next }) } }, {
        default: () => [
          h('div', { ref: scrollEl, class: cn('tabs-scroll max-w-full overflow-x-auto', scrollLeft.value && 'is-scroll-left', scrollRight.value && 'is-scroll-right'), onScroll: syncScroll }, [h(TabsList, { variant: 'line', class: 'h-auto min-h-10 w-full justify-start gap-6 rounded-none bg-transparent p-0' }, {
            default: () => panes.map((pane, index) => h(TabsTrigger, { value: String(pane.props?.name ?? index), disabled: !!pane.props?.disabled, class: 'h-auto min-h-10 flex-none rounded-none px-1 pb-3 pt-1 text-sm' }, { default: () => (pane.children as any)?.label?.() ?? pane.props?.label })),
          })]),
          ...(summary ? [h('div', { class: 'tabs-summary min-w-0' }, summary)] : []),
          ...panes.map((pane, index) => h(TabsContent, { value: String(pane.props?.name ?? index), class: 'min-w-0 outline-none' }, { default: () => (pane.children as any)?.default?.() })),
        ],
      })
    }
  },
})

interface RadioContext { name: string; value: () => any; disabled: () => boolean; update(value: any): void }
const radioContext: InjectionKey<RadioContext> = Symbol('academy-radio-group')
export const UiRadioGroup = defineComponent({
  name: 'UiRadioGroup', inheritAttrs: false,
  props: { modelValue: null, size: String, disabled: Boolean }, emits: ['update:modelValue', 'change'],
  setup(props, { attrs, slots, emit }) {
    const field = inject(fieldContext, null)
    provide(radioContext, { name: `radio-${useId()}`, value: () => props.modelValue, disabled: () => props.disabled, update: value => { emit('update:modelValue', value); emit('change', value) } })
    return () => h('div', { ...attrs, role: 'radiogroup', 'aria-labelledby': field?.labelId, class: cn('ui-radio-group flex min-w-0 flex-wrap gap-2', attrs.class as string) }, slots.default?.())
  },
})
function radio(name: string, segmented: boolean) {
  return defineComponent({
    name, props: { value: null, label: null, disabled: Boolean },
    setup(props, { attrs, slots }) {
      const group = inject(radioContext, null)
      return () => {
        const value = props.value !== undefined ? props.value : props.label
        const checked = Object.is(group?.value(), value)
        return h('label', { class: cn('ui-radio relative inline-flex min-h-9 cursor-pointer items-center gap-2 rounded-md text-sm', segmented && 'border px-3 py-2', checked && segmented && 'border-primary bg-accent text-accent-foreground', props.disabled && 'cursor-not-allowed opacity-50', attrs.class as string) }, [
          h('input', { type: 'radio', name: group?.name, value: String(value), checked, disabled: props.disabled || group?.disabled(), class: 'absolute inset-0 z-10 h-full w-full cursor-inherit opacity-0', onChange: () => group?.update(value) }),
          segmented ? null : h('span', { class: cn('grid size-4 shrink-0 place-content-center rounded-full border border-input', checked && 'border-primary') }, checked ? [h('span', { class: 'size-2 rounded-full bg-primary' })] : []),
          h('span', { class: 'min-w-0 break-words' }, slots.default?.() ?? String(props.label ?? props.value ?? '')),
        ])
      }
    },
  })
}
export const UiRadio = radio('UiRadio', false)
export const UiRadioButton = radio('UiRadioButton', true)
export const UiCheckbox = defineComponent({
  name: 'UiCheckbox', props: { modelValue: Boolean, disabled: Boolean, label: String }, emits: ['update:modelValue', 'change'],
  setup(props, { attrs, slots, emit }) {
    const id = `checkbox-${useId()}`
    return () => h('label', { for: id, class: cn('inline-flex min-w-0 cursor-pointer items-center gap-2 text-sm', attrs.class as string) }, [
      h(Checkbox as any, { ...attrs, id, modelValue: props.modelValue, disabled: props.disabled, 'onUpdate:modelValue': (value: boolean) => { emit('update:modelValue', value); emit('change', value) } }),
      slots.default?.() ?? props.label,
    ])
  },
})

export const UiUpload = defineComponent({
  name: 'UiUpload', inheritAttrs: false,
  props: { accept: String, multiple: Boolean, disabled: Boolean, beforeUpload: Function, onChange: Function, autoUpload: Boolean, showFileList: Boolean, drag: Boolean, action: String, fileList: Array, limit: Number },
  setup(props, { attrs, slots }) {
    const input = ref<HTMLInputElement>()
    async function change(event: Event) {
      const target = event.target as HTMLInputElement
      const files = Array.from(target.files || [])
      for (const file of files) {
        if (props.beforeUpload) await props.beforeUpload(file)
        if (props.onChange) await props.onChange({ raw: file, name: file.name, status: 'ready', uid: file.lastModified })
      }
      target.value = ''
    }
    return () => h('div', { ...attrs, class: cn('ui-upload min-w-0 max-w-full', attrs.class as string), 'data-slot': 'upload' }, [
      h('input', { ref: input, type: 'file', accept: props.accept, multiple: props.multiple, disabled: props.disabled, class: 'hidden', onChange: change, 'aria-label': '选择上传文件' }),
      h('div', { class: cn('inline-flex max-w-full', props.drag && 'upload-dropzone'), onClick: () => { if (!props.disabled) input.value?.click() }, onDragover: (event: DragEvent) => { if (props.drag) event.preventDefault() }, onDrop: async (event: DragEvent) => { if (!props.drag || props.disabled) return; event.preventDefault(); for (const file of Array.from(event.dataTransfer?.files || [])) await props.beforeUpload?.(file) } }, slots.default?.()),
      slots.tip ? h('p', { class: 'mt-2 text-xs text-muted-foreground' }, slots.tip()) : null,
    ])
  },
})

export const UiPagination = defineComponent({
  name: 'UiPagination', inheritAttrs: false,
  props: { currentPage: { type: Number, default: 1 }, pageSize: { type: Number, default: 10 }, total: { type: Number, default: 0 }, pageSizes: { type: Array as PropType<number[]>, default: () => [10, 20, 50, 100] }, layout: String, small: Boolean, background: Boolean, showSinglePage: Boolean },
  emits: ['update:currentPage', 'update:pageSize', 'current-change', 'size-change'],
  setup(props, { attrs, emit }) {
    const pages = computed(() => Math.max(1, Math.ceil(props.total / props.pageSize)))
    const changePage = (value: number) => { const page = Math.min(pages.value, Math.max(1, value)); emit('update:currentPage', page); emit('current-change', page) }
    return () => {
      // 默认只有一页时不渲染分页器（无操作可做）；管理端关键表格用
      // showSinglePage 常驻总数与每页条数，避免“是否分页”随数据量跳变。
      if (props.total <= 0) return null
      if (!props.showSinglePage && props.total <= props.pageSize) return null
      return h('nav', { ...attrs, 'aria-label': '分页', class: cn('ui-pagination mt-5 flex min-w-0 flex-wrap items-center gap-2 text-sm', attrs.class as string) }, [
      h('span', { class: 'mr-auto text-xs text-muted-foreground' }, `共 ${props.total} 条`),
      props.layout?.includes('sizes') ? h('select', { class: 'page-size-control', 'aria-label': '每页条数', value: props.pageSize, onChange: (event: Event) => { const size = Number((event.target as HTMLSelectElement).value); emit('update:pageSize', size); emit('update:currentPage', 1); emit('size-change', size) } }, props.pageSizes.map(size => h('option', { value: size }, `${size} 条 / 页`))) : null,
      h(Button, { variant: 'outline', size: 'icon', 'aria-label': '上一页', disabled: props.currentPage <= 1, onClick: () => changePage(props.currentPage - 1) }, { default: () => h(ChevronLeft, { class: 'size-4' }) }),
      ...Array.from({ length: Math.min(5, pages.value) }, (_, index) => Math.min(Math.max(1, props.currentPage - 2), Math.max(1, pages.value - 4)) + index).map(page => h(Button, { variant: page === props.currentPage ? 'default' : 'ghost', size: 'icon', 'aria-current': page === props.currentPage ? 'page' : undefined, class: cn('page-number', page !== props.currentPage && 'page-neighbor'), onClick: () => changePage(page) }, { default: () => String(page) })),
      h(Button, { variant: 'outline', size: 'icon', 'aria-label': '下一页', disabled: props.currentPage >= pages.value, onClick: () => changePage(props.currentPage + 1) }, { default: () => h(ChevronRight, { class: 'size-4' }) }),
      ])
    }
  },
})

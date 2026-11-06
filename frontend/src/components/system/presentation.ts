import { defineComponent, h, inject, provide, ref, useId, type InjectionKey } from 'vue'
import { ArrowRight, CircleCheck, CircleHelp, CircleX, Info, Library, RefreshCw, TriangleAlert, X } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader } from '@/components/ui/card'
import { Progress } from '@/components/ui/progress'
import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'

export const UiCard = defineComponent({
  name: 'UiCard', inheritAttrs: false,
  props: { header: String, bodyStyle: [Object, String], shadow: String },
  setup(props, { attrs, slots }) {
    return () => h(Card, { ...attrs, class: cn('ui-card min-w-0 gap-0 overflow-hidden py-0 shadow-none', attrs.class as string) }, {
      default: () => [props.header || slots.header ? h(CardHeader, { class: 'border-b px-5 py-4' }, { default: () => slots.header?.() ?? props.header }) : null, h(CardContent, { class: 'min-w-0 p-5', style: props.bodyStyle as any }, { default: slots.default })],
    })
  },
})

export const UiAlert = defineComponent({
  name: 'UiAlert', inheritAttrs: false,
  props: { type: { type: String, default: 'info' }, title: String, description: String, showIcon: Boolean, closable: { type: Boolean, default: true }, center: Boolean, effect: String }, emits: ['close'],
  setup(props, { attrs, slots, emit }) {
    const closed = ref(false)
    return () => closed.value ? null : h('div', { ...attrs, role: props.type === 'error' ? 'alert' : 'status', 'data-slot': 'alert', 'data-tone': props.type, class: cn('ui-alert flex min-w-0 items-start gap-3 rounded-lg border p-4 text-sm', attrs.class as string) }, [
      props.showIcon ? h(props.type === 'error' ? CircleX : props.type === 'success' ? CircleCheck : props.type === 'warning' ? TriangleAlert : Info, { class: 'mt-0.5 size-4 shrink-0', 'aria-hidden': true }) : null,
      h('div', { class: 'min-w-0 flex-1 break-words leading-relaxed' }, [props.title || slots.title ? h('p', { class: 'font-medium' }, slots.title?.() ?? props.title) : null, props.description ? h('p', { class: 'mt-1' }, props.description) : null, slots.default?.()]),
      props.closable ? h('button', { type: 'button', class: 'shrink-0 rounded-sm p-0.5', 'aria-label': '关闭提示', onClick: () => { closed.value = true; emit('close') } }, h(X, { class: 'size-4' })) : null,
    ])
  },
})

export const UiEmpty = defineComponent({
  name: 'UiEmpty', props: { description: { type: String, default: '暂无内容' }, imageSize: Number },
  setup(props, { attrs, slots }) { return () => h('div', { ...attrs, class: cn('ui-empty', attrs.class as string), role: 'status' }, [h(Library, { class: 'mb-4 size-8 text-muted-foreground/60', 'aria-hidden': true }), h('p', { class: 'max-w-prose break-words text-sm text-muted-foreground' }, slots.description?.() ?? props.description), slots.default?.()]) },
})

// 页面内错误态：toast 会消失，数据页加载失败必须留下可重试的可见入口。
export const UiLoadError = defineComponent({
  name: 'UiLoadError', inheritAttrs: false,
  props: { title: { type: String, default: '加载失败' }, message: { type: String, default: '数据加载失败，请稍后重试。' }, retryText: { type: String, default: '重试' }, compact: Boolean },
  emits: ['retry'],
  setup(props, { attrs, slots, emit }) {
    return () => h('div', { ...attrs, role: 'alert', 'data-slot': 'load-error', class: cn('ui-load-error', props.compact && 'is-compact', attrs.class as string) }, [
      h(CircleX, { class: 'ui-load-error-icon', 'aria-hidden': true }),
      h('div', { class: 'ui-load-error-copy' }, [
        h('p', { class: 'ui-load-error-title' }, slots.title?.() ?? props.title),
        h('p', { class: 'ui-load-error-message' }, slots.default?.() ?? props.message),
      ]),
      h(Button, { variant: 'outline', size: 'sm', class: 'ui-load-error-action', onClick: () => emit('retry') }, { default: () => slots.action?.() ?? [h(RefreshCw, { class: 'size-3.5', 'aria-hidden': true }), props.retryText] }),
    ])
  },
})

export const UiResult = defineComponent({
  name: 'UiResult', props: { icon: String, title: String, subTitle: String },
  setup(props, { attrs, slots }) { return () => h('section', { ...attrs, class: cn('ui-result flex min-w-0 flex-col gap-4 py-8', attrs.class as string) }, [h(props.icon === 'error' ? CircleX : props.icon === 'warning' ? TriangleAlert : CircleCheck, { class: 'size-10 text-accent-foreground', 'aria-hidden': true }), h('h3', { class: 'text-xl font-medium' }, props.title), h('p', { class: 'text-sm leading-relaxed text-muted-foreground' }, props.subTitle), slots.extra?.()]) },
})

export const UiSkeleton = defineComponent({
  name: 'UiSkeleton', props: { rows: { type: Number, default: 3 }, animated: Boolean, loading: { type: Boolean, default: true } },
  setup(props, { attrs, slots }) { return () => !props.loading ? slots.default?.() : h('div', { ...attrs, role: 'status', 'aria-label': '正在加载', class: cn('min-w-0 space-y-3 py-4', attrs.class as string) }, Array.from({ length: props.rows }, (_, index) => h(Skeleton, { class: index === 0 ? 'h-7 w-2/5' : 'h-4 w-full', style: index === props.rows - 1 ? { maxWidth: '78%' } : undefined }))) },
})

export const UiProgress = defineComponent({
  name: 'UiProgress', props: { percentage: { type: Number, default: 0 }, color: String, indeterminate: Boolean, showText: { type: Boolean, default: true }, strokeWidth: Number, status: String, textInside: Boolean },
  setup(props, { attrs }) { return () => {
    const value = Math.min(100, Math.max(0, Number(props.percentage) || 0))
    return h('div', { ...attrs, class: cn('ui-progress flex min-w-0 items-center gap-3', attrs.class as string) }, [h(Progress as any, { modelValue: props.indeterminate ? null : value, class: 'min-w-0 flex-1', 'aria-label': attrs['aria-label'] || '完成进度', style: { '--progress-color': props.color || 'var(--primary)', height: props.strokeWidth ? `${Math.min(24, Math.max(2, props.strokeWidth))}px` : undefined } }), props.showText ? h('span', { class: 'shrink-0 text-xs tabular-nums text-muted-foreground' }, `${Math.round(value)}%`) : null])
  } },
})

export const UiLink = defineComponent({
  name: 'UiLink', inheritAttrs: false, props: { type: String, underline: [Boolean, String], disabled: Boolean, icon: null },
  setup(props, { attrs, slots }) { return () => h(attrs.href ? 'a' : 'button', { ...attrs, type: attrs.href ? undefined : 'button', disabled: attrs.href ? undefined : props.disabled, rel: attrs.target === '_blank' ? 'noopener noreferrer' : attrs.rel, 'aria-disabled': props.disabled || undefined, class: cn('ui-link max-w-full break-all text-accent-foreground underline-offset-4 hover:underline', props.disabled && 'pointer-events-none opacity-50', attrs.class as string) }, slots.default?.()) },
})

export const UiDivider = defineComponent({ name: 'UiDivider', setup(_props, { slots, attrs }) { return () => h('div', { ...attrs, role: 'separator', class: cn('ui-divider my-6 flex items-center gap-4 text-xs text-muted-foreground', attrs.class as string) }, [h('span', { class: 'h-px flex-1 bg-border' }), slots.default?.(), slots.default ? h('span', { class: 'h-px flex-1 bg-border' }) : null]) } })
export const UiBadge = defineComponent({ name: 'UiBadge', props: { value: [String, Number], max: { type: Number, default: 99 }, hidden: Boolean, isDot: Boolean }, setup(props, { slots, attrs }) { return () => h('span', { ...attrs, class: cn('relative inline-flex min-w-0 items-center gap-1.5', attrs.class as string) }, [slots.default?.(), !props.hidden ? h('span', { class: 'inline-flex min-h-4 min-w-4 items-center justify-center rounded-full bg-primary px-1 text-[11px] text-primary-foreground', 'aria-label': `${props.value || ''} 条未读消息` }, props.isDot ? '' : typeof props.value === 'number' && props.value > props.max ? `${props.max}+` : String(props.value ?? '')) : null]) } })
export const UiAvatar = defineComponent({ name: 'UiAvatar', props: { src: String, size: [String, Number], shape: String, alt: String }, setup(props, { attrs, slots }) { const failed = ref(false); return () => h('span', { ...attrs, class: cn('inline-flex size-12 shrink-0 items-center justify-center overflow-hidden rounded-full border bg-accent text-accent-foreground', attrs.class as string), style: typeof props.size === 'number' ? { width: `${props.size}px`, height: `${props.size}px` } : attrs.style as any }, props.src && !failed.value ? h('img', { src: props.src, alt: props.alt || '用户头像', class: 'size-full object-cover', onError: () => { failed.value = true } }) : slots.default?.() ?? 'Y') } })

export const UiDescriptions = defineComponent({ name: 'UiDescriptions', props: { column: { type: Number, default: 3 }, title: String, border: Boolean, size: String }, setup(props, { attrs, slots }) { return () => h('section', { ...attrs, class: cn('min-w-0', attrs.class as string) }, [props.title ? h('h3', { class: 'mb-4 font-medium' }, props.title) : null, h('dl', { class: 'ui-descriptions grid min-w-0 gap-x-6 gap-y-5', style: { gridTemplateColumns: `repeat(${props.column}, minmax(0, 1fr))` } }, slots.default?.())]) } })
export const UiDescriptionsItem = defineComponent({ name: 'UiDescriptionsItem', props: { label: String, span: Number }, setup(props, { attrs, slots }) { return () => h('div', { ...attrs, class: cn('min-w-0', attrs.class as string) }, [h('dt', { class: 'mb-1 text-xs text-muted-foreground' }, slots.label?.() ?? props.label), h('dd', { class: 'min-w-0 break-all text-sm leading-relaxed' }, slots.default?.())]) } })

interface CollapseContext { value: () => any; toggle(name: string | number, open: boolean): void }
const collapseContext: InjectionKey<CollapseContext> = Symbol('academy-collapse')
export const UiCollapse = defineComponent({ name: 'UiCollapse', props: { modelValue: null, accordion: Boolean }, emits: ['update:modelValue', 'change'], setup(props, { attrs, slots, emit }) { const local = ref<string[]>([]); provide(collapseContext, { value: () => props.modelValue ?? local.value, toggle: (name, open) => { const old = props.modelValue ?? local.value; const next = props.accordion ? (open ? name : '') : open ? [...new Set([...(Array.isArray(old) ? old : []), name])] : (Array.isArray(old) ? old : []).filter(value => value !== name); local.value = next as string[]; emit('update:modelValue', next); emit('change', next) } }); return () => h('div', { ...attrs, class: cn('ui-collapse min-w-0 divide-y border-y', attrs.class as string) }, slots.default?.()) } })
export const UiCollapseItem = defineComponent({ name: 'UiCollapseItem', props: { name: [String, Number], title: String, disabled: Boolean }, setup(props, { attrs, slots }) { const group = inject(collapseContext, null); const own = useId(); return () => { const name = props.name ?? own; const value = group?.value(); const open = Array.isArray(value) ? value.includes(name) : value === name; return h('details', { ...attrs, open, class: cn('min-w-0', attrs.class as string), onToggle: (event: Event) => { const next = (event.currentTarget as HTMLDetailsElement).open; if (next !== open) group?.toggle(name, next) } }, [h('summary', { class: 'cursor-pointer list-none py-4 text-sm font-medium', 'aria-expanded': open, onClick: (event: Event) => { if (props.disabled) event.preventDefault() } }, slots.title?.() ?? props.title), h('div', { class: 'min-w-0 pb-4' }, slots.default?.())]) } } })
export const UiCollapseTransition = defineComponent({ name: 'UiCollapseTransition', setup(_props, { attrs, slots }) { return () => h('div', { ...attrs, class: 'min-w-0' }, slots.default?.()) } })

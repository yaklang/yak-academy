import { defineComponent, h } from 'vue'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Sheet, SheetContent, SheetDescription, SheetFooter, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'

function overlay(name: string, drawer: boolean) {
  return defineComponent({
    name, inheritAttrs: false,
    props: { modelValue: Boolean, title: String, width: [String, Number], size: [String, Number], top: String, appendToBody: Boolean, destroyOnClose: Boolean, closeOnClickModal: { type: Boolean, default: true }, closeOnPressEscape: { type: Boolean, default: true }, direction: String },
    emits: ['update:modelValue', 'close', 'closed', 'open', 'opened'],
    setup(props, { attrs, slots, emit }) {
      const width = () => props.width ?? props.size ?? (drawer ? '520px' : '640px')
      const update = (value: boolean) => { emit('update:modelValue', value); if (!value) { emit('close'); emit('closed') } else { emit('open'); emit('opened') } }
      return () => h(drawer ? Sheet : Dialog, { open: props.modelValue, 'onUpdate:open': update }, {
        default: () => h((drawer ? SheetContent : DialogContent) as any, {
          ...attrs,
          side: drawer ? 'right' : undefined,
          class: cn('ui-overlay flex min-w-0 flex-col gap-0 overflow-hidden p-0', drawer ? 'w-[calc(100vw-1rem)] sm:max-w-none' : 'max-h-[calc(100dvh-2rem)] sm:max-w-none', attrs.class as string),
          style: [{ width: typeof width() === 'number' ? `${width()}px` : width(), maxWidth: 'calc(100vw - 2rem)' }, attrs.style as any],
          'onInteractOutside': (event: Event) => { if (!props.closeOnClickModal) event.preventDefault() },
          'onEscapeKeyDown': (event: Event) => { if (!props.closeOnPressEscape) event.preventDefault() },
        }, { default: () => [
          h(drawer ? SheetHeader : DialogHeader, { class: 'border-b px-6 py-5 pr-12 text-left' }, { default: () => [h(drawer ? SheetTitle : DialogTitle, { class: 'break-words text-base font-medium' }, { default: () => slots.header?.() ?? props.title ?? '详情' }), h(drawer ? SheetDescription : DialogDescription, { class: 'sr-only' }, { default: () => props.title || '详情与操作' })] }),
          h('div', { class: 'ui-overlay-body min-h-0 min-w-0 overflow-y-auto overscroll-contain px-6 py-5' }, slots.default?.()),
          slots.footer ? h(drawer ? SheetFooter : DialogFooter, { class: 'shrink-0 flex-wrap gap-2 border-t px-6 py-4' }, { default: slots.footer }) : null,
        ] }),
      })
    },
  })
}
export const UiDialog = overlay('UiDialog', false)
export const UiDrawer = overlay('UiDrawer', true)
export const UiTooltip = defineComponent({
  name: 'UiTooltip', props: { content: String, placement: String, disabled: Boolean },
  setup(props, { slots }) {
    return () => props.disabled ? slots.default?.() : h(TooltipProvider, {}, { default: () => h(Tooltip, {}, {
      default: () => [h(TooltipTrigger, { asChild: true }, { default: slots.default }), h(TooltipContent, { side: props.placement?.startsWith('bottom') ? 'bottom' : 'top' }, { default: () => slots.content?.() ?? props.content })],
    }) })
  },
})

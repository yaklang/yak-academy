import { computed, defineComponent, h, inject, provide, ref, useId, watch } from 'vue'
import { Eye, EyeOff, LoaderCircle, X } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { cn } from '@/lib/utils'
import { fieldContext, withoutStyle } from './context'

export const UiButton = defineComponent({
  name: 'UiButton', inheritAttrs: false,
  props: { type: String, size: String, nativeType: { type: String, default: 'button' }, loading: Boolean, disabled: Boolean, link: Boolean, text: Boolean, plain: Boolean, round: Boolean, icon: null },
  setup(props, { attrs, slots }) {
    return () => h(Button as any, {
      ...attrs,
      type: props.nativeType,
      variant: props.link ? 'link' : props.text ? 'ghost' : props.plain ? 'outline' : props.type === 'danger' ? 'destructive' : props.type === 'primary' ? 'default' : 'outline',
      size: props.size === 'small' ? 'sm' : props.size === 'large' ? 'lg' : 'default',
      disabled: props.disabled || props.loading,
      'aria-busy': props.loading || undefined,
      class: cn('ui-button shrink-0', attrs.class as string),
    }, { default: () => [props.loading ? h(LoaderCircle, { class: 'size-4 animate-spin motion-reduce:animate-none', 'aria-hidden': true }) : props.icon ? h(props.icon as any, { class: 'size-4', 'aria-hidden': true }) : null, slots.default?.()] })
  },
})

export const UiTag = defineComponent({
  name: 'UiTag', inheritAttrs: false,
  props: { type: String, size: String, effect: String, closable: Boolean }, emits: ['close'],
  setup(props, { attrs, slots, emit }) {
    return () => h(Badge as any, { ...attrs, variant: props.type === 'danger' ? 'destructive' : 'secondary', class: cn('ui-tag max-w-full whitespace-normal break-words', attrs.class as string), 'data-slot': 'badge', 'data-tone': props.type || 'default' }, {
      default: () => [slots.default?.(), props.closable ? h('button', { type: 'button', 'aria-label': '移除此项', onClick: () => emit('close') }, h(X, { class: 'size-3' })) : null],
    })
  },
})

export const UiForm = defineComponent({
  name: 'UiForm', inheritAttrs: false,
  props: { model: null, labelPosition: String, labelWidth: String, inline: Boolean },
  setup(props, { attrs, slots }) { return () => h('form', { ...attrs, novalidate: true, class: cn('ui-form min-w-0', props.inline && 'ui-form-inline', attrs.class as string) }, slots.default?.()) },
})

export const UiFormItem = defineComponent({
  name: 'UiFormItem', inheritAttrs: false,
  props: { label: String, required: Boolean, prop: String, labelWidth: String },
  setup(props, { attrs, slots }) {
    const id = `field-${useId()}`
    const labelId = `${id}-label`
    provide(fieldContext, { id, labelId, get required() { return props.required } })
    return () => h('div', { ...attrs, class: cn('ui-form-item min-w-0 space-y-2', attrs.class as string) }, [
      props.label || slots.label ? h(Label as any, { id: labelId, for: id, class: 'flex flex-wrap gap-1.5 text-sm font-medium' }, { default: () => [slots.label?.() ?? props.label, props.required ? h('span', { class: 'text-destructive', 'aria-hidden': true }, '*') : null] }) : null,
      h('div', { class: 'ui-form-control min-w-0' }, slots.default?.()),
      slots.error ? h('p', { role: 'alert', class: 'text-sm text-destructive' }, slots.error()) : null,
    ])
  },
})

export const UiInput = defineComponent({
  name: 'UiInput', inheritAttrs: false,
  props: { modelValue: null, type: { type: String, default: 'text' }, placeholder: String, disabled: Boolean, clearable: Boolean, showPassword: Boolean, showWordLimit: Boolean, rows: Number, maxlength: [String, Number], prefixIcon: null, size: String, autocomplete: String },
  emits: ['update:modelValue', 'change', 'input', 'clear'],
  setup(props, { attrs, emit, slots }) {
    const field = inject(fieldContext, null)
    const generatedId = `input-${useId()}`
    const show = ref(false)
    const update = (value: string | number) => { emit('update:modelValue', value); emit('input', value) }
    return () => h('div', { class: cn('ui-input relative min-w-0 max-w-full', attrs.class as string), style: attrs.style as any }, [
      h(props.type === 'textarea' ? Textarea as any : Input as any, {
        ...withoutStyle(attrs), id: attrs.id || field?.id || generatedId, modelValue: props.modelValue ?? '',
        type: props.type === 'password' && show.value ? 'text' : props.type,
        rows: props.rows, placeholder: props.placeholder, disabled: props.disabled, maxlength: props.maxlength,
        autocomplete: props.autocomplete ?? (props.type === 'password' ? 'current-password' : undefined),
        'aria-required': field?.required || undefined,
        class: cn('w-full min-w-0', (props.showPassword || props.clearable) && 'pr-10', (props.prefixIcon || slots.prefix) && 'pl-9'),
        'onUpdate:modelValue': update,
        onChange: (event: Event) => emit('change', (event.target as HTMLInputElement).value),
      }),
      props.prefixIcon || slots.prefix ? h('span', { class: 'pointer-events-none absolute left-3 top-2.5 text-muted-foreground' }, slots.prefix?.() ?? h(props.prefixIcon as any, { class: 'size-4' })) : null,
      props.showPassword ? h('button', { type: 'button', class: 'input-action', 'aria-label': show.value ? '隐藏密码' : '显示密码', onClick: () => { show.value = !show.value } }, h(show.value ? EyeOff : Eye, { class: 'size-4' })) :
      props.clearable && props.modelValue ? h('button', { type: 'button', class: 'input-action', 'aria-label': '清空输入', disabled: props.disabled, onClick: () => { update(''); emit('clear'); emit('change', '') } }, h(X, { class: 'size-3.5' })) : null,
      props.showWordLimit ? h('span', { class: 'mt-1 block text-right text-xs text-muted-foreground' }, `${String(props.modelValue ?? '').length}${props.maxlength ? ` / ${props.maxlength}` : ''}`) : null,
      slots.append ? h('div', { class: 'input-append' }, slots.append()) : null,
    ])
  },
})

export const UiInputNumber = defineComponent({
  name: 'UiInputNumber', inheritAttrs: false,
  props: { modelValue: null, min: Number, max: Number, disabled: Boolean, controlsPosition: String, size: String, step: { type: Number, default: 1 } },
  emits: ['update:modelValue', 'change'],
  setup(props, { attrs, emit }) {
    const field = inject(fieldContext, null)
    function update(value: string | number) {
      const number = value === '' ? null : Number(value)
      const normalized = number === null || !Number.isFinite(number) ? null : Math.min(props.max ?? Infinity, Math.max(props.min ?? -Infinity, number))
      emit('update:modelValue', normalized)
    }
    return () => h('div', { class: cn('ui-input-number w-32 max-w-full min-w-0', attrs.class as string), style: attrs.style as any }, [h(Input as any, { ...withoutStyle(attrs), id: attrs.id || field?.id, type: 'number', step: props.step, min: props.min, max: props.max, disabled: props.disabled, modelValue: props.modelValue ?? '', class: 'w-full min-w-0', 'onUpdate:modelValue': update, onChange: () => emit('change', props.modelValue) })])
  },
})

export const UiSwitch = defineComponent({
  name: 'UiSwitch', inheritAttrs: false,
  props: { modelValue: Boolean, loading: Boolean, disabled: Boolean, activeText: String, inactiveText: String }, emits: ['update:modelValue', 'change'],
  setup(props, { attrs, emit }) {
    const field = inject(fieldContext, null)
    return () => h(Switch as any, { ...attrs, id: attrs.id || field?.id, modelValue: props.modelValue, disabled: props.disabled || props.loading, 'aria-label': attrs['aria-label'] || props.activeText || '切换可见性', 'aria-busy': props.loading || undefined, 'onUpdate:modelValue': (value: boolean) => { emit('update:modelValue', value); emit('change', value) } })
  },
})

function datetimeValue(value: unknown) {
  if (!value) return ''
  const date = value instanceof Date ? value : new Date(String(value))
  if (!Number.isFinite(date.getTime())) return ''
  const pad = (number: number) => String(number).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`
}

export const UiDatePicker = defineComponent({
  name: 'UiDatePicker', inheritAttrs: false,
  props: { modelValue: null, type: String, disabled: Boolean, placeholder: String, startPlaceholder: String, endPlaceholder: String, rangeSeparator: String, valueFormat: String, format: String },
  emits: ['update:modelValue', 'change'],
  setup(props, { attrs, emit }) {
    const field = inject(fieldContext, null)
    const draft = ref(['', ''])
    const range = computed(() => props.type?.includes('range'))
    watch(() => props.modelValue, (value) => { draft.value = range.value && Array.isArray(value) ? value.map(datetimeValue) : [datetimeValue(value), ''] }, { immediate: true })
    function update(value: string | number, index: number) {
      draft.value[index] = String(value)
      const result = range.value ? (draft.value.every(Boolean) ? draft.value.map(part => new Date(part)) : null) : (value ? new Date(String(value)) : null)
      if (!range.value || result || !value) { emit('update:modelValue', result); emit('change', result) }
    }
    return () => h('div', { class: cn('ui-date-picker min-w-0 max-w-full', range.value && 'ui-date-range', attrs.class as string), style: attrs.style as any },
      Array.from({ length: range.value ? 2 : 1 }, (_, index) => h(Input as any, {
        ...withoutStyle(attrs), id: index === 0 ? (attrs.id || field?.id) : `${field?.id || 'date'}-end`,
        type: 'datetime-local', disabled: props.disabled, modelValue: draft.value[index],
        class: 'w-full min-w-0 max-w-full', 'aria-label': range.value ? (index === 0 ? props.startPlaceholder || '开始时间' : props.endPlaceholder || '结束时间') : props.placeholder || undefined,
        'onUpdate:modelValue': (value: string | number) => update(value, index),
      })),
    )
  },
})

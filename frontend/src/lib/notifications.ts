import { shallowRef } from 'vue'
import { toast } from 'vue-sonner'

type Notice = string | { message: string; duration?: number }
type NoticeType = 'success' | 'error' | 'warning' | 'info'
function send(type: NoticeType, value: Notice) {
  const message = typeof value === 'string' ? value : value.message
  const duration = typeof value === 'string' ? 4500 : value.duration ?? 4500
  return toast[type](message, { duration })
}
export const notify = Object.assign((value: Notice) => send('info', value), {
  success: (value: Notice) => send('success', value),
  error: (value: Notice) => send('error', value),
  warning: (value: Notice) => send('warning', value),
  info: (value: Notice) => send('info', value),
})

export interface Confirmation {
  message: string
  title: string
  options: { type?: string; confirmButtonText?: string; cancelButtonText?: string; confirmButtonClass?: string }
  resolve: () => void
  reject: (reason: Error) => void
}
export const confirmation = shallowRef<Confirmation | null>(null)
const queue: Confirmation[] = []
export function finishConfirmation(accepted: boolean) {
  const current = confirmation.value
  if (!current) return
  confirmation.value = null
  if (accepted) current.resolve(); else current.reject(new Error('cancel'))
  queueMicrotask(() => { if (!confirmation.value && queue.length) confirmation.value = queue.shift()! })
}
export const dialogs = {
  confirm(message: string, title = '确认操作', options: Confirmation['options'] = {}) {
    return new Promise<void>((resolve, reject) => {
      const next = { message, title, options, resolve, reject }
      if (confirmation.value) queue.push(next); else confirmation.value = next
    })
  },
}

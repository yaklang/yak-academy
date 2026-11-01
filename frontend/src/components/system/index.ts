import type { App, Directive } from 'vue'
import * as controls from './controls'
import * as composite from './composite'
import * as presentation from './presentation'
import * as table from './table'
import * as overlays from './overlays'

const busy: Directive<HTMLElement, boolean> = {
  mounted(element, binding) { updateBusy(element, binding.value) },
  updated(element, binding) { updateBusy(element, binding.value) },
}
function updateBusy(element: HTMLElement, value: boolean) {
  element.setAttribute('aria-busy', value ? 'true' : 'false')
  element.classList.toggle('is-busy', !!value)
}
export function installDesignSystem(app: App) {
  for (const [name, component] of Object.entries({ ...controls, ...composite, ...presentation, ...table, ...overlays })) {
    if (name.startsWith('Ui')) app.component(name, component)
  }
  app.directive('busy', busy)
}

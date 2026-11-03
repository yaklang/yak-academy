import { computed, ref } from 'vue'

export type ThemePreference = 'light' | 'dark' | 'system'
const storageKey = 'academy_theme'
const isPreference = (value: unknown): value is ThemePreference => ['light', 'dark', 'system'].includes(String(value))
function readPreference(): ThemePreference {
  try {
    const value = localStorage.getItem(storageKey)
    return isPreference(value) ? value : 'system'
  } catch { return 'system' }
}

const preference = ref<ThemePreference>(readPreference())
const systemDark = ref(typeof matchMedia === 'function' && matchMedia('(prefers-color-scheme: dark)').matches)
const resolved = computed(() => preference.value === 'system' ? (systemDark.value ? 'dark' : 'light') : preference.value)
let initialized = false

function applyTheme() {
  document.documentElement.classList.toggle('dark', resolved.value === 'dark')
  document.documentElement.dataset.theme = resolved.value
  document.documentElement.style.colorScheme = resolved.value
  document.querySelector('meta[name="theme-color"]')?.setAttribute('content', resolved.value === 'dark' ? '#050506' : '#ffffff')
}

export function initializeTheme() {
  if (initialized || typeof window === 'undefined') return
  initialized = true
  const media = window.matchMedia('(prefers-color-scheme: dark)')
  systemDark.value = media.matches
  media.addEventListener('change', (event) => { systemDark.value = event.matches; applyTheme() })
  window.addEventListener('storage', (event) => {
    if (event.key === storageKey) {
      preference.value = isPreference(event.newValue) ? event.newValue : 'system'
      applyTheme()
    }
  })
  applyTheme()
}

export function useTheme() {
  function setTheme(value: string) {
    if (!isPreference(value)) return
    preference.value = value
    try { localStorage.setItem(storageKey, value) } catch { /* Private browsing can disable storage. */ }
    applyTheme()
  }
  return { preference, resolved, setTheme }
}

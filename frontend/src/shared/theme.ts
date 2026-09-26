import { ref, watch } from 'vue'

export type Theme = 'system' | 'light' | 'dark'

const KEY = 'lumen.theme'

function load(): Theme {
  try {
    const v = localStorage.getItem(KEY)
    if (v === 'light' || v === 'dark') return v
  } catch {
    /* storage unavailable: fall back to the system theme */
  }
  return 'system'
}

/** The chosen theme. "system" follows Windows' light/dark setting. */
export const theme = ref<Theme>(load())

function apply(v: Theme) {
  const root = document.documentElement
  if (v === 'system') delete root.dataset.theme
  else root.dataset.theme = v
}

apply(theme.value)
watch(theme, (v) => {
  apply(v)
  try {
    localStorage.setItem(KEY, v)
  } catch {
    /* not critical */
  }
})

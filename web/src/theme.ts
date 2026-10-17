export type Theme = 'light' | 'dark'
export const themeKey = 'family-budget-theme'
type ThemeStorage = Pick<Storage, 'getItem' | 'setItem'>

export function parseTheme(value: string | null | undefined): Theme {
  return value === 'dark' ? 'dark' : 'light'
}

export function readTheme(storage: () => ThemeStorage = () => window.localStorage): Theme {
  try {
    return parseTheme(storage().getItem(themeKey))
  } catch {
    return 'light'
  }
}

export function saveTheme(
  theme: Theme,
  storage: () => ThemeStorage = () => window.localStorage,
): boolean {
  try {
    storage().setItem(themeKey, theme)
    return true
  } catch {
    return false
  }
}

export function applyTheme(theme: Theme) {
  document.documentElement.dataset.theme = theme
  document
    .querySelector('meta[name="theme-color"]')
    ?.setAttribute('content', theme === 'dark' ? '#141c18' : '#245c47')
}

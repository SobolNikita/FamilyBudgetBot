import { useEffect, useState } from 'react'
import { applyTheme, parseTheme, readTheme, saveTheme, themeKey } from './theme'

export default function ThemePicker() {
  const [theme, setTheme] = useState(() => parseTheme(document.documentElement.dataset.theme))
  const [saved, setSaved] = useState(true)

  useEffect(() => {
    function sync(event: StorageEvent) {
      if (event.key !== themeKey && event.key !== null) return
      const next = readTheme()
      applyTheme(next)
      setTheme(next)
      setSaved(true)
    }
    window.addEventListener('storage', sync)
    return () => window.removeEventListener('storage', sync)
  }, [])

  return (
    <div className="theme-picker">
      <button
        type="button"
        className="theme-toggle"
        role="switch"
        aria-label="Тёмная тема"
        aria-checked={theme === 'dark'}
        title={theme === 'dark' ? 'Включить светлую тему' : 'Включить тёмную тему'}
        onClick={() => {
          const next = theme === 'dark' ? 'light' : 'dark'
          applyTheme(next)
          setTheme(next)
          setSaved(saveTheme(next))
        }}
      >
        <svg
          className="theme-sun"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.8"
          strokeLinecap="round"
          aria-hidden="true"
        >
          <circle cx="12" cy="12" r="4" />
          <path d="M12 2v2m0 16v2M2 12h2m16 0h2M5 5l1.5 1.5m11 11L19 19M5 19l1.5-1.5m11-11L19 5" />
        </svg>
        <svg
          className="theme-moon"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.8"
          strokeLinecap="round"
          strokeLinejoin="round"
          aria-hidden="true"
        >
          <path d="M20.5 13A8.5 8.5 0 0 1 11 3.5 8.5 8.5 0 1 0 20.5 13Z" />
        </svg>
      </button>
      {!saved && (
        <span className="theme-notice" role="status">
          Браузер не разрешил сохранить тему.
        </span>
      )}
    </div>
  )
}

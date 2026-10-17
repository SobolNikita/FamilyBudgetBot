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
      <select
        aria-label="Тема оформления"
        value={theme}
        onChange={(event) => {
          const next = parseTheme(event.target.value)
          applyTheme(next)
          setTheme(next)
          setSaved(saveTheme(next))
        }}
      >
        <option value="light">Светлая тема</option>
        <option value="dark">Тёмная тема</option>
      </select>
      {!saved && (
        <span className="theme-notice" role="status">
          Браузер не разрешил сохранить тему.
        </span>
      )}
    </div>
  )
}

import assert from 'node:assert/strict'
import test from 'node:test'
import { parseTheme, readTheme, saveTheme, themeKey } from '../src/theme.ts'

test('only explicit dark preference enables dark theme', () => {
  assert.equal(parseTheme('dark'), 'dark')
  for (const value of [null, undefined, '', 'light', 'invalid'])
    assert.equal(parseTheme(value), 'light')
})

test('theme survives a new read of persistent storage', () => {
  const values = new Map()
  const storage = () => ({
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => values.set(key, value),
  })
  assert.equal(readTheme(storage), 'light')
  for (const theme of ['dark', 'light']) {
    assert.equal(saveTheme(theme, storage), true)
    assert.equal(values.get(themeKey), theme)
    assert.equal(readTheme(storage), theme)
  }
})

test('unavailable storage does not prevent rendering or theme changes', () => {
  const unavailable = () => {
    throw new Error('SecurityError')
  }
  assert.equal(readTheme(unavailable), 'light')
  assert.equal(saveTheme('dark', unavailable), false)
  assert.equal(
    saveTheme('dark', () => ({
      getItem: () => null,
      setItem: () => {
        throw new Error('QuotaExceededError')
      },
    })),
    false,
  )
})

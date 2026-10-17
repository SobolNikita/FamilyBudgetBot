import assert from 'node:assert/strict'
import test from 'node:test'
import { readView, viewURL } from '../src/viewState.ts'

const today = '2026-10-17'
const categories = ['еда', 'транспорт', 'другое']

test('first visit defaults to the current family month', () => {
  assert.deepEqual(readView('', today, categories), {
    filters: { user_id: '', from: '2026-10-01', to: today, category: '' }, offset: 0,
  })
})

test('reload restores member, dates, category and page from the URL', () => {
  const view = { filters: { user_id: '9223372036854775807', from: '2026-09-01', to: '2026-09-30', category: 'еда' }, offset: 40 }
  const url = new URL(viewURL('https://family.test/', view), 'https://family.test')
  assert.deepEqual(readView(url.search, today, categories), view)
})

test('cleared dates remain cleared after reload instead of reverting to this month', () => {
  for (const dates of [{ from: '', to: '' }, { from: '', to: '2026-09-30' }, { from: '2026-09-01', to: '' }]) {
    const view = { filters: { user_id: '', category: '', ...dates }, offset: 0 }
    const url = new URL(viewURL('https://family.test/?user_id=111&offset=40&category=еда', view), 'https://family.test')
    assert.deepEqual(readView(url.search, today, categories), view)
    assert.equal(url.searchParams.has('user_id'), false)
    assert.equal(url.searchParams.has('offset'), false)
    assert.equal(url.searchParams.has('category'), false)
  }
})

test('partial links use the default period without losing the selected member', () => {
  const view = readView('?user_id=111', today, categories)
  assert.equal(view.filters.user_id, '111')
  assert.equal(view.filters.from, '2026-10-01')
  assert.equal(view.filters.to, today)
})

test('shared expenses keep period and category while removing member and resetting pagination', () => {
  const member = readView('?user_id=111&from=2026-09-01&to=2026-09-30&category=еда&offset=40', today, categories)
  const shared = { filters: { ...member.filters, user_id: '' }, offset: 0 }
  const url = new URL(viewURL('https://family.test/?user_id=111&offset=40', shared), 'https://family.test')
  assert.equal(url.searchParams.has('user_id'), false)
  assert.equal(url.searchParams.has('offset'), false)
  assert.deepEqual(readView(url.search, today, categories), shared)
})

test('URLs preserve the path, fragment and unrelated query parameters', () => {
  const view = readView('?user_id=111', today, categories)
  const url = new URL(viewURL('https://family.test/budget?source=bookmark#expenses', view), 'https://family.test')
  assert.equal(url.pathname, '/budget')
  assert.equal(url.hash, '#expenses')
  assert.equal(url.searchParams.get('source'), 'bookmark')
  assert.deepEqual(readView(url.search, today, categories), view)
})

test('history entries restore independently without cross-tab shared storage', () => {
  const first = readView('?user_id=111&category=еда&offset=20', today, categories)
  const second = { filters: { ...first.filters, user_id: '222' }, offset: 0 }
  const entries = [first, second].map(view => new URL(viewURL('https://family.test/', view), 'https://family.test'))
  assert.deepEqual(readView(entries[0].search, today, categories), first)
  assert.deepEqual(readView(entries[1].search, today, categories), second)
})

test('invalid dates, ranges, member IDs and offsets are normalized safely', () => {
  const invalid = readView('?user_id=9223372036854775808&from=2026-02-30&to=invalid&category=unknown&offset=1000001', today, categories)
  assert.deepEqual(invalid, { filters: { user_id: '', from: '', to: '', category: '' }, offset: 0 })
  const reversed = readView('?from=2026-10-31&to=2026-10-01', today, categories)
  assert.equal(reversed.filters.from, '')
  assert.equal(reversed.filters.to, '')
  for (const offset of ['-20', '1e2', 'Infinity', '20.5']) assert.equal(readView(`?offset=${offset}`, today, categories).offset, 0)
  assert.equal(readView('?offset=39', today, categories).offset, 20)
  assert.equal(readView('?user_id=000111', today, categories).filters.user_id, '111')
  assert.equal(readView('?from=2024-02-29', today, categories).filters.from, '2024-02-29')
})

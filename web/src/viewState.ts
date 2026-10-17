import type { Filters } from './api'

export type ViewState = { filters: Filters; offset: number }

function date(value: string): string {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value) || value.startsWith('0000')) return ''
  const parsed = new Date(`${value}T00:00:00Z`)
  return Number.isFinite(parsed.getTime()) && parsed.toISOString().slice(0, 10) === value ? value : ''
}

export function readView(search: string, today: string, categories: readonly string[]): ViewState {
  const parameters = new URLSearchParams(search)
  const rawUser = parameters.get('user_id') || ''
  const userID = /^\d{1,19}$/.test(rawUser) ? BigInt(rawUser) : 0n
  let from = date(parameters.get('from') ?? `${today.slice(0, 7)}-01`)
  let to = date(parameters.get('to') ?? today)
  if (from && to && from > to) { from = ''; to = '' }
  const category = parameters.get('category') || ''
  const rawOffset = parameters.get('offset') || '0'
  const offset = /^\d{1,7}$/.test(rawOffset) && Number(rawOffset) <= 1000000 ? Math.floor(Number(rawOffset) / 20) * 20 : 0
  return {
    filters: { user_id: userID > 0n && userID <= 9223372036854775807n ? String(userID) : '', from, to, category: categories.includes(category) ? category : '' },
    offset,
  }
}

export function viewURL(href: string, view: ViewState): string {
  const url = new URL(href)
  for (const [key, value] of Object.entries(view.filters)) {
    if (value || key === 'from' || key === 'to') url.searchParams.set(key, value)
    else url.searchParams.delete(key)
  }
  if (view.offset) url.searchParams.set('offset', String(view.offset))
  else url.searchParams.delete('offset')
  return `${url.pathname}${url.search}${url.hash}`
}

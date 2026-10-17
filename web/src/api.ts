export type User = { id: string; display_name: string }
export type Expense = {
  id: string
  user_id: string
  amount: string
  category: string
  spent_at: string
  comment: string
  version: number
}
export type ExpenseInput = Omit<Expense, 'id'>
export type Page = { items: Expense[]; total: number }
export type Summary = { amount: string; count: number; categories: { category: string; amount: string; count: number }[] }
export type Settings = { currency: string; timezone: string; today: string }
export type Filters = { user_id: string; from: string; to: string; category: string }

export async function api<Result>(path: string, options?: RequestInit): Promise<Result> {
  const response = await fetch(`/api${path}`, {
    ...options,
    headers: { 'Content-Type': 'application/json', ...options?.headers },
  })
  if (!response.ok) {
    const body = await response.json().catch(() => ({}))
    throw new Error(body.error || `Не удалось выполнить запрос (${response.status})`)
  }
  if (response.status === 204) return undefined as Result
  return response.json() as Promise<Result>
}

export function query(filters: Filters, offset = 0): string {
  const parameters = new URLSearchParams({ limit: '20', offset: String(offset) })
  Object.entries(filters).forEach(([key, value]) => { if (value) parameters.set(key, value) })
  return parameters.toString()
}

export function money(amount: string, currency: string): string {
  const [whole, fraction = '00'] = amount.split('.')
  return `${BigInt(whole || '0').toLocaleString('ru-RU')},${fraction.padEnd(2, '0')} ${currency}`
}

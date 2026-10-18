export type User = { id: string; display_name: string; version: number; removed_at?: string }
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
export type Summary = {
  amount: string
  count: number
  categories: { category: string; amount: string; count: number }[]
}
export type Settings = { currency: string; timezone: string; today: string }
export type Filters = { user_id: string; from: string; to: string; category: string }

export class APIError extends Error {
  status: number
  constructor(message: string, status = 0) {
    super(message)
    this.status = status
  }
}

export function requestKey(): string {
  return Array.from(crypto.getRandomValues(new Uint8Array(16)), (value) =>
    value.toString(16).padStart(2, '0'),
  ).join('')
}

export async function api<Result>(
  path: string,
  options?: RequestInit,
  timeoutMs = 15000,
): Promise<Result> {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), timeoutMs)
  try {
    const response = await fetch(`/api${path}`, {
      ...options,
      signal: options?.signal
        ? AbortSignal.any([controller.signal, options.signal])
        : controller.signal,
      headers: { 'Content-Type': 'application/json', ...options?.headers },
    })
    if (!response.ok) {
      const body = await response.json().catch(() => ({}))
      throw new APIError(
        body.error || 'Не получилось выполнить действие. Попробуйте немного позже.',
        response.status,
      )
    }
    if (response.status === 204) return undefined as Result
    return (await response.json()) as Result
  } catch (error) {
    if (options?.signal?.aborted) throw error
    if (error instanceof APIError) throw error
    if (!options?.method || options.method === 'GET') {
      throw new APIError(
        controller.signal.aborted
          ? 'Данные не загрузились вовремя. Повторите запрос.'
          : 'Не удалось загрузить данные. Проверьте соединение.',
      )
    }
    throw new APIError(
      controller.signal.aborted
        ? 'Не удалось получить подтверждение. Проверьте список расходов перед новой попыткой.'
        : 'Связь прервалась. Проверьте список расходов: изменение могло сохраниться.',
    )
  } finally {
    clearTimeout(timer)
  }
}

export function query(filters: Filters, offset = 0): string {
  const parameters = new URLSearchParams({ limit: '20', offset: String(offset) })
  Object.entries(filters).forEach(([key, value]) => {
    if (value) parameters.set(key, value)
  })
  return parameters.toString()
}

export function money(amount: string, currency: string): string {
  const [whole, fraction = '00'] = amount.split('.')
  return `${BigInt(whole || '0').toLocaleString('ru-RU')},${fraction.padEnd(2, '0')} ${currency}`
}

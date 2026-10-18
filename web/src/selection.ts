import type { Expense } from './api'

export type ExpenseRef = Pick<Expense, 'id' | 'version'>
export type SelectionState =
  | { kind: 'manual'; items: Record<string, ExpenseRef> }
  | { kind: 'all'; token: string; count: number; excluded: string[] }

export const emptySelection = (): SelectionState => ({ kind: 'manual', items: {} })
export const maxSelectionItems = 1000

export function selectedCount(state: SelectionState): number {
  return state.kind === 'all'
    ? state.count - state.excluded.length
    : Object.keys(state.items).length
}

export function isSelected(state: SelectionState, id: string): boolean {
  return state.kind === 'all' ? !state.excluded.includes(id) : !!state.items[id]
}

export function toggleExpense(state: SelectionState, expense: ExpenseRef): SelectionState {
  if (state.kind === 'all') {
    const excluded = state.excluded.includes(expense.id)
      ? state.excluded.filter((id) => id !== expense.id)
      : [...state.excluded, expense.id]
    if (excluded.length > maxSelectionItems)
      throw new Error('Можно исключить до 1000 расходов за один раз.')
    return excluded.length >= state.count ? emptySelection() : { ...state, excluded }
  }
  const items = { ...state.items }
  if (items[expense.id]) delete items[expense.id]
  else items[expense.id] = { id: expense.id, version: expense.version }
  if (Object.keys(items).length > maxSelectionItems) {
    throw new Error(
      'Можно отметить до 1000 отдельных расходов. Для большего набора используйте «Выбрать все по фильтру».',
    )
  }
  return { kind: 'manual', items }
}

export function selectionRequest(state: SelectionState) {
  return state.kind === 'all'
    ? { token: state.token, exclude: state.excluded }
    : { items: Object.values(state.items) }
}

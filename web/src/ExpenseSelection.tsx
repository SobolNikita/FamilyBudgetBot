import { createContext, useContext, useEffect, useRef, useState, type ReactNode } from 'react'
import { api, query, type Expense, type Filters } from './api'
import {
  emptySelection,
  isSelected,
  selectedCount,
  toggleExpense,
  type SelectionState,
} from './selection'
import SelectionCheckbox from './SelectionCheckbox'

type SelectionContext = {
  state: SelectionState
  count: number
  busy: boolean
  error: string
  setError: (value: string) => void
  clear: () => void
  selectAll: () => Promise<void>
  toggle: (expense: Expense) => void
}
const Context = createContext<SelectionContext | null>(null)

export function useExpenseSelection() {
  const context = useContext(Context)
  if (!context) throw new Error('ExpenseSelectionProvider is required')
  return context
}

export function ExpenseSelectionProvider({
  filters,
  disabled,
  children,
}: {
  filters: Filters
  disabled: boolean
  children: ReactNode
}) {
  const [state, setState] = useState<SelectionState>(emptySelection)
  const [selecting, setSelecting] = useState(false)
  const [error, setError] = useState('')
  const controller = useRef<AbortController | null>(null)
  useEffect(() => () => controller.current?.abort(), [])
  const busy = disabled || selecting
  function clear() {
    setState(emptySelection())
    setError('')
  }
  async function selectAll() {
    if (busy) return
    if (state.kind === 'all' && state.excluded.length === 0) {
      clear()
      return
    }
    const request = new AbortController()
    controller.current = request
    setSelecting(true)
    setError('')
    try {
      const result = await api<{ token: string; count: number }>(
        `/expenses/selection?${query(filters)}`,
        { signal: request.signal },
      )
      if (request.signal.aborted) return
      if (!result.count) {
        clear()
        setError('Подходящих расходов больше нет. Обновите список.')
      } else setState({ kind: 'all', ...result, excluded: [] })
    } catch (failure) {
      if (!request.signal.aborted) setError((failure as Error).message)
    } finally {
      if (!request.signal.aborted) setSelecting(false)
    }
  }
  function toggle(expense: Expense) {
    if (busy) return
    try {
      setState(toggleExpense(state, expense))
      setError('')
    } catch (failure) {
      setError((failure as Error).message)
    }
  }
  return (
    <Context.Provider
      value={{
        state,
        count: selectedCount(state),
        busy,
        error,
        setError,
        clear,
        selectAll,
        toggle,
      }}
    >
      {children}
    </Context.Provider>
  )
}

export function ExpenseCheckbox({ expense }: { expense: Expense }) {
  const selection = useExpenseSelection()
  return (
    <SelectionCheckbox
      className="expense-select"
      checked={isSelected(selection.state, expense.id)}
      disabled={selection.busy}
      onChange={() => selection.toggle(expense)}
      label={`Выбрать расход ${expense.comment || expense.category}, ${expense.amount}, ${expense.spent_at}, №${expense.id}`}
    />
  )
}

export function SelectableExpenseRow({
  expense,
  children,
}: {
  expense: Expense
  children: ReactNode
}) {
  const { state } = useExpenseSelection()
  return (
    <tr role="row" className={isSelected(state, expense.id) ? 'expense-selected' : undefined}>
      {children}
    </tr>
  )
}

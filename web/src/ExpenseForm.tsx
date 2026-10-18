import { useRef, useState, type FormEvent } from 'react'
import {
  api,
  APIError,
  requestKey,
  type Expense,
  type ExpenseInput,
  type Settings,
  type User,
} from './api'
import Modal from './Modal'

export default function ExpenseForm({
  expense,
  users,
  categories,
  settings,
  defaultUserID,
  onClose,
  onSaved,
}: {
  expense: Expense | 'new'
  users: User[]
  categories: string[]
  settings: Settings
  defaultUserID: string
  onClose: () => void
  onSaved: () => void
}) {
  const [form, setForm] = useState<ExpenseInput>(
    expense === 'new'
      ? {
          user_id: users.some((user) => user.id === defaultUserID)
            ? defaultUserID
            : users[0]?.id || '',
          amount: '',
          category: 'другое',
          spent_at: settings.today,
          comment: '',
          version: 0,
        }
      : {
          user_id: expense.user_id,
          amount: expense.amount,
          category: expense.category,
          spent_at: expense.spent_at,
          comment: expense.comment,
          version: expense.version,
        },
  )
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [uncertain, setUncertain] = useState(false)
  const [conflict, setConflict] = useState(false)
  const [latest, setLatest] = useState<Expense | null>(null)
  const [missing, setMissing] = useState(false)
  const [reloaded, setReloaded] = useState(false)
  const submission = useRef<{ key: string; body: string } | null>(null)
  const inFlight = useRef(false)
  function field(key: keyof ExpenseInput, value: string) {
    setForm((current) => ({ ...current, [key]: value }))
  }
  async function submit(event: FormEvent) {
    event.preventDefault()
    if (inFlight.current || conflict || missing || (uncertain && expense !== 'new')) return
    inFlight.current = true
    setBusy(true)
    setError('')
    try {
      const body = JSON.stringify({ ...form, amount: form.amount.replace(',', '.') })
      if (!submission.current || (!uncertain && submission.current.body !== body))
        submission.current = { key: requestKey(), body }
      await api<Expense>(expense === 'new' ? '/expenses' : `/expenses/${expense.id}`, {
        method: expense === 'new' ? 'POST' : 'PUT',
        headers: expense === 'new' ? { 'Idempotency-Key': submission.current.key } : undefined,
        body: submission.current.body,
      })
      onSaved()
    } catch (failure) {
      setError((failure as Error).message)
      setConflict(failure instanceof APIError && failure.status === 409)
      setMissing(failure instanceof APIError && failure.status === 404)
      setUncertain(
        failure instanceof APIError &&
          (failure.status === 0 || failure.status === 408 || failure.status >= 500),
      )
    } finally {
      inFlight.current = false
      setBusy(false)
    }
  }
  async function reloadCurrent() {
    if (expense === 'new' || inFlight.current) return
    inFlight.current = true
    setBusy(true)
    try {
      const fresh = await api<Expense>(`/expenses/${expense.id}`)
      const submitted = submission.current
        ? (JSON.parse(submission.current.body) as ExpenseInput)
        : null
      const same =
        submitted &&
        ['user_id', 'amount', 'category', 'spent_at', 'comment'].every((key) => {
          const field = key as keyof ExpenseInput
          if (field === 'amount')
            return (
              normalizedAmount(String(submitted[field])) === normalizedAmount(String(fresh[field]))
            )
          return String(submitted[field]).trim() === String(fresh[field])
        })
      if (uncertain && same) {
        onSaved()
        return
      }
      setLatest(fresh)
      setError('')
      setConflict(true)
      setUncertain(false)
    } catch (failure) {
      setError((failure as Error).message)
      setMissing(failure instanceof APIError && failure.status === 404)
    } finally {
      inFlight.current = false
      setBusy(false)
    }
  }
  function applyLatest(keepDraft: boolean) {
    if (!latest) return
    setForm((current) =>
      keepDraft
        ? { ...current, version: latest.version }
        : {
            user_id: latest.user_id,
            amount: latest.amount,
            category: latest.category,
            spent_at: latest.spent_at,
            comment: latest.comment,
            version: latest.version,
          },
    )
    submission.current = null
    setConflict(false)
    setLatest(null)
    setUncertain(false)
    setReloaded(true)
  }
  return (
    <Modal
      title={expense === 'new' ? 'Новый расход' : 'Редактирование расхода'}
      onClose={() => {
        if (!busy) onClose()
      }}
    >
      <form onSubmit={submit}>
        <p className="muted">Небольшая запись — полная картина бюджета.</p>
        {error && (
          <div className="alert error" role="alert">
            {error}
          </div>
        )}
        {uncertain && expense === 'new' && (
          <p role="status">
            Не удалось подтвердить сохранение. Можно нажать «Сохранить» ещё раз с теми же данными
            или закрыть форму и проверить список расходов.
          </p>
        )}
        {expense !== 'new' && (conflict || uncertain) && !missing && (
          <div role="status">
            <p>
              {uncertain
                ? 'Ответ на изменение потерян. Проверьте актуальную запись перед повтором.'
                : 'Запись изменена. Ваш черновик сохранён в форме.'}
            </p>
            {!latest && (
              <button type="button" className="secondary" disabled={busy} onClick={reloadCurrent}>
                Загрузить актуальную запись
              </button>
            )}
            {latest && (
              <>
                <p>
                  Сейчас: {latest.amount} {settings.currency} · {latest.spent_at} ·{' '}
                  {latest.category} ·{' '}
                  {users.find((user) => user.id === latest.user_id)?.display_name || latest.user_id}{' '}
                  · {latest.comment}
                </p>
                <button type="button" className="secondary" onClick={() => applyLatest(false)}>
                  Использовать актуальные данные
                </button>
                <button type="button" className="secondary" onClick={() => applyLatest(true)}>
                  Оставить мой черновик для проверки
                </button>
                <p>
                  Черновик может заменить показанные изменения. Проверьте поля перед сохранением.
                </p>
              </>
            )}
          </div>
        )}
        {missing && (
          <p role="status">Расход удалён. Скопируйте нужные данные из формы и закройте её.</p>
        )}
        {reloaded && (
          <p role="status">Актуальная версия загружена. Проверьте поля перед сохранением.</p>
        )}
        <fieldset className="form-fields" disabled={busy || uncertain || conflict || missing}>
          <div className="form-grid">
            <label>
              Сумма, {settings.currency}
              <input
                autoFocus
                required
                inputMode="decimal"
                placeholder="0,00"
                pattern="[0-9]+([.,][0-9]{1,2})?"
                value={form.amount}
                onChange={(event) => field('amount', event.target.value)}
              />
            </label>
            <label>
              Дата
              <input
                type="date"
                required
                value={form.spent_at}
                onChange={(event) => field('spent_at', event.target.value)}
              />
            </label>
            <label>
              Участник
              <select
                required
                value={form.user_id}
                onChange={(event) => field('user_id', event.target.value)}
              >
                {users.map((user) => (
                  <option key={user.id} value={user.id}>
                    {user.display_name}
                  </option>
                ))}
              </select>
            </label>
            <label>
              Категория
              <select
                value={form.category}
                onChange={(event) => field('category', event.target.value)}
              >
                {categories.map((category) => (
                  <option key={category}>{category}</option>
                ))}
              </select>
            </label>
            <label className="full-width">
              Комментарий
              <textarea
                maxLength={1000}
                rows={3}
                placeholder="Например, продукты на неделю"
                value={form.comment}
                onChange={(event) => field('comment', event.target.value)}
              />
            </label>
          </div>
        </fieldset>
        <div className="modal-actions">
          <button type="button" className="secondary" disabled={busy} onClick={onClose}>
            Отмена
          </button>
          <button
            className="primary"
            disabled={busy || conflict || missing || (uncertain && expense !== 'new')}
          >
            {busy ? 'Сохраняем…' : 'Сохранить расход'}
          </button>
        </div>
      </form>
    </Modal>
  )
}

function normalizedAmount(value: string): string {
  const [whole, fraction = ''] = value.trim().replace(',', '.').split('.')
  return `${whole.replace(/^0+(?=\d)/, '')}.${fraction.padEnd(2, '0')}`
}

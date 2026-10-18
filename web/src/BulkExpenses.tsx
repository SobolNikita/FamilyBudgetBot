import { useEffect, useRef, useState } from 'react'
import { api, query, type Filters } from './api'
import { useExpenseSelection } from './ExpenseSelection'
import { selectionRequest } from './selection'
import SelectionCheckbox from './SelectionCheckbox'
import Modal from './Modal'

export default function BulkExpenses({
  filters,
  total,
  disabled,
  description,
  onDeleted,
  onRefresh,
}: {
  filters: Filters
  total: number
  disabled: boolean
  description: string
  onDeleted: (count: number) => void
  onRefresh: () => void
}) {
  const { state, count, busy, error, setError, clear, selectAll } = useExpenseSelection()
  const [confirming, setConfirming] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const active = useRef(true)
  useEffect(() => {
    active.current = true
    return () => {
      active.current = false
    }
  }, [])

  async function remove() {
    if (!count || deleting) return
    setDeleting(true)
    setError('')
    try {
      const result = await api<{ deleted: number }>(`/expenses/bulk-delete?${query(filters)}`, {
        method: 'POST',
        body: JSON.stringify(selectionRequest(state)),
      })
      if (active.current) onDeleted(result.deleted)
    } catch (failure) {
      // A lost response may mean the deletion committed. Require a fresh list
      // and a new explicit selection, never silently repeat a destructive write.
      if (active.current) {
        clear()
        setError((failure as Error).message)
      }
    } finally {
      if (active.current) setDeleting(false)
    }
  }

  return (
    <>
      <div className="bulk-toolbar" aria-busy={busy}>
        <SelectionCheckbox
          className="bulk-select"
          label="Выбрать все по фильтру"
          checked={state.kind === 'all' && state.excluded.length === 0}
          mixed={count > 0 && (state.kind === 'manual' || state.excluded.length > 0)}
          disabled={busy || !total}
          onChange={selectAll}
        >
          <span className="bulk-select-copy">
            <strong>Выбрать все по фильтру</strong>
            <span>На всех страницах · {total} записей</span>
          </span>
        </SelectionCheckbox>
        {count > 0 && (
          <div className="bulk-selection-actions">
            <span className="selection-count" role="status">
              Выбрано: {count}
            </span>
            <button className="text-button clear-selection" disabled={busy} onClick={clear}>
              Снять выбор
            </button>
            <button
              className="danger"
              disabled={disabled || busy}
              onClick={() => setConfirming(true)}
            >
              Удалить выбранное ({count})
            </button>
          </div>
        )}
        {error && !confirming && (
          <div className="bulk-error" role="alert">
            {error}
          </div>
        )}
      </div>
      {confirming && (
        <Modal
          title="Удалить выбранные расходы?"
          onClose={() => {
            if (!deleting) setConfirming(false)
          }}
        >
          <p className="delete-description">{description}</p>
          {count > 0 && (
            <p className="delete-description">
              Будут удалены выбранные расходы: {count}.{' '}
              {state.kind === 'all'
                ? 'Выбор охватывает все страницы с учётом снятых галочек.'
                : 'Выбор включает отмеченные записи с разных страниц.'}{' '}
              Это действие нельзя отменить.
            </p>
          )}
          {error && (
            <div className="alert error" role="alert">
              {error}
            </div>
          )}
          <div className="modal-actions">
            <button className="secondary" disabled={deleting} onClick={() => setConfirming(false)}>
              Отмена
            </button>
            {count > 0 ? (
              <button className="danger" disabled={deleting} onClick={remove}>
                {deleting ? 'Удаляем…' : `Удалить ${count} расходов`}
              </button>
            ) : (
              <button className="primary" onClick={onRefresh}>
                Обновить список
              </button>
            )}
          </div>
        </Modal>
      )}
    </>
  )
}

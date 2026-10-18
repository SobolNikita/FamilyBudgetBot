import { useEffect, useRef, useState } from 'react'
import { api, query, type Filters } from './api'
import Modal from './Modal'

type Selection = { token: string; count: number }

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
  const [selection, setSelection] = useState<Selection | null>(null)
  const [selecting, setSelecting] = useState(false)
  const [confirming, setConfirming] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [error, setError] = useState('')
  const controller = useRef<AbortController | null>(null)
  const active = useRef(true)
  useEffect(() => {
    active.current = true
    return () => {
      active.current = false
      controller.current?.abort()
    }
  }, [])

  async function selectAll() {
    if (selection) {
      setSelection(null)
      setError('')
      return
    }
    const request = new AbortController()
    controller.current = request
    setSelecting(true)
    setError('')
    try {
      const result = await api<Selection>(`/expenses/selection?${query(filters)}`, {
        signal: request.signal,
      })
      if (request.signal.aborted) return
      if (result.count === 0) {
        setError('Подходящих расходов больше нет. Обновите список.')
      } else {
        setSelection(result)
      }
    } catch (failure) {
      if (!request.signal.aborted) setError((failure as Error).message)
    } finally {
      if (!request.signal.aborted) setSelecting(false)
    }
  }

  async function remove() {
    if (!selection || deleting) return
    setDeleting(true)
    setError('')
    try {
      const result = await api<{ deleted: number }>(`/expenses/bulk-delete?${query(filters)}`, {
        method: 'POST',
        body: JSON.stringify({ token: selection.token }),
      })
      if (active.current) onDeleted(result.deleted)
    } catch (failure) {
      // A lost response may mean the deletion committed. Require a fresh list
      // and a new explicit selection, never silently repeat a destructive write.
      if (active.current) {
        setSelection(null)
        setError((failure as Error).message)
      }
    } finally {
      if (active.current) setDeleting(false)
    }
  }

  return (
    <>
      <div className="bulk-toolbar" aria-busy={selecting}>
        <label className="bulk-select">
          <input
            type="checkbox"
            checked={!!selection}
            disabled={disabled || selecting || !total}
            onChange={selectAll}
          />
          <span>
            {selecting
              ? 'Выбираем расходы…'
              : selection
                ? `Выбрано ${selection.count} по фильтру — все страницы`
                : `Выбрать все по фильтру (${total}) — все страницы`}
          </span>
        </label>
        {selection && (
          <button
            className="danger"
            disabled={disabled || selecting}
            onClick={() => setConfirming(true)}
          >
            Удалить выбранные ({selection.count})
          </button>
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
          {selection && (
            <p className="delete-description">
              Будут удалены все {selection.count} расходов по этому фильтру со всех страниц. Это
              действие нельзя отменить.
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
            {selection ? (
              <button className="danger" disabled={deleting} onClick={remove}>
                {deleting ? 'Удаляем…' : `Удалить ${selection.count} расходов`}
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

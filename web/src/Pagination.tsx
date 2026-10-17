import { useEffect, useState, type FormEvent } from 'react'
import { pageCount, pageNumbers, pageOffset } from './paginationState'

export default function Pagination({
  total,
  offset,
  disabled,
  onChange,
}: {
  total: number
  offset: number
  disabled: boolean
  onChange: (offset: number) => void
}) {
  const current = Math.floor(offset / 20) + 1
  const count = pageCount(total)
  const [target, setTarget] = useState(String(current))
  useEffect(() => {
    setTarget(String(current))
  }, [current, count])

  function submit(event: FormEvent) {
    event.preventDefault()
    const nextOffset = pageOffset(target, count)
    if (!disabled && total > 0 && nextOffset !== null && nextOffset !== offset) onChange(nextOffset)
  }

  return (
    <footer className="pagination">
      <span>
        {total ? `${offset + 1}–${Math.min(offset + 20, total)} из ${total}` : '0 записей'}
      </span>
      <nav className="page-buttons" aria-label="Страницы расходов">
        <button
          disabled={disabled || current <= 1}
          onClick={() => onChange(offset - 20)}
          aria-label="Предыдущая страница"
        >
          ← Назад
        </button>
        {total > 0 &&
          pageNumbers(current, count).map((page) =>
            typeof page === 'number' ? (
              <button
                key={page}
                aria-label={`Страница ${page}`}
                aria-current={page === current ? 'page' : undefined}
                disabled={disabled}
                onClick={() => {
                  if (page !== current) onChange((page - 1) * 20)
                }}
              >
                {page}
              </button>
            ) : (
              <span key={page} aria-hidden="true">
                …
              </span>
            ),
          )}
        <button
          disabled={disabled || current >= count}
          onClick={() => onChange(offset + 20)}
          aria-label="Следующая страница"
        >
          Далее →
        </button>
      </nav>
      {count > 1 && (
        <form className="page-jump" onSubmit={submit}>
          <label>
            Страница{' '}
            <input
              type="number"
              inputMode="numeric"
              min="1"
              max={count}
              step="1"
              required
              value={target}
              disabled={disabled}
              onChange={(event) => setTarget(event.target.value)}
            />
          </label>
          <span>из {count}</span>
          <button type="submit" disabled={disabled}>
            Перейти
          </button>
        </form>
      )}
    </footer>
  )
}

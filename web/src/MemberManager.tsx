import { useEffect, useRef, useState } from 'react'
import { api, APIError, type User } from './api'
import Modal from './Modal'

export default function MemberManager({
  onClose,
  onChanged,
}: {
  onClose: () => void
  onChanged: () => void
}) {
  const [active, setActive] = useState<User[]>([])
  const [removed, setRemoved] = useState<User[]>([])
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [selected, setSelected] = useState<{ user: User; restore: boolean } | null>(null)
  const [uncertain, setUncertain] = useState(false)
  const [revision, setRevision] = useState(0)
  const inFlight = useRef(false)
  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    Promise.all([
      api<User[]>('/users', { signal: controller.signal }),
      api<User[]>('/users/removed', { signal: controller.signal }),
    ])
      .then(([people, deleted]) => {
        if (!controller.signal.aborted) {
          setActive(people)
          setRemoved(deleted)
          setError('')
        }
      })
      .catch((failure) => {
        if (!controller.signal.aborted) setError(failure.message)
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })
    return () => controller.abort()
  }, [revision])
  async function submit() {
    if (!selected || inFlight.current) return
    inFlight.current = true
    setBusy(true)
    setError('')
    try {
      const { user, restore } = selected
      await api<void>(`/users/${user.id}${restore ? '/restore' : ''}?version=${user.version}`, {
        method: restore ? 'POST' : 'DELETE',
      })
      setSelected(null)
      setUncertain(false)
      setRevision((value) => value + 1)
      onChanged()
      setNotice(
        restore
          ? 'Доступ восстановлен. Участнику нужно снова представиться боту. Старые расходы не возвращаются.'
          : 'Участник и его расходы удалены. Повторная регистрация заблокирована до восстановления доступа.',
      )
    } catch (failure) {
      setError((failure as Error).message)
      setUncertain(
        failure instanceof APIError &&
          (failure.status === 0 || failure.status === 408 || failure.status >= 500),
      )
      if (failure instanceof APIError && (failure.status === 409 || failure.status === 404))
        setSelected(null)
    } finally {
      inFlight.current = false
      setBusy(false)
    }
  }
  return (
    <Modal
      title="Участники семьи"
      onClose={() => {
        if (!busy) onClose()
      }}
    >
      {error && (
        <div className="alert error" role="alert">
          {error}
          <button
            disabled={busy}
            onClick={() => {
              setSelected(null)
              setUncertain(false)
              setRevision((value) => value + 1)
              onChanged()
            }}
          >
            Обновить список
          </button>
        </div>
      )}
      {notice && <p role="status">{notice}</p>}
      {selected ? (
        <>
          <h3>{selected.restore ? 'Восстановить доступ?' : 'Удалить участника?'}</h3>
          <p>
            <strong>{selected.user.display_name || `Telegram ID ${selected.user.id}`}</strong>
          </p>
          <p>
            {selected.restore
              ? 'Расходы и прежняя история не восстановятся. При следующем сообщении бот попросит представиться заново.'
              : 'Все расходы этого участника будут безвозвратно удалены, а общие итоги пересчитаны. История диалога и ожидающие запросы будут очищены. Бот не позволит зарегистрироваться снова до восстановления доступа.'}
          </p>
          {!selected.restore && (
            <p>
              Уже отправленные сообщения в Telegram и резервные копии базы этим действием не
              удаляются.
            </p>
          )}
          {uncertain && (
            <p role="status">
              Ответ потерян. Можно безопасно повторить эту же операцию либо обновить список и
              проверить результат.
            </p>
          )}
          <div className="modal-actions">
            <button
              className="secondary"
              disabled={busy}
              onClick={() => {
                setSelected(null)
                setUncertain(false)
              }}
            >
              Отмена
            </button>
            <button
              className={selected.restore ? 'primary' : 'danger'}
              disabled={busy}
              onClick={submit}
            >
              {busy
                ? 'Выполняем…'
                : selected.restore
                  ? 'Восстановить доступ'
                  : 'Удалить участника и расходы'}
            </button>
          </div>
        </>
      ) : (
        <>
          {loading ? (
            <p role="status">Загрузка участников…</p>
          ) : (
            <>
              <h3>В семье</h3>
              {active.length === 0 && <p>Участников пока нет.</p>}
              <ul className="member-management-list">
                {active.map((user) => (
                  <li key={user.id}>
                    <span>
                      {user.display_name}
                      <small>Telegram ID {user.id}</small>
                    </span>
                    <button
                      className="secondary"
                      onClick={() => {
                        setSelected({ user, restore: false })
                        setNotice('')
                      }}
                    >
                      Удалить
                    </button>
                  </li>
                ))}
              </ul>
              <h3>Удалённые участники</h3>
              <p>
                Сохранены только служебные сведения для блокировки повторного входа. Имя и расходы
                удалены.
              </p>
              {removed.length === 0 && <p>Нет удалённых участников.</p>}
              <ul className="member-management-list">
                {removed.map((user) => (
                  <li key={user.id}>
                    <span>Telegram ID {user.id}</span>
                    <button
                      className="secondary"
                      onClick={() => {
                        setSelected({ user, restore: true })
                        setNotice('')
                      }}
                    >
                      Восстановить доступ
                    </button>
                  </li>
                ))}
              </ul>
            </>
          )}
        </>
      )}
    </Modal>
  )
}

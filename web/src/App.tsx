import { useEffect, useRef, useState, type FormEvent } from 'react'
import { api, money, query, type Expense, type ExpenseInput, type Filters, type Page, type Settings, type Summary, type User } from './api'
import { readView, viewURL, type ViewState } from './viewState'

const emptyFilters: Filters = { user_id: '', from: '', to: '', category: '' }
const symbols: Record<string, string> = { еда: '◒', транспорт: '↗', жильё: '⌂', развлечения: '✳', здоровье: '+', покупки: '◇', другое: '⋯' }

function saveView(view: ViewState, replace = false) {
  const url = viewURL(window.location.href, view)
  if (url !== `${window.location.pathname}${window.location.search}${window.location.hash}`) {
    window.history[replace ? 'replaceState' : 'pushState'](window.history.state, '', url)
  }
}

export default function App() {
  const [users, setUsers] = useState<User[]>([])
  const [categories, setCategories] = useState<string[]>([])
  const [settings, setSettings] = useState<Settings | null>(null)
  const [filters, setFilters] = useState<Filters>(emptyFilters)
  const [offset, setOffset] = useState(0)
  const [page, setPage] = useState<Page>({ items: [], total: 0 })
  const [summary, setSummary] = useState<Summary>({ amount: '0.00', count: 0, categories: [] })
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [revision, setRevision] = useState(0)
  const [editing, setEditing] = useState<Expense | 'new' | null>(null)
  const [deleting, setDeleting] = useState<Expense | null>(null)
  const [deleteBusy, setDeleteBusy] = useState(false)

  useEffect(() => {
    if (settings) return
    const controller = new AbortController()
    setLoading(true); setError('')
    Promise.all([
      api<string[]>('/categories', { signal: controller.signal }),
      api<Settings>('/config', { signal: controller.signal }),
    ]).then(([names, config]) => {
      if (controller.signal.aborted) return
      const view = readView(window.location.search, config.today, names)
      setFilters(view.filters); setOffset(view.offset); saveView(view, true)
      setCategories(names); setSettings(config)
    }).catch(failure => { if (!controller.signal.aborted) { setError(failure.message); setLoading(false) } })
    return () => controller.abort()
  }, [revision, settings])

  useEffect(() => {
    if (!settings) return
    function restore() {
      const view = readView(window.location.search, settings!.today, categories)
      setFilters(view.filters); setOffset(view.offset); saveView(view, true)
      setEditing(null); setDeleting(null); setNotice('')
    }
    window.addEventListener('popstate', restore)
    return () => window.removeEventListener('popstate', restore)
  }, [settings, categories])

  useEffect(() => {
    if (!settings) return
    const controller = new AbortController()
    setLoading(true); setError('')
    Promise.all([
      api<Page>(`/expenses?${query(filters, offset)}`, { signal: controller.signal }),
      api<Summary>(`/reports/summary?${query(filters)}`, { signal: controller.signal }),
      api<User[]>('/users', { signal: controller.signal }),
    ]).then(([expenses, totals, people]) => {
      if (controller.signal.aborted) return
      const lastOffset = expenses.total > 0 ? Math.floor((expenses.total - 1) / 20) * 20 : 0
      if (offset > lastOffset) { changeView(filters, lastOffset, true); return }
      setPage(expenses); setSummary(totals); setUsers(people)
    }).catch(failure => { if (!controller.signal.aborted) setError(failure.message) })
      .finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => controller.abort()
  }, [filters, offset, revision, settings])

  function filter(key: keyof Filters, value: string) {
    changeView({ ...filters, [key]: value }, 0); setNotice('')
  }

  function changeView(nextFilters: Filters, nextOffset: number, replace = false) {
    setFilters(nextFilters); setOffset(nextOffset)
    saveView({ filters: nextFilters, offset: nextOffset }, replace)
  }

  function saved() {
    setEditing(null); setRevision(current => current + 1); setNotice('Расход сохранён')
  }

  async function remove() {
    if (!deleting) return
    setDeleteBusy(true); setError('')
    try {
      await api<void>(`/expenses/${deleting.id}?version=${deleting.version}`, { method: 'DELETE' })
      setDeleting(null); setRevision(current => current + 1); setNotice('Расход удалён')
    } catch (failure) { setError((failure as Error).message); setDeleting(null) }
    finally { setDeleteBusy(false) }
  }

  const currency = settings?.currency || ''
  const period = filters.from || filters.to ? `${filters.from ? dateLabel(filters.from) : 'Начало'} — ${filters.to ? dateLabel(filters.to) : 'сегодня'}` : 'За всё время'

  return <div className="layout">
    <aside className="sidebar">
      <a className="brand" href="/" aria-label="Семейный бюджет, главная"><span className="brand-icon">⌂</span><span>Семейный<br /><strong>бюджет</strong></span></a>
      <div className="nav-caption">ВАШЕ ПРОСТРАНСТВО</div>
      <div className="nav-active"><span>▦</span> Расходы семьи <span className="nav-dot" /></div>
      <div className="family-list"><div className="nav-caption">УЧАСТНИКИ</div>{users.map((user, index) => <button className={`member ${filters.user_id === user.id ? 'selected' : ''}`} key={user.id} onClick={() => filter('user_id', filters.user_id === user.id ? '' : user.id)}><span className={`avatar tone-${index % 3}`}>{user.display_name.slice(0, 1)}</span>{user.display_name}</button>)}</div>
      <div className="sidebar-note"><span>Маленькие расходы.<br />Большая ясность.</span><p>Все семейные траты<br />в одном месте.</p></div>
      <div className="sidebar-footer">СЕМЬЯ · ОБЩИЙ БЮДЖЕТ</div>
    </aside>

    <main>
      <header className="topbar"><span>Обзор семьи <span className="breadcrumb">/</span> Расходы</span><span className="period-label">{settings?.timezone || 'Семейные финансы'}</span></header>
      <section className="heading"><div><div className="eyebrow">КАЖДЫЙ ДЕНЬ ПОД КОНТРОЛЕМ</div><h1>Расходы семьи</h1><p>Понимайте, куда уходят деньги. Планируйте вместе.</p></div><button className="primary" disabled={!settings || !users.length} onClick={() => setEditing('new')}><span>＋</span> Добавить расход</button></section>

      {error && <div className="alert error" role="alert">{error}<button onClick={() => setRevision(current => current + 1)}>Повторить</button></div>}
      {notice && <div className="alert success" role="status">{notice}<button aria-label="Закрыть уведомление" onClick={() => setNotice('')}>×</button></div>}
      {!loading && !error && settings && !users.length && <div className="alert success" role="status">Чтобы добавить участника, напишите Telegram-боту и ответьте на вопрос об имени. Затем нажмите «Обновить».</div>}

      <section className="stats" aria-busy={loading}>
        <article className="total-card"><div className="stat-label">Общие расходы <span>↗</span></div><div className="total-value">{loading ? '…' : money(summary.amount, currency)}</div><div className="stat-foot">{period}</div><div className="card-decoration" aria-hidden="true">⌂</div></article>
        <article className="stat-card"><div className="stat-label">Записей за период <span className="stat-symbol">▤</span></div><div className="stat-value">{loading ? '…' : summary.count}</div><div className="stat-foot">Каждая мелочь имеет значение</div></article>
        <article className="stat-card"><div className="stat-label">Участников семьи <span className="stat-symbol">♧</span></div><div className="stat-value">{users.length}</div><div className="stat-foot">Один бюджет для самых близких</div></article>
      </section>

      <section className="content-grid">
        <div className="expenses-panel">
          <div className="section-title"><h2>История расходов</h2><button className="text-button" onClick={() => setRevision(current => current + 1)} disabled={loading}>↻ Обновить</button></div>
          <div className="filters">
            <label>Участник<select value={filters.user_id} onChange={event => filter('user_id', event.target.value)}><option value="">Вся семья</option>{filters.user_id && !users.some(user => user.id === filters.user_id) && <option value={filters.user_id}>Участник #{filters.user_id}</option>}{users.map(user => <option key={user.id} value={user.id}>{user.display_name}</option>)}</select></label>
            <label>Категория<select value={filters.category} onChange={event => filter('category', event.target.value)}><option value="">Все категории</option>{categories.map(category => <option key={category}>{category}</option>)}</select></label>
            <label>С даты<input type="date" value={filters.from} max={filters.to || undefined} onChange={event => filter('from', event.target.value)} /></label>
            <label>По дату<input type="date" value={filters.to} min={filters.from || undefined} onChange={event => filter('to', event.target.value)} /></label>
            <button className="reset-button" onClick={() => changeView({ ...emptyFilters }, 0)}>Сбросить</button>
          </div>
          <div className="table-wrap" aria-busy={loading}>
            <table><thead><tr><th>Расход</th><th>Участник</th><th>Дата</th><th className="amount-cell">Сумма</th><th><span className="sr-only">Действия</span></th></tr></thead>
              <tbody>{!loading && !error && page.items.map(expense => <tr key={expense.id}>
                <td><div className="expense-name"><span className={`category-icon category-${categories.indexOf(expense.category)}`}>{symbols[expense.category] || '◇'}</span><div><strong>{expense.comment || expense.category}</strong><span className="category-name">{expense.category}</span></div></div></td>
                <td><span className="user-name">{users.find(user => user.id === expense.user_id)?.display_name || expense.user_id}</span></td>
                <td className="date-cell">{dateLabel(expense.spent_at)}</td>
                <td className="amount-cell"><strong>{money(expense.amount, currency)}</strong></td>
                <td><div className="row-actions"><button aria-label={`Изменить расход ${expense.comment || expense.id}`} title="Изменить" onClick={() => setEditing(expense)}>✎</button><button className="delete-button" aria-label={`Удалить расход ${expense.comment || expense.id}`} title="Удалить" onClick={() => setDeleting(expense)}>×</button></div></td>
              </tr>)}</tbody>
            </table>
            {loading && <div className="empty-state" role="status"><span className="loader" />Загружаем расходы…</div>}
            {!loading && !error && page.items.length === 0 && <div className="empty-state"><span className="empty-icon">◇</span><h3>Здесь пока тихо</h3><p>Добавьте первый расход или измените фильтры.</p><button className="secondary" disabled={!settings || !users.length} onClick={() => setEditing('new')}>Добавить расход</button></div>}
            {!loading && error && <div className="empty-state">Не удалось загрузить данные. Попробуйте ещё раз.</div>}
          </div>
          <footer className="pagination"><span>{page.total ? `${offset + 1}–${Math.min(offset + 20, page.total)} из ${page.total}` : '0 записей'}</span><div><button disabled={loading || offset === 0} onClick={() => changeView(filters, Math.max(0, offset - 20))}>← Назад</button><button disabled={loading || offset + 20 >= page.total} onClick={() => changeView(filters, offset + 20)}>Далее →</button></div></footer>
        </div>

        <aside className="breakdown"><h2>По категориям</h2><p className="muted">За выбранный период</p><div className="breakdown-list">{!loading && summary.categories.map(category => <div className="breakdown-item" key={category.category}><div><span><i className={`category-dot category-${categories.indexOf(category.category)}`} />{category.category}</span><strong>{money(category.amount, currency)}</strong></div><progress max="100" value={summary.amount === '0' || summary.amount === '0.00' ? 0 : Number(category.amount) / Number(summary.amount) * 100} aria-label={`Доля категории ${category.category}`} /></div>)}{!loading && !summary.categories.length && <p className="muted">Распределение появится вместе с первыми расходами.</p>}{loading && <p className="muted">Загрузка…</p>}</div><div className="tip"><span>✦</span><p>Записывайте расходы сразу — даже кофе по пути домой.</p></div></aside>
      </section>
      <footer className="page-footer">Больше ясности. Меньше забот.<span>Семейный бюджет</span></footer>
    </main>

    {editing && settings && <ExpenseForm key={editing === 'new' ? 'new' : editing.id} expense={editing} users={users} categories={categories} settings={settings} onClose={() => setEditing(null)} onSaved={saved} />}
    {deleting && <Modal title="Удалить расход?" onClose={() => { if (!deleteBusy) setDeleting(null) }}><p className="delete-description">{deleting.comment || deleting.category} · {money(deleting.amount, currency)}<br />{dateLabel(deleting.spent_at)}. Это действие нельзя отменить.</p><div className="modal-actions"><button className="secondary" disabled={deleteBusy} onClick={() => setDeleting(null)}>Отмена</button><button className="danger" disabled={deleteBusy} onClick={remove}>{deleteBusy ? 'Удаляем…' : 'Удалить'}</button></div></Modal>}
  </div>
}

function ExpenseForm({ expense, users, categories, settings, onClose, onSaved }: { expense: Expense | 'new'; users: User[]; categories: string[]; settings: Settings; onClose: () => void; onSaved: () => void }) {
  const [form, setForm] = useState<ExpenseInput>(expense === 'new' ? { user_id: users[0]?.id || '', amount: '', category: 'другое', spent_at: settings.today, comment: '', version: 0 } : { user_id: expense.user_id, amount: expense.amount, category: expense.category, spent_at: expense.spent_at, comment: expense.comment, version: expense.version })
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  function field(key: keyof ExpenseInput, value: string) { setForm(current => ({ ...current, [key]: value })) }
  async function submit(event: FormEvent) {
    event.preventDefault(); setBusy(true); setError('')
    try {
      await api<Expense>(expense === 'new' ? '/expenses' : `/expenses/${expense.id}`, { method: expense === 'new' ? 'POST' : 'PUT', body: JSON.stringify({ ...form, amount: form.amount.replace(',', '.') }) })
      onSaved()
    } catch (failure) { setError((failure as Error).message) }
    finally { setBusy(false) }
  }
  return <Modal title={expense === 'new' ? 'Новый расход' : 'Редактирование расхода'} onClose={() => { if (!busy) onClose() }}>
    <form onSubmit={submit}><p className="muted">Небольшая запись — полная картина бюджета.</p>{error && <div className="alert error" role="alert">{error}</div>}
      <div className="form-grid"><label>Сумма, {settings.currency}<input autoFocus required inputMode="decimal" placeholder="0,00" pattern="[0-9]+([.,][0-9]{1,2})?" value={form.amount} onChange={event => field('amount', event.target.value)} /></label><label>Дата<input type="date" required value={form.spent_at} onChange={event => field('spent_at', event.target.value)} /></label><label>Участник<select required value={form.user_id} onChange={event => field('user_id', event.target.value)}>{users.map(user => <option key={user.id} value={user.id}>{user.display_name}</option>)}</select></label><label>Категория<select value={form.category} onChange={event => field('category', event.target.value)}>{categories.map(category => <option key={category}>{category}</option>)}</select></label><label className="full-width">Комментарий<textarea maxLength={1000} rows={3} placeholder="Например, продукты на неделю" value={form.comment} onChange={event => field('comment', event.target.value)} /></label></div>
      <div className="modal-actions"><button type="button" className="secondary" disabled={busy} onClick={onClose}>Отмена</button><button className="primary" disabled={busy}>{busy ? 'Сохраняем…' : 'Сохранить расход'}</button></div>
    </form>
  </Modal>
}

function Modal({ title, onClose, children }: { title: string; onClose: () => void; children: React.ReactNode }) {
  const reference = useRef<HTMLDialogElement>(null)
  useEffect(() => { reference.current?.showModal() }, [])
  return <dialog ref={reference} className="modal" aria-labelledby="modal-title" onCancel={event => { event.preventDefault(); onClose() }}><div className="modal-header"><h2 id="modal-title">{title}</h2><button aria-label="Закрыть" onClick={onClose}>×</button></div>{children}</dialog>
}

function dateLabel(date: string): string {
  const [year, month, day] = date.split('-')
  return `${day}.${month}.${year}`
}

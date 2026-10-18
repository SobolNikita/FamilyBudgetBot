import assert from 'node:assert/strict'
import { createServer } from 'node:http'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { spawn } from 'node:child_process'
import { resolve } from 'node:path'

const root = resolve('web/dist')
const requests = []
let empty = false
let stress = false
let failExpenses = false
let configDate = '2026-10-17'
let loseCreateResponse = true
const created = new Map()
const createKeys = []
let editRecord = null
let loseEditResponse = false
let editWrites = 0
let memberDeleted = false
let memberRestored = false
let memberDeleteCalls = 0
let bulkMode = 'success'
let bulkDeleteCalls = 0
const bulkRequests = []
const bulkDeletedIDs = new Set()
await mkdir(resolve('.cache'), { recursive: true })
const server = createServer(async (request, response) => {
  response.setHeader(
    'Content-Security-Policy',
    "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'self'",
  )
  response.setHeader('X-Content-Type-Options', 'nosniff')
  const url = new URL(request.url, 'http://localhost')
  if (url.pathname.startsWith('/api/')) {
    requests.push(url.pathname + url.search)
    if (url.pathname === '/api/expenses/selection') {
      response.setHeader('Content-Type', 'application/json')
      response.end(
        JSON.stringify({
          token: 'confirmed-snapshot',
          count: empty ? 0 : 400 - bulkDeletedIDs.size,
        }),
      )
      return
    }
    if (url.pathname === '/api/expenses/bulk-delete' && request.method === 'POST') {
      let body = ''
      for await (const chunk of request) body += chunk
      const input = JSON.parse(body)
      bulkRequests.push({ url: url.toString(), body: input })
      bulkDeleteCalls++
      response.setHeader('Content-Type', 'application/json')
      if (bulkMode === 'conflict') {
        response.writeHead(409)
        response.end(
          JSON.stringify({ error: 'Набор расходов изменился. Выберите расходы заново.' }),
        )
      } else if (bulkMode === 'lost') {
        empty = true
        response.writeHead(504)
        response.end(JSON.stringify({ error: 'Ответ потерян. Обновите список расходов.' }))
      } else {
        const ids = input.items
          ? input.items.map((item) => item.id)
          : Array.from({ length: 400 }, (_, index) => String(index + 1)).filter(
              (id) => !(input.exclude || []).includes(id) && !bulkDeletedIDs.has(id),
            )
        for (const id of ids) bulkDeletedIDs.add(id)
        response.end(JSON.stringify({ deleted: ids.length }))
      }
      return
    }
    if (url.pathname === '/api/users/111' && request.method === 'DELETE') {
      memberDeleteCalls++
      memberDeleted = true
      response.writeHead(204)
      response.end()
      return
    }
    if (url.pathname === '/api/users/111/restore' && request.method === 'POST') {
      memberRestored = true
      response.writeHead(204)
      response.end()
      return
    }
    if (editRecord && url.pathname === `/api/expenses/${editRecord.id}`) {
      response.setHeader('Content-Type', 'application/json')
      if (request.method === 'PUT') {
        let body = ''
        for await (const chunk of request) body += chunk
        const input = JSON.parse(body)
        if (input.version !== editRecord.version) {
          response.writeHead(409)
          response.end(JSON.stringify({ error: 'Данные уже изменились' }))
          return
        }
        editRecord = { ...editRecord, ...input, version: input.version + 1 }
        editWrites++
        if (loseEditResponse) {
          loseEditResponse = false
          response.writeHead(504)
          response.end(JSON.stringify({ error: 'Ответ потерян после сохранения' }))
          return
        }
      }
      response.end(JSON.stringify(editRecord))
      return
    }
    if (url.pathname === '/api/expenses' && request.method === 'POST') {
      let body = ''
      for await (const chunk of request) body += chunk
      const key = request.headers['idempotency-key']
      createKeys.push(key)
      if (!created.has(key)) created.set(key, { ...JSON.parse(body), id: '9001' })
      if (loseCreateResponse) {
        loseCreateResponse = false
        response.writeHead(504, { 'Content-Type': 'application/json' })
        response.end(JSON.stringify({ error: 'Шлюз потерял ответ после сохранения' }))
        return
      }
      response.writeHead(201, { 'Content-Type': 'application/json' })
      response.end(JSON.stringify(created.get(key)))
      return
    }
    if (url.pathname === '/api/expenses' && failExpenses) {
      response.writeHead(503, { 'Content-Type': 'application/json' })
      response.end(JSON.stringify({ error: 'Тестовый сбой загрузки' }))
      return
    }
    const total = empty ? 0 : 400 - bulkDeletedIDs.size
    const offset = Number(url.searchParams.get('offset') || 0)
    const data = {
      '/api/config': { currency: 'RUB', timezone: 'Europe/Moscow', today: configDate },
      '/api/categories': ['еда', 'транспорт', 'другое'],
      '/api/users': [
        ...(!memberDeleted
          ? [
              {
                id: '111',
                display_name: stress ? 'Оченьдлинноеимя'.repeat(8) : 'Никита',
                version: 1,
              },
            ]
          : []),
        { id: '222', display_name: 'Ева', version: 1 },
      ],
      '/api/users/removed':
        memberDeleted && !memberRestored
          ? [{ id: '111', display_name: '', version: 2, removed_at: '2026-10-18T00:00:00Z' }]
          : [],
      '/api/expenses': {
        total,
        items: empty
          ? []
          : Array.from({ length: 400 }, (_, index) => ({
              id: String(index + 1),
              user_id: url.searchParams.get('user_id') || '111',
              amount: stress ? '999999999999.99' : '5.00',
              category: 'еда',
              spent_at: '2026-10-16',
              comment: stress ? 'Длинноеописание'.repeat(30) : 'Тестовая покупка',
              version: 1,
            }))
              .filter((item) => !bulkDeletedIDs.has(item.id))
              .slice(offset, offset + 20),
      },
      '/api/reports/summary': {
        amount: stress ? '99999999999999999.99' : '300.00',
        count: total,
        categories: empty
          ? []
          : [{ category: 'еда', amount: stress ? '99999999999999999.99' : '300.00' }],
      },
    }[url.pathname]
    response.setHeader('Content-Type', 'application/json')
    response.end(JSON.stringify(data))
    return
  }
  try {
    const path = url.pathname.startsWith('/assets/')
      ? resolve(root, '.' + url.pathname)
      : resolve(root, 'index.html')
    response.setHeader(
      'Content-Type',
      path.endsWith('.js') ? 'text/javascript' : path.endsWith('.css') ? 'text/css' : 'text/html',
    )
    response.end(await readFile(path))
  } catch {
    response.writeHead(404)
    response.end()
  }
})
await new Promise((resolve) => server.listen(5189, '127.0.0.1', resolve))
const browser = spawn(
  process.env.CHROME_PATH || 'C:/Program Files/Google/Chrome/Application/chrome.exe',
  [
    '--headless=new',
    '--remote-debugging-port=9239',
    `--user-data-dir=${resolve('.cache/view-browser-profile')}`,
    '--no-first-run',
    '--no-default-browser-check',
    'about:blank',
  ],
  { windowsHide: true, stdio: 'ignore' },
)
let launchError
browser.on('error', (error) => {
  launchError = error
})
const delay = (duration) => new Promise((resolve) => setTimeout(resolve, duration))
let socket
try {
  let target
  for (let attempt = 0; attempt < 100; attempt++) {
    if (launchError) throw launchError
    try {
      target = (
        await (
          await fetch('http://127.0.0.1:9239/json', { signal: AbortSignal.timeout(1000) })
        ).json()
      ).find((item) => item.type === 'page')
      if (target) break
    } catch {}
    await delay(100)
  }
  assert.ok(target, 'browser debugging endpoint unavailable')
  socket = new WebSocket(target.webSocketDebuggerUrl)
  await new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error('Browser WebSocket open timed out')), 10000)
    socket.addEventListener(
      'open',
      () => {
        clearTimeout(timer)
        resolve()
      },
      { once: true },
    )
    socket.addEventListener(
      'error',
      () => {
        clearTimeout(timer)
        reject(new Error('Browser WebSocket failed'))
      },
      { once: true },
    )
    socket.addEventListener(
      'close',
      () => {
        clearTimeout(timer)
        reject(new Error('Browser WebSocket closed before opening'))
      },
      { once: true },
    )
  })
  let sequence = 0
  const pending = new Map()
  socket.addEventListener('message', (event) => {
    const message = JSON.parse(event.data)
    if (message.id && pending.has(message.id)) {
      pending.get(message.id).resolve(message)
      pending.delete(message.id)
    }
  })
  const rejectPending = () => {
    for (const operation of pending.values())
      operation.reject(new Error('Browser connection closed'))
    pending.clear()
  }
  socket.addEventListener('close', rejectPending)
  socket.addEventListener('error', rejectPending)
  async function command(method, params = {}) {
    const id = ++sequence
    const result = await new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        pending.delete(id)
        reject(new Error(`Browser command timed out: ${method}`))
      }, 10000)
      const finish = (callback) => (value) => {
        clearTimeout(timer)
        pending.delete(id)
        callback(value)
      }
      pending.set(id, { resolve: finish(resolve), reject: finish(reject) })
      try {
        socket.send(JSON.stringify({ id, method, params }))
      } catch (error) {
        pending.get(id).reject(error)
      }
    })
    assert.equal(result.error, undefined)
    return result.result
  }
  async function evaluate(expression) {
    const result = await command('Runtime.evaluate', { expression, returnByValue: true })
    assert.equal(result.exceptionDetails, undefined)
    return result.result.value
  }
  async function wait(expression) {
    for (let attempt = 0; attempt < 100; attempt++) {
      if (await evaluate(expression)) return
      await delay(100)
    }
    throw new Error('Timed out: ' + expression)
  }
  await command('Page.enable')
  const ready = `document.querySelector('.pagination') && !document.querySelector('.loader')`
  async function reloadPage() {
    await evaluate('window.__reloadMarker = true')
    await command('Page.reload')
    await wait(`window.__reloadMarker !== true && document.readyState === 'complete'`)
  }
  async function checkJumpLayout(name) {
    assert.equal(
      await evaluate(`getComputedStyle(document.querySelector('.page-jump label')).flexDirection`),
      'row',
    )
    assert.equal(
      await evaluate(
        `(() => { const input = document.querySelector('.page-jump input').getBoundingClientRect(); const button = document.querySelector('.page-jump button').getBoundingClientRect(); return input.height === button.height && Math.abs(input.y - button.y) < 1; })()`,
      ),
      true,
    )
    const clip = await evaluate(
      `(() => { const rect = document.querySelector('.pagination').getBoundingClientRect(); return { x: rect.x + scrollX, y: rect.y + scrollY, width: rect.width, height: rect.height, scale: 1 }; })()`,
    )
    const screenshot = await command('Page.captureScreenshot', {
      format: 'png',
      captureBeyondViewport: true,
      clip,
    })
    await writeFile(
      resolve('.cache/pagination-' + name + '.png'),
      Buffer.from(screenshot.data, 'base64'),
    )
  }
  await command('Page.navigate', {
    url: 'http://127.0.0.1:5189/?user_id=111&from=2026-09-01&to=2026-09-30&category=%D0%B5%D0%B4%D0%B0&offset=20',
  })
  await wait(ready)
  const snapshot = `({ values: Array.from(document.querySelectorAll('.filters select,.filters input')).map(element=>element.value), page: document.querySelector('.pagination').textContent, search: location.search })`
  const before = await evaluate(snapshot)
  assert.equal(
    await evaluate(
      `document.documentElement.dataset.theme === (localStorage.getItem('family-budget-theme') === 'dark' ? 'dark' : 'light')`,
    ),
    true,
  )
  if (process.env.EXPECT_SAVED_THEME) {
    assert.equal(
      await evaluate(`document.documentElement.dataset.theme`),
      process.env.EXPECT_SAVED_THEME,
    )
  }
  async function chooseTheme(theme) {
    await evaluate(
      `(() => { const button = document.querySelector('.theme-toggle'); if (button.getAttribute('aria-checked') !== ${JSON.stringify(String(theme === 'dark'))}) button.click(); })()`,
    )
    await wait(`document.documentElement.dataset.theme === ${JSON.stringify(theme)}`)
  }
  await chooseTheme('dark')
  assert.equal(await evaluate(`localStorage.getItem('family-budget-theme')`), 'dark')
  await command('Network.enable')
  await command('Network.setBlockedURLs', { urls: ['*/assets/index-*.js'] })
  await reloadPage()
  await wait(
    `document.documentElement.dataset.theme === 'dark' && !document.querySelector('.theme-picker')`,
  )
  await command('Network.setBlockedURLs', { urls: [] })
  await reloadPage()
  await wait(ready)
  assert.equal(
    await evaluate(`document.querySelector('.theme-toggle').getAttribute('aria-checked')`),
    'true',
  )
  for (const width of [320, 390, 1440]) {
    await command('Emulation.setDeviceMetricsOverride', {
      width,
      height: 844,
      deviceScaleFactor: 1,
      mobile: width <= 700,
    })
    assert.equal(await evaluate('document.documentElement.scrollWidth <= innerWidth'), true)
    assert.equal(
      await evaluate(`getComputedStyle(document.querySelector('.expenses-panel')).backgroundColor`),
      'rgb(30, 41, 35)',
    )
    await evaluate('window.scrollTo(0, 0)')
    const screenshot = await command('Page.captureScreenshot', { format: 'png' })
    await writeFile(resolve(`.cache/dark-${width}.png`), Buffer.from(screenshot.data, 'base64'))
    await evaluate(`document.querySelector('.heading .primary').click()`)
    await wait(`!!document.querySelector('dialog[open]')`)
    assert.equal(
      await evaluate(`getComputedStyle(document.querySelector('dialog')).backgroundColor`),
      'rgb(30, 41, 35)',
    )
    assert.equal(
      await evaluate(
        `document.querySelector('dialog').scrollWidth <= document.querySelector('dialog').clientWidth`,
      ),
      true,
    )
    await evaluate(`document.querySelector('.modal-header button').click()`)
  }
  await command('Emulation.clearDeviceMetricsOverride')
  const blockedStorage = await command('Page.addScriptToEvaluateOnNewDocument', {
    source: `Storage.prototype.setItem = function() { throw new Error('Storage unavailable') }`,
  })
  await reloadPage()
  await wait(ready)
  await chooseTheme('light')
  assert.equal(await evaluate(`!!document.querySelector('.theme-notice')`), true)
  assert.equal(await evaluate(`localStorage.getItem('family-budget-theme')`), 'dark')
  await command('Page.removeScriptToEvaluateOnNewDocument', {
    identifier: blockedStorage.identifier,
  })
  await reloadPage()
  await wait(ready)
  await chooseTheme('light')
  await reloadPage()
  await wait(ready)
  assert.equal(await evaluate(`document.documentElement.dataset.theme`), 'light')
  await evaluate(
    `localStorage.setItem('family-budget-theme', 'dark'); window.dispatchEvent(new StorageEvent('storage', { key: 'family-budget-theme' }))`,
  )
  await wait(`document.querySelector('.theme-toggle').getAttribute('aria-checked') === 'true'`)
  await chooseTheme('light')
  assert.deepEqual(await evaluate(snapshot), before)
  assert.deepEqual(before.values, ['111', 'еда', '2026-09-01', '2026-09-30'])
  assert.match(before.page, /21–40/)
  await checkJumpLayout('desktop')
  requests.length = 0
  await reloadPage()
  await wait(ready)
  assert.deepEqual(await evaluate(snapshot), before)
  assert.ok(requests.some((request) => request.startsWith('/api/expenses?')))
  assert.ok(
    requests
      .filter((request) => request.startsWith('/api/expenses?'))
      .every(
        (request) => new URL(request, 'http://localhost').searchParams.get('user_id') === '111',
      ),
  )
  await evaluate(`document.querySelector('[aria-label="Страница 20"]').click()`)
  await wait(`${ready} && new URLSearchParams(location.search).get('offset')==='380'`)
  assert.equal(await evaluate(`document.querySelector('[aria-current="page"]').textContent`), '20')
  assert.equal(
    await evaluate(`document.querySelector('[aria-label="Следующая страница"]').disabled`),
    true,
  )
  assert.deepEqual((await evaluate(snapshot)).values, before.values)
  async function jump(value) {
    await evaluate(
      `(() => { const input = document.querySelector('.page-jump input'); Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(input, ${JSON.stringify(value)}); input.dispatchEvent(new Event('input', { bubbles: true })); })()`,
    )
    await evaluate(`document.querySelector('.page-jump').requestSubmit()`)
  }
  for (const invalid of ['0', '21', '1.5', '']) {
    await jump(invalid)
    assert.equal(
      await evaluate(`document.querySelector('.page-jump input').checkValidity()`),
      false,
    )
    assert.equal(await evaluate(`new URLSearchParams(location.search).get('offset')`), '380')
  }
  await jump('15')
  await wait(`${ready} && new URLSearchParams(location.search).get('offset')==='280'`)
  assert.equal(await evaluate(`document.querySelector('[aria-current="page"]').textContent`), '15')
  assert.deepEqual((await evaluate(snapshot)).values, before.values)
  await reloadPage()
  await wait(`${ready} && document.querySelector('.page-jump input').value==='15'`)
  assert.equal(await evaluate(`document.querySelector('[aria-current="page"]').textContent`), '15')
  await evaluate('history.back()')
  await wait(`${ready} && document.querySelector('.page-jump input').value==='20'`)
  await evaluate('history.forward()')
  await wait(`${ready} && document.querySelector('.page-jump input').value==='15'`)
  await jump('2')
  await wait(`${ready} && new URLSearchParams(location.search).get('offset')==='20'`)
  await evaluate(
    `Array.from(document.querySelectorAll('.member')).find(element=>element.textContent.includes('Ева')).click()`,
  )
  await wait(`${ready} && document.querySelector('.filters select').value==='222'`)
  assert.equal(await evaluate(`new URLSearchParams(location.search).has('offset')`), false)
  await evaluate('history.back()')
  await wait(
    `${ready} && document.querySelector('.filters select').value==='111' && document.querySelector('.pagination').textContent.includes('21–40')`,
  )
  await evaluate('history.forward()')
  await wait(`${ready} && document.querySelector('.filters select').value==='222'`)
  requests.length = 0
  await evaluate(`document.querySelector('.shared-expenses').click()`)
  await wait(`${ready} && document.querySelector('.filters select').value===''`)
  assert.deepEqual((await evaluate(snapshot)).values, ['', 'еда', '2026-09-01', '2026-09-30'])
  assert.equal(
    await evaluate(`document.querySelector('.shared-expenses').getAttribute('aria-pressed')`),
    'true',
  )
  assert.equal(await evaluate(`document.querySelector('.family-list .selected') === null`), true)
  assert.ok(
    requests
      .filter((request) => request.startsWith('/api/expenses?'))
      .every((request) => !new URL(request, 'http://localhost').searchParams.has('user_id')),
  )
  assert.ok(
    requests.some(
      (request) =>
        request.startsWith('/api/reports/summary?') &&
        !new URL(request, 'http://localhost').searchParams.has('user_id'),
    ),
  )
  await reloadPage()
  await wait(ready)
  assert.deepEqual((await evaluate(snapshot)).values, ['', 'еда', '2026-09-01', '2026-09-30'])
  await command('Emulation.setDeviceMetricsOverride', {
    width: 390,
    height: 844,
    deviceScaleFactor: 1,
    mobile: true,
  })
  assert.equal(
    await evaluate(`getComputedStyle(document.querySelector('.family-list')).display`),
    'flex',
  )
  assert.equal(
    await evaluate(
      `document.querySelector('.family-list').getBoundingClientRect().width <= innerWidth`,
    ),
    true,
  )
  assert.equal(
    await evaluate(
      `document.querySelector('.pagination').scrollWidth <= document.querySelector('.pagination').clientWidth`,
    ),
    true,
  )
  await checkJumpLayout('mobile')
  await evaluate(`document.querySelector('[aria-label="Страница 20"]').click()`)
  await wait(`${ready} && document.querySelector('[aria-current="page"]').textContent==='20'`)
  await evaluate(
    `Array.from(document.querySelectorAll('.member')).find(element=>element.textContent.includes('Ева')).click()`,
  )
  await wait(`${ready} && document.querySelector('.filters select').value==='222'`)
  await evaluate(`document.querySelector('.shared-expenses').click()`)
  await wait(`${ready} && document.querySelector('.filters select').value===''`)
  await command('Emulation.clearDeviceMetricsOverride')
  await evaluate(`document.querySelector('.reset-button').click()`)
  await wait(`${ready} && document.querySelector('.filters select').value===''`)
  await reloadPage()
  await wait(ready)
  assert.deepEqual((await evaluate(snapshot)).values, ['', '', '', ''])
  await evaluate(`document.querySelector('[aria-label="Следующая страница"]').click()`)
  await wait(`${ready} && new URLSearchParams(location.search).get('offset')==='20'`)
  for (const width of [320, 360, 390, 430, 768, 1024, 1440]) {
    await command('Emulation.setDeviceMetricsOverride', {
      width,
      height: 844,
      deviceScaleFactor: 1,
      mobile: width <= 700,
    })
    await evaluate('window.scrollTo(0, 0)')
    assert.equal(
      await evaluate(
        `(() => { const overview = document.querySelector('.shared-expenses'); const heading = document.querySelector('.members-caption'); const members = document.querySelector('.family-list'); return !members.contains(overview) && overview.getBoundingClientRect().bottom <= heading.getBoundingClientRect().top && heading.getBoundingClientRect().height > 0 && heading.getBoundingClientRect().bottom <= members.getBoundingClientRect().top; })()`,
      ),
      true,
      `overview separated from members at ${width}`,
    )
    assert.equal(
      await evaluate(
        `(() => { const box = document.querySelector('.brand-icon').getBoundingClientRect(); const icon = document.querySelector('.brand-icon svg').getBoundingClientRect(); return Math.abs(box.x + box.width / 2 - icon.x - icon.width / 2) < 1 && Math.abs(box.y + box.height / 2 - icon.y - icon.height / 2) < 1 && icon.width < box.width && icon.height < box.height; })()`,
      ),
      true,
      `brand icon alignment at ${width}`,
    )
    assert.equal(
      await evaluate('document.documentElement.scrollWidth <= innerWidth'),
      true,
      `page overflow at ${width}`,
    )
    assert.equal(
      await evaluate(`getComputedStyle(document.querySelector('tbody tr')).display`),
      width <= 700 ? 'grid' : 'table-row',
    )
    if (width <= 700) {
      assert.equal(
        await evaluate(
          `Array.from(document.querySelectorAll('.row-actions button, .pagination button, .filters input, .filters select')).every(element => element.getBoundingClientRect().height >= 44)`,
        ),
        true,
      )
      assert.equal(
        await evaluate(
          `Array.from(document.querySelectorAll('.filters input, .filters select, .page-jump input')).every(element => parseFloat(getComputedStyle(element).fontSize) >= 16)`,
        ),
        true,
      )
      assert.equal(
        await evaluate(
          `document.querySelector('.table-wrap').scrollWidth <= document.querySelector('.table-wrap').clientWidth`,
        ),
        true,
      )
    }
    if (width === 390 || width === 1440) {
      const screenshot = await command('Page.captureScreenshot', { format: 'png' })
      await writeFile(resolve(`.cache/layout-${width}.png`), Buffer.from(screenshot.data, 'base64'))
    }
    if (width === 390) {
      await evaluate(`document.querySelector('tbody tr').scrollIntoView({ block: 'start' })`)
      const screenshot = await command('Page.captureScreenshot', { format: 'png' })
      await writeFile(resolve('.cache/mobile-expenses.png'), Buffer.from(screenshot.data, 'base64'))
    }
    for (const trigger of [
      '.heading .primary',
      '.row-actions button',
      '.row-actions .delete-button',
    ]) {
      await evaluate(`document.querySelector(${JSON.stringify(trigger)}).click()`)
      await wait(`!!document.querySelector('dialog[open]')`)
      assert.equal(
        await evaluate(
          `(() => { const dialog = document.querySelector('dialog'); const rect = dialog.getBoundingClientRect(); return rect.x >= 0 && rect.right <= innerWidth && rect.y >= 0 && rect.bottom <= innerHeight && dialog.scrollWidth <= dialog.clientWidth; })()`,
        ),
        true,
        `dialog overflow at ${width}`,
      )
      if (width === 390 && trigger === '.heading .primary') {
        const screenshot = await command('Page.captureScreenshot', { format: 'png' })
        await writeFile(resolve('.cache/mobile-form.png'), Buffer.from(screenshot.data, 'base64'))
      }
      if (width <= 700 && trigger !== '.row-actions .delete-button') {
        assert.equal(
          await evaluate(
            `getComputedStyle(document.querySelector('.form-grid')).gridTemplateColumns.split(' ').length`,
          ),
          1,
        )
        await evaluate(
          `document.querySelector('.modal-actions button:last-child').scrollIntoView({ block: 'center' })`,
        )
        assert.equal(
          await evaluate(
            `document.querySelector('.modal-actions button:last-child').getBoundingClientRect().bottom <= innerHeight`,
          ),
          true,
        )
      }
      await evaluate(`document.querySelector('.modal-header button').click()`)
      await wait(`!document.querySelector('dialog')`)
    }
  }
  stress = true
  await evaluate(`document.querySelector('.section-title button').click()`)
  await wait(`${ready} && document.querySelector('.expense-name strong').textContent.length > 100`)
  for (const width of [320, 390, 768]) {
    await command('Emulation.setDeviceMetricsOverride', {
      width,
      height: 360,
      deviceScaleFactor: 1,
      mobile: true,
    })
    assert.equal(
      await evaluate('document.documentElement.scrollWidth <= innerWidth'),
      true,
      `long content overflow at ${width}`,
    )
    if (width <= 700) {
      assert.equal(
        await evaluate(
          `document.querySelector('.table-wrap').scrollWidth <= document.querySelector('.table-wrap').clientWidth`,
        ),
        true,
      )
    }
    await evaluate(`document.querySelector('.row-actions button').click()`)
    await wait(`!!document.querySelector('dialog[open]')`)
    assert.equal(
      await evaluate(
        `document.querySelector('dialog').getBoundingClientRect().height <= innerHeight`,
      ),
      true,
    )
    await evaluate(
      `document.querySelector('.modal-actions button:last-child').scrollIntoView({ block: 'center' })`,
    )
    assert.equal(
      await evaluate(
        `document.querySelector('.modal-actions button:last-child').getBoundingClientRect().bottom <= innerHeight`,
      ),
      true,
    )
    await evaluate(`document.querySelector('.modal-header button').click()`)
  }
  await command('Emulation.clearDeviceMetricsOverride')
  empty = true
  await evaluate(`document.querySelector('.section-title button').click()`)
  await wait(
    `${ready} && document.querySelector('.pagination').textContent.includes('0 записей') && !new URLSearchParams(location.search).has('offset')`,
  )
  assert.equal(await evaluate(`document.querySelector('.page-jump')`), null)
  assert.equal(
    await evaluate(
      `Array.from(document.querySelectorAll('.pagination button')).every(button=>button.disabled)`,
    ),
    true,
  )
  empty = false
  stress = false
  await evaluate(`document.querySelector('.section-title button').click()`)
  await wait(`${ready} && !!document.querySelector('tbody tr')`)
  await evaluate(
    `Array.from(document.querySelectorAll('.member')).find(e=>e.textContent.includes('Ева')).click()`,
  )
  await wait(`${ready} && document.querySelector('.filters select').value==='222'`)
  configDate = '2026-10-18'
  await evaluate(`document.querySelector('.heading .primary').click()`)
  await wait(`!!document.querySelector('dialog[open]')`)
  assert.equal(await evaluate(`document.querySelector('dialog select').value`), '222')
  assert.equal(
    await evaluate(`document.querySelector('dialog input[type=date]').value`),
    '2026-10-18',
  )
  await evaluate(
    `(() => {const input=document.querySelector('dialog input');Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value').set.call(input,'12.50');input.dispatchEvent(new Event('input',{bubbles:true}));})()`,
  )
  await evaluate(`document.querySelector('dialog form').requestSubmit()`)
  await wait(
    `!!document.querySelector('dialog .alert.error') && !document.querySelector('dialog .modal-actions .primary').disabled`,
  )
  assert.equal(await evaluate(`document.querySelector('dialog fieldset').disabled`), true)
  await evaluate(`document.querySelector('dialog form').requestSubmit()`)
  await wait(`${ready} && !document.querySelector('dialog')`)
  assert.equal(createKeys.length, 2)
  assert.equal(createKeys[0], createKeys[1])
  assert.match(createKeys[0], /^[0-9a-f]{32}$/)
  assert.equal(created.size, 1)
  editRecord = {
    id: '1',
    user_id: '222',
    amount: '9.00',
    category: 'еда',
    spent_at: '2026-10-16',
    comment: 'Изменено другим участником',
    version: 2,
  }
  await wait(`${ready} && !!document.querySelector('.row-actions button')`)
  await evaluate(`document.querySelector('.row-actions button').click()`)
  await wait(`!!document.querySelector('dialog[open]')`)
  await evaluate(`document.querySelector('dialog form').requestSubmit()`)
  await wait(`!!document.querySelector('dialog .alert.error')`)
  assert.equal(
    await evaluate(`document.querySelector('dialog .modal-actions .primary').disabled`),
    true,
  )
  await evaluate(
    `Array.from(document.querySelectorAll('dialog button')).find(b=>b.textContent==='Загрузить актуальную запись').click()`,
  )
  await wait(`document.querySelector('dialog').textContent.includes('Изменено другим участником')`)
  await evaluate(
    `Array.from(document.querySelectorAll('dialog button')).find(b=>b.textContent==='Оставить мой черновик для проверки').click()`,
  )
  assert.equal(await evaluate(`document.querySelector('dialog input').value`), '5.00')
  loseEditResponse = true
  await evaluate(`document.querySelector('dialog form').requestSubmit()`)
  await wait(`document.querySelector('dialog').textContent.includes('Ответ на изменение потерян')`)
  await evaluate(
    `Array.from(document.querySelectorAll('dialog button')).find(b=>b.textContent==='Загрузить актуальную запись').click()`,
  )
  await wait(`${ready} && !document.querySelector('dialog')`)
  assert.equal(editWrites, 1, 'lost PUT response must not write twice')
  console.log(
    'PASS: conflicting PUT keeps draft; lost PUT response is reconciled without another write',
  )
  await evaluate(`document.querySelector('.manage-members').click()`)
  await wait(`document.querySelectorAll('dialog .member-management-list li').length===2`)
  for (const width of [320, 1440]) {
    await command('Emulation.setDeviceMetricsOverride', {
      width,
      height: 900,
      deviceScaleFactor: 1,
      mobile: false,
    })
    assert.equal(
      await evaluate(`(() => {
      const button = document.querySelector('dialog .modal-header button').getBoundingClientRect();
      const icon = document.querySelector('dialog .modal-header button svg').getBoundingClientRect();
      return button.width === 44 && button.height === 44 && icon.width === 20 && icon.height === 20 &&
        Math.abs(button.x + button.width/2 - icon.x - icon.width/2) < 1 &&
        Math.abs(button.y + button.height/2 - icon.y - icon.height/2) < 1;
    })()`),
      true,
      `close icon centered at ${width}`,
    )
    const screenshot = await command('Page.captureScreenshot', { format: 'png' })
    await writeFile(
      resolve(`.cache/members-close-${width}.png`),
      Buffer.from(screenshot.data, 'base64'),
    )
  }
  await evaluate(`document.querySelector('dialog .member-management-list button').click()`)
  await wait(`document.querySelector('dialog').textContent.includes('безвозвратно')`)
  assert.equal(memberDeleteCalls, 0)
  await evaluate(`document.querySelector('dialog .modal-actions .secondary').click()`)
  await wait(`!!document.querySelector('dialog .member-management-list button')`)
  await evaluate(`document.querySelector('dialog .member-management-list button').click()`)
  await evaluate(`document.querySelector('dialog .modal-actions .danger').click()`)
  await wait(
    `document.querySelector('dialog').textContent.includes('Повторная регистрация заблокирована') && Array.from(document.querySelectorAll('dialog button')).some(b=>b.textContent==='Восстановить доступ')`,
  )
  assert.equal(memberDeleteCalls, 1)
  await evaluate(
    `Array.from(document.querySelectorAll('dialog button')).find(b=>b.textContent==='Восстановить доступ').click()`,
  )
  await evaluate(`document.querySelector('dialog .modal-actions .primary').click()`)
  await wait(
    `document.querySelector('dialog').textContent.includes('Доступ восстановлен') && document.querySelectorAll('dialog .member-management-list li').length===1`,
  )
  await evaluate(`document.querySelector('dialog .modal-header button').click()`)
  await wait(ready)
  console.log(
    'PASS: member removal requires confirmation; restoration does not restore old membership data',
  )
  await jump(1)
  await wait(`${ready} && !!document.querySelector('.expense-select input')`)
  await evaluate(`document.querySelector('.expense-select input').click()`)
  await wait(`document.querySelector('.selection-count')?.textContent === 'Выбрано: 1'`)
  assert.equal(await evaluate(`document.querySelector('.bulk-select input').indeterminate`), true)
  // The custom control remains a native keyboard-operable checkbox.
  await evaluate(`document.querySelectorAll('.expense-select input')[1].focus()`)
  await command('Input.dispatchKeyEvent', {
    type: 'keyDown',
    key: ' ',
    code: 'Space',
    windowsVirtualKeyCode: 32,
  })
  await command('Input.dispatchKeyEvent', {
    type: 'keyUp',
    key: ' ',
    code: 'Space',
    windowsVirtualKeyCode: 32,
  })
  await wait(`document.querySelector('.selection-count')?.textContent === 'Выбрано: 2'`)
  await jump(2)
  await wait(`${ready} && new URL(location.href).searchParams.get('offset') === '20'`)
  await evaluate(`document.querySelector('.expense-select input').click()`)
  await wait(`document.querySelector('.selection-count')?.textContent === 'Выбрано: 3'`)
  await jump(1)
  await wait(`${ready} && !new URL(location.href).searchParams.has('offset')`)
  assert.equal(
    await evaluate(`document.querySelectorAll('.expense-select input:checked').length`),
    2,
  )
  await evaluate(`document.querySelectorAll('.expense-select input')[1].click()`)
  await wait(`document.querySelector('.selection-count')?.textContent === 'Выбрано: 2'`)
  for (const theme of ['light', 'dark']) {
    await chooseTheme(theme)
    for (const width of [320, 1440]) {
      await command('Emulation.setDeviceMetricsOverride', {
        width,
        height: 900,
        deviceScaleFactor: 1,
        mobile: false,
      })
      await evaluate(`document.querySelector('.bulk-toolbar').scrollIntoView({block:'start'})`)
      assert.equal(await evaluate(`document.documentElement.scrollWidth <= innerWidth`), true)
      assert.equal(
        await evaluate(
          `(() => { const r=document.querySelector('.expense-select').getBoundingClientRect(); return r.width>=44 && r.height>=44 })()`,
        ),
        true,
      )
      const screenshot = await command('Page.captureScreenshot', { format: 'png' })
      await writeFile(
        resolve(`.cache/row-selection-${theme}-${width}.png`),
        Buffer.from(screenshot.data, 'base64'),
      )
    }
  }
  await chooseTheme('light')
  await evaluate(`document.querySelector('.bulk-toolbar .danger').click()`)
  await wait(`!!document.querySelector('dialog[open]')`)
  assert.equal(bulkDeleteCalls, 0)
  await evaluate(`document.querySelector('dialog .modal-actions .secondary').click()`)
  await wait(`!document.querySelector('dialog')`)
  assert.equal(bulkDeleteCalls, 0)
  await evaluate(`document.querySelector('.bulk-toolbar .danger').click()`)
  await wait(`!!document.querySelector('dialog[open]')`)
  await evaluate(`document.querySelector('dialog .modal-actions .danger').click()`)
  await wait(
    `${ready} && !document.querySelector('dialog') && document.querySelector('.alert.success').textContent.includes('2')`,
  )
  assert.deepEqual(bulkRequests.at(-1).body, {
    items: [
      { id: '1', version: 1 },
      { id: '21', version: 1 },
    ],
  })
  assert.equal(
    await evaluate(`document.querySelectorAll('.expense-select input:checked').length`),
    0,
  )
  bulkDeletedIDs.clear()
  await reloadPage()
  await wait(ready)
  await evaluate(`document.querySelector('.bulk-select input').click()`)
  await wait(`document.querySelectorAll('.expense-select input:checked').length === 20`)
  await evaluate(`document.querySelector('.expense-select input').click()`)
  await wait(`document.querySelector('.selection-count').textContent === 'Выбрано: 399'`)
  assert.equal(await evaluate(`document.querySelector('.bulk-select input').indeterminate`), true)
  await jump(2)
  await wait(`${ready} && new URL(location.href).searchParams.get('offset') === '20'`)
  await evaluate(`document.querySelector('.expense-select input').click()`)
  await wait(`document.querySelector('.selection-count').textContent === 'Выбрано: 398'`)
  await evaluate(`document.querySelector('.bulk-toolbar .danger').click()`)
  await wait(`!!document.querySelector('dialog[open]')`)
  await evaluate(`document.querySelector('dialog .modal-actions .danger').click()`)
  await wait(
    `${ready} && !document.querySelector('dialog') && document.querySelectorAll('.expense-select input').length === 2`,
  )
  assert.deepEqual(bulkRequests.at(-1).body, { token: 'confirmed-snapshot', exclude: ['1', '21'] })
  bulkDeletedIDs.clear()
  await reloadPage()
  await wait(ready)
  console.log(
    'PASS: row selection, keyboard control, mixed state, cross-page selection, exclusions and exact deletion payloads',
  )
  const beforeBulkAll = bulkDeleteCalls
  // A single selection covers 400 records, not just the 20 rendered rows.
  await evaluate(`document.querySelector('.bulk-select input').click()`)
  await wait(`!!document.querySelector('.bulk-toolbar .danger')`)
  assert.equal(
    await evaluate(`document.querySelector('.bulk-toolbar .danger').textContent`),
    'Удалить выбранное (400)',
  )
  await jump(2)
  await wait(`${ready} && new URL(location.href).searchParams.get('offset') === '20'`)
  assert.equal(
    await evaluate(`document.querySelector('.bulk-select input').checked`),
    true,
    'selection survives pagination',
  )
  await evaluate(`document.querySelector('.bulk-toolbar .danger').click()`)
  await wait(`!!document.querySelector('dialog[open]')`)
  assert.equal(bulkDeleteCalls, beforeBulkAll)
  await evaluate(`document.querySelector('dialog .modal-actions .secondary').click()`)
  await wait(`!document.querySelector('dialog')`)
  assert.equal(bulkDeleteCalls, beforeBulkAll, 'cancel must not delete')
  await evaluate(
    `document.querySelector('.filters select').value = '222'; document.querySelector('.filters select').dispatchEvent(new Event('change', {bubbles:true}))`,
  )
  await wait(`${ready} && document.querySelector('.filters select').value === '222'`)
  assert.equal(
    await evaluate(`document.querySelector('.bulk-select input').checked`),
    false,
    'filter changes reset selection',
  )
  for (const mode of ['conflict', 'lost', 'success']) {
    bulkMode = mode
    empty = false
    bulkDeletedIDs.clear()
    await reloadPage()
    await wait(ready)
    await evaluate(`document.querySelector('.bulk-select input').click()`)
    await wait(`!!document.querySelector('.bulk-toolbar .danger')`)
    await command('Emulation.setDeviceMetricsOverride', {
      width: 320,
      height: 900,
      deviceScaleFactor: 1,
      mobile: false,
    })
    assert.equal(
      await evaluate(`document.documentElement.scrollWidth <= innerWidth`),
      true,
      'selected toolbar fits mobile',
    )
    await evaluate(`document.querySelector('.bulk-toolbar').scrollIntoView({block:'center'})`)
    const screenshot = await command('Page.captureScreenshot', { format: 'png' })
    if (mode === 'success')
      await writeFile(
        resolve('.cache/bulk-selection-320.png'),
        Buffer.from(screenshot.data, 'base64'),
      )
    await evaluate(`document.querySelector('.bulk-toolbar .danger').click()`)
    await wait(`!!document.querySelector('dialog[open]')`)
    assert.equal(
      await evaluate(
        `document.querySelector('dialog').textContent.includes('400') && document.querySelector('dialog').textContent.includes('Ева')`,
      ),
      true,
    )
    await evaluate(`document.querySelector('dialog .modal-actions .danger').click()`)
    if (mode === 'success') {
      await wait(
        `${ready} && !document.querySelector('dialog') && document.querySelector('.alert.success').textContent.includes('400')`,
      )
    } else {
      await wait(`!!document.querySelector('dialog .alert.error')`)
      assert.equal(
        await evaluate(`!!document.querySelector('dialog .modal-actions .danger')`),
        false,
        'no blind retry',
      )
      await evaluate(`document.querySelector('dialog .modal-actions .primary').click()`)
      await wait(`${ready} && !document.querySelector('dialog')`)
      assert.equal(await evaluate(`document.querySelector('.bulk-select input').checked`), false)
    }
  }
  assert.equal(bulkDeleteCalls, beforeBulkAll + 3, 'exactly one write per confirmation')
  for (const request of bulkRequests.slice(-3)) {
    assert.equal(request.body.token, 'confirmed-snapshot')
    assert.equal(new URL(request.url).searchParams.get('user_id'), '222')
  }
  empty = false
  bulkDeletedIDs.clear()
  await reloadPage()
  await wait(ready)
  await command('Emulation.setDeviceMetricsOverride', {
    width: 1440,
    height: 900,
    deviceScaleFactor: 1,
    mobile: false,
  })
  console.log(
    'PASS: select all pages, filter reset, cancellation, bulk deletion, conflict and lost response; centered close icon',
  )
  await evaluate(`document.querySelector('.shared-expenses').click()`)
  await wait(`${ready} && document.querySelector('.filters select').value === ''`)
  await evaluate(`(() => {
    const input = document.querySelectorAll('.filters input[type=date]')[0];
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(input, '2026-01-01');
    input.dispatchEvent(new Event('input', { bubbles: true }));
  })()`)
  await wait(
    `${ready} && document.querySelector('.filters input[type=date]').value === '2026-01-01'`,
  )
  await evaluate(`(() => {
    const input = document.querySelectorAll('.filters input[type=date]')[1];
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(input, '');
    input.dispatchEvent(new Event('input', { bubbles: true }));
  })()`)
  await wait(`${ready} && document.querySelectorAll('.filters input[type=date]')[1].value === ''`)
  assert.equal(
    await evaluate(`document.querySelector('.stat-foot').textContent`),
    '01.01.2026 — без конечной даты',
  )
  const openPeriodRequest = requests
    .filter((path) => path.startsWith('/api/reports/summary?'))
    .at(-1)
  assert.equal(new URL(openPeriodRequest, 'http://localhost').searchParams.has('to'), false)
  failExpenses = true
  await evaluate(
    `Array.from(document.querySelectorAll('.member')).find(e=>e.textContent.includes('Ева')).click()`,
  )
  await wait(`!!document.querySelector('.alert.error') && !document.querySelector('.loader')`)
  assert.equal(await evaluate(`document.querySelector('.total-value').textContent`), '—')
  assert.equal(await evaluate(`document.querySelectorAll('.breakdown-item').length`), 0)
  console.log(
    'PASS: selected owner, fresh form date, safe POST retry after lost response, no stale totals on API failure',
  )
  console.log(
    'PASS: 320–1440px layouts, touch targets, dialogs, long content, pagination, URL/history, filters and empty results',
  )
  await chooseTheme('dark')
  await command('Browser.close')
} finally {
  socket?.close()
  browser.kill()
  server.closeAllConnections()
  await new Promise((resolve) => server.close(resolve))
}

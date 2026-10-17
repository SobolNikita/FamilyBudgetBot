import assert from 'node:assert/strict'
import { createServer } from 'node:http'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { spawn } from 'node:child_process'
import { resolve } from 'node:path'

const root = resolve('web/dist')
const requests = []
let empty = false
let stress = false
await mkdir(resolve('.cache'), { recursive: true })
const server = createServer(async (request, response) => {
  const url = new URL(request.url, 'http://localhost')
  if (url.pathname.startsWith('/api/')) {
    requests.push(url.pathname + url.search)
    const total = empty ? 0 : 400
    const offset = Number(url.searchParams.get('offset') || 0)
    const data = {
      '/api/config': { currency: 'BYN', timezone: 'Europe/Minsk', today: '2026-10-17' },
      '/api/categories': ['еда', 'транспорт', 'другое'],
      '/api/users': [
        { id: '111', display_name: stress ? 'Оченьдлинноеимя'.repeat(8) : 'Никита' },
        { id: '222', display_name: 'Ева' },
      ],
      '/api/expenses': {
        total,
        items: empty
          ? []
          : Array.from({ length: 20 }, (_, index) => ({
              id: String(offset + index + 1),
              user_id: url.searchParams.get('user_id') || '111',
              amount: stress ? '999999999999.99' : '5.00',
              category: 'еда',
              spent_at: '2026-10-16',
              comment: stress ? 'Длинноеописание'.repeat(30) : 'Тестовая покупка',
              version: 1,
            })),
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
      target = (await (await fetch('http://127.0.0.1:9239/json')).json()).find(
        (item) => item.type === 'page',
      )
      if (target) break
    } catch {}
    await delay(100)
  }
  assert.ok(target, 'browser debugging endpoint unavailable')
  socket = new WebSocket(target.webSocketDebuggerUrl)
  await new Promise((resolve) => socket.addEventListener('open', resolve, { once: true }))
  let sequence = 0
  const pending = new Map()
  socket.addEventListener('message', (event) => {
    const message = JSON.parse(event.data)
    if (message.id && pending.has(message.id)) {
      pending.get(message.id)(message)
      pending.delete(message.id)
    }
  })
  async function command(method, params = {}) {
    const id = ++sequence
    const result = await new Promise((resolve) => {
      pending.set(id, resolve)
      socket.send(JSON.stringify({ id, method, params }))
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
      `(() => { const select = document.querySelector('.theme-picker select'); select.value = ${JSON.stringify(theme)}; select.dispatchEvent(new Event('change', { bubbles: true })); })()`,
    )
    await wait(`document.documentElement.dataset.theme === ${JSON.stringify(theme)}`)
  }
  await chooseTheme('dark')
  assert.equal(await evaluate(`localStorage.getItem('family-budget-theme')`), 'dark')
  await command('Network.enable')
  await command('Network.setBlockedURLs', { urls: ['*/assets/*.js'] })
  await reloadPage()
  await wait(
    `document.documentElement.dataset.theme === 'dark' && !document.querySelector('.theme-picker')`,
  )
  await command('Network.setBlockedURLs', { urls: [] })
  await reloadPage()
  await wait(ready)
  assert.equal(await evaluate(`document.querySelector('.theme-picker select').value`), 'dark')
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
  await wait(`document.querySelector('.theme-picker select').value === 'dark'`)
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

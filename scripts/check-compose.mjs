import assert from 'node:assert/strict'
import { randomBytes } from 'node:crypto'
import { spawnSync } from 'node:child_process'
import { mkdir, mkdtemp, writeFile } from 'node:fs/promises'
import { resolve } from 'node:path'

// Use the real Compose definition with a fresh project, volume, image and env file.
// Never read the developer's .env or connect to an existing application database.
const root = resolve('.')
const project = `family-expenses-smoke-${randomBytes(6).toString('hex')}`
const image = `family-expenses:${project}`
await mkdir(resolve('.cache'), { recursive: true })
const directory = await mkdtemp(resolve('.cache/compose-smoke-'))
const envFile = resolve(directory, 'smoke.env')
const settings = {
  POSTGRES_DB: 'family_smoke',
  POSTGRES_USER: 'family_smoke',
  POSTGRES_PASSWORD: randomBytes(24).toString('hex'),
  APP_PORT: '127.0.0.1:0',
  APP_IMAGE: image,
  APP_ENV_FILE: envFile,
  BOT_ENABLED: 'false',
  FAMILY_CURRENCY: 'RUB',
  FAMILY_TIMEZONE: 'Europe/Moscow',
}
await writeFile(
  envFile,
  Object.entries(settings)
    .map(([key, value]) => `${key}=${value}`)
    .join('\n') + '\n',
  { mode: 0o600 },
)
const environment = { ...process.env, ...settings }
const prefix = ['compose', '--env-file', envFile, '-p', project, '-f', resolve('compose.yaml')]
function docker(args, capture = false, tolerateFailure = false) {
  const result = spawnSync('docker', args, {
    cwd: root,
    env: environment,
    encoding: 'utf8',
    stdio: capture ? 'pipe' : 'inherit',
    windowsHide: true,
    timeout: 10 * 60 * 1000,
  })
  if (!tolerateFailure && (result.error || result.status !== 0)) {
    throw new Error(
      `Docker check failed: ${result.error?.message || result.stderr || result.status}`,
    )
  }
  return result.stdout?.trim() || ''
}
const compose = (args, capture = false, tolerateFailure = false) =>
  docker([...prefix, ...args], capture, tolerateFailure)

try {
  compose(['up', '--build', '--wait', '--wait-timeout', '120'])
  const address = compose(['port', 'app', '8080'], true)
  assert.match(address, /^127\.0\.0\.1:\d+$/)
  const origin = `http://${address}`
  async function request(path, status = 200, options = {}) {
    const response = await fetch(origin + path, {
      ...options,
      headers: { 'Content-Type': 'application/json', ...options.headers },
      signal: AbortSignal.timeout(15000),
    })
    assert.equal(response.status, status, path)
    assert.ok(response.headers.get('X-Request-ID'), `request ID missing: ${path}`)
    return response
  }
  assert.equal((await (await request('/health/ready')).json()).status, 'ok')
  assert.equal((await (await request('/health/bot')).json()).status, 'disabled')
  assert.equal((await (await request('/api/config')).json()).currency, 'RUB')
  const html = await (await request('/')).text()
  const asset = html.match(/src="(\/assets\/index-[^"]+\.js)"/)?.[1]
  assert.ok(asset, 'built JavaScript missing from HTML')
  assert.ok((await (await request(asset)).text()).length > 1000)
  compose([
    'exec',
    '-T',
    'db',
    'psql',
    '-U',
    settings.POSTGRES_USER,
    '-d',
    settings.POSTGRES_DB,
    '-v',
    'ON_ERROR_STOP=1',
    '-c',
    "INSERT INTO users(id,display_name) VALUES(111,'Smoke'),(222,'Other');",
  ])
  const input = {
    user_id: '111',
    amount: '10.50',
    category: 'еда',
    spent_at: '2030-01-01',
    comment: 'smoke',
  }
  const create = () =>
    request('/api/expenses', 201, {
      method: 'POST',
      headers: { 'Idempotency-Key': 'smoke-create-000001' },
      body: JSON.stringify(input),
    })
  const created = await (await create()).json()
  const replay = await (await create()).json()
  assert.equal(replay.id, created.id)
  const list = await (await request('/api/expenses?user_id=111')).json()
  assert.equal(list.total, 1)
  const filtered = await (await request('/api/expenses?user_id=222')).json()
  assert.equal(filtered.total, 0)
  const updated = await (
    await request(`/api/expenses/${created.id}`, 200, {
      method: 'PUT',
      body: JSON.stringify({ ...input, amount: '12.50', version: created.version }),
    })
  ).json()
  assert.equal(updated.version, created.version + 1)
  await request(`/api/expenses/${created.id}`, 409, {
    method: 'PUT',
    body: JSON.stringify({ ...input, version: created.version }),
  })
  const summary = await (await request('/api/reports/summary?from=2026-01-01')).json()
  assert.equal(summary.amount, '12.50')
  const bounded = await (await request('/api/reports/summary?from=2026-01-01&to=2026-12-31')).json()
  assert.equal(bounded.count, 0)
  await request(`/api/expenses/${created.id}?version=${updated.version}`, 204, { method: 'DELETE' })
  await create()
  assert.equal((await (await request('/api/expenses')).json()).total, 0)
  compose([
    'exec',
    '-T',
    'db',
    'psql',
    '-U',
    settings.POSTGRES_USER,
    '-d',
    settings.POSTGRES_DB,
    '-v',
    'ON_ERROR_STOP=1',
    '-c',
    "INSERT INTO expenses(user_id,amount,category,spent_at) SELECT 111,1,'еда','2030-01-01' FROM generate_series(1,45); INSERT INTO expenses(user_id,amount,category,spent_at) VALUES(222,1,'другое','2030-01-01');",
  ])
  const bulkFilter =
    'user_id=111&from=2030-01-01&to=2030-01-01&category=' + encodeURIComponent('еда')
  const selection = await (
    await request(`/api/expenses/selection?${bulkFilter}&limit=20&offset=20`)
  ).json()
  assert.equal(selection.count, 45)
  const selectedPage = await (await request(`/api/expenses?${bulkFilter}`)).json()
  await request(`/api/expenses/${selectedPage.items[0].id}`, 200, {
    method: 'PUT',
    body: JSON.stringify({ ...input, version: 1, amount: '2.00' }),
  })
  await request(`/api/expenses/bulk-delete?${bulkFilter}`, 409, {
    method: 'POST',
    body: JSON.stringify({ token: selection.token }),
  })
  assert.equal((await (await request(`/api/expenses?${bulkFilter}`)).json()).total, 45)
  const freshSelection = await (await request(`/api/expenses/selection?${bulkFilter}`)).json()
  const excludedID = selectedPage.items[1].id
  const deletion = await (
    await request(`/api/expenses/bulk-delete?${bulkFilter}`, 200, {
      method: 'POST',
      body: JSON.stringify({ token: freshSelection.token, exclude: [excludedID] }),
    })
  ).json()
  assert.equal(deletion.deleted, 44)
  await request(`/api/expenses/bulk-delete?${bulkFilter}`, 409, {
    method: 'POST',
    body: JSON.stringify({ token: freshSelection.token }),
  })
  const kept = await (await request(`/api/expenses/${excludedID}`)).json()
  const other = (await (await request('/api/expenses?user_id=222')).json()).items[0]
  // Mixing a selected ID from outside the filter must roll back the whole batch.
  await request(`/api/expenses/bulk-delete?${bulkFilter}`, 409, {
    method: 'POST',
    body: JSON.stringify({
      items: [
        { id: kept.id, version: kept.version },
        { id: other.id, version: other.version },
      ],
    }),
  })
  await request(`/api/expenses/${excludedID}`)
  const manual = await (
    await request(`/api/expenses/bulk-delete?${bulkFilter}`, 200, {
      method: 'POST',
      body: JSON.stringify({ items: [{ id: kept.id, version: kept.version }] }),
    })
  ).json()
  assert.equal(manual.deleted, 1)
  const survivor = (await (await request('/api/expenses')).json()).items[0]
  assert.equal(survivor.user_id, '222')
  await request(`/api/expenses/${survivor.id}?version=${survivor.version}`, 204, {
    method: 'DELETE',
  })
  console.log('PASS: bulk deletion across pages, optimistic conflict, exact filter and safe replay')
  const secondInput = { ...input, comment: 'removed with participant' }
  await request('/api/expenses', 201, {
    method: 'POST',
    headers: { 'Idempotency-Key': 'smoke-member-00001' },
    body: JSON.stringify(secondInput),
  })
  const member = (await (await request('/api/users')).json()).find((user) => user.id === '111')
  assert.equal(member.version, 1)
  await request('/api/users/111?version=1', 204, { method: 'DELETE' })
  await request('/api/users/111?version=1', 204, { method: 'DELETE' })
  assert.equal((await (await request('/api/users')).json()).length, 1)
  assert.equal((await (await request('/api/expenses')).json()).total, 0)
  await request('/api/expenses', 410, {
    method: 'POST',
    headers: { 'Idempotency-Key': 'smoke-member-00001' },
    body: JSON.stringify(secondInput),
  })
  const removed = await (await request('/api/users/removed')).json()
  assert.equal(removed[0].id, '111')
  assert.equal(removed[0].display_name, '')
  await request('/api/users/111/restore?version=2', 204, { method: 'POST' })
  await request('/api/users/111?version=1', 409, { method: 'DELETE' })
  assert.equal((await (await request('/api/users/removed')).json()).length, 0)
  assert.equal((await (await request('/api/users')).json()).length, 1)
  await request('/api/expenses', 410, {
    method: 'POST',
    headers: { 'Idempotency-Key': 'smoke-member-00001' },
    body: JSON.stringify(secondInput),
  })
  console.log(
    'PASS: member removal, idempotent retry, data erasure, revoked POST and restoration without old data',
  )
  console.log(
    'PASS: isolated Compose build, migrations, static assets, health, CRUD, filters, conflicts and idempotent retries',
  )
} catch (error) {
  compose(['logs', '--no-color', '--tail', '80'], false, true)
  throw error
} finally {
  // Only this random, newly created project and its test image are removed.
  compose(['down', '--volumes', '--remove-orphans'])
  docker(['image', 'rm', image], false, true)
}

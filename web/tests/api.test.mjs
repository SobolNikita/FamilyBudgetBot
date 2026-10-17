import assert from 'node:assert/strict'
import { test } from 'node:test'
import { api, APIError, requestKey } from '../src/api.ts'

test('timeout also cancels a stalled response body', async () => {
  const original = globalThis.fetch
  globalThis.fetch = async (_url, options) => ({
    ok: true,
    status: 200,
    json: () =>
      new Promise((_resolve, reject) =>
        options.signal.addEventListener('abort', () => reject(new Error('aborted')), {
          once: true,
        }),
      ),
  })
  try {
    await assert.rejects(
      api('/expenses', undefined, 20),
      (error) => error instanceof APIError && error.status === 0,
    )
  } finally {
    globalThis.fetch = original
  }
})

test('request retains its key and reports conflict status', async () => {
  const original = globalThis.fetch
  const key = requestKey()
  assert.match(key, /^[0-9a-f]{32}$/)
  globalThis.fetch = async (_url, options) => {
    assert.equal(options.headers['Idempotency-Key'], key)
    return { ok: false, status: 409, json: async () => ({ error: 'conflict' }) }
  }
  try {
    await assert.rejects(
      api('/expenses', { method: 'POST', headers: { 'Idempotency-Key': key } }),
      (error) => error instanceof APIError && error.status === 409,
    )
  } finally {
    globalThis.fetch = original
  }
})

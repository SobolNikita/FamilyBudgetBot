import assert from 'node:assert/strict'
import test from 'node:test'
import { pageCount, pageNumbers, pageOffset } from '../src/paginationState.ts'

test('page count handles empty, partial and API-limited results', () => {
  assert.equal(pageCount(0), 1)
  assert.equal(pageCount(20), 1)
  assert.equal(pageCount(21), 2)
  assert.equal(pageCount(60), 3)
  assert.equal(pageCount(2000000), 50001)
})

test('short lists show all page numbers', () => {
  assert.deepEqual(pageNumbers(1, 1), [1])
  assert.deepEqual(pageNumbers(2, 3), [1, 2, 3])
  assert.deepEqual(pageNumbers(4, 7), [1, 2, 3, 4, 5, 6, 7])
})

test('long lists retain first, last and neighboring pages', () => {
  assert.deepEqual(pageNumbers(1, 20), [1, 2, 3, 4, 5, 'gap-after', 20])
  assert.deepEqual(pageNumbers(10, 20), [1, 'gap-before', 9, 10, 11, 'gap-after', 20])
  assert.deepEqual(pageNumbers(20, 20), [1, 'gap-before', 16, 17, 18, 19, 20])
  for (const count of [8, 20, 50001]) {
    for (const current of [1, 2, 4, 5, count - 4, count - 1, count]) {
      const pages = pageNumbers(current, count)
      assert.ok(pages.length <= 7)
      assert.ok(pages.includes(current))
      assert.equal(new Set(pages).size, pages.length)
      assert.ok(pages.filter(page => typeof page === 'number').every(page => page >= 1 && page <= count))
    }
  }
})

test('page selection produces the corresponding offset', () => {
  assert.equal(pageOffset('1', 20), 0)
  assert.equal(pageOffset('15', 20), 280)
  assert.equal(pageOffset('20', 20), 380)
  assert.equal(pageOffset('50001', 50001), 1000000)
})

test('invalid page selections cannot navigate', () => {
  for (const value of ['', '0', '-1', '21', '1.5', '1e1', 'NaN', 'Infinity', 'text', '9007199254740992']) {
    assert.equal(pageOffset(value, 20), null, value)
  }
  assert.equal(pageOffset('50002', 50002), null)
})

import test from 'node:test'
import assert from 'node:assert/strict'
import {
  emptySelection,
  isSelected,
  selectedCount,
  toggleExpense,
  selectionRequest,
} from '../src/selection.ts'

test('manual selection preserves IDs and original versions across pages', () => {
  let selection = toggleExpense(emptySelection(), { id: '9007199254740993', version: 2 })
  selection = toggleExpense(selection, { id: '42', version: 3 })
  assert.equal(selectedCount(selection), 2)
  assert.equal(isSelected(selection, '9007199254740993'), true)
  assert.deepEqual(selectionRequest(selection).items, [
    { id: '42', version: 3 },
    { id: '9007199254740993', version: 2 },
  ])
  selection = toggleExpense(selection, { id: '42', version: 4 })
  assert.equal(selectedCount(selection), 1)
  assert.equal(isSelected(selection, '42'), false)
})

test('select all supports exclusions and restoring an excluded row', () => {
  const all = { kind: 'all', token: 'snapshot', count: 400, excluded: [] }
  const partial = toggleExpense(all, { id: '1', version: 1 })
  assert.equal(selectedCount(partial), 399)
  assert.equal(isSelected(partial, '1'), false)
  assert.equal(isSelected(partial, '21'), true)
  assert.deepEqual(selectionRequest(partial), { token: 'snapshot', exclude: ['1'] })
  assert.deepEqual(toggleExpense(partial, { id: '1', version: 1 }), all)
  assert.deepEqual(toggleExpense({ ...all, count: 1 }, { id: '1', version: 1 }), emptySelection())
})

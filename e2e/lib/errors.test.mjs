import { strict as assert } from 'node:assert'
import { EventEmitter } from 'node:events'
import { test } from 'node:test'
import { errorRecorder } from './errors.mjs'
const url = 'http://wails.localhost/wails/runtime'
function resource(page, location = url, status = 422) {
  page.emit('console', { type: () => 'error', text: () => `Failed to load resource: the server responded with a status of ${status} (Unprocessable Entity)`, location: () => ({ url: location }), args: () => [] })
}
function refusal(page, method = 1, message = 'planned refusal') {
  page.emit('response', { status: () => 422, url: () => url, request: () => ({ postData: () => JSON.stringify({ args: { methodID: method } }) }), json: async () => ({ kind: 'RuntimeError', message }) })
}
test('only the exact planned response accounts for its one resource error', async () => {
  const page = new EventEmitter()
  const errors = errorRecorder(new Map([[1, 'Albums'], [2, 'Asset']]))
  errors.watch(page, { expected: row => row.kind === 'response' && row.method === 'Albums' && row.message === 'planned refusal' })
  refusal(page); resource(page)
  await errors.settle()
  assert.deepEqual(errors.unexpected(), [])
  refusal(page, 2); resource(page)
  resource(page, 'http://wails.localhost/media/1')
  page.emit('pageerror', new Error('Failed to load resource: custom.js'))
  await errors.settle()
  assert.equal(errors.unexpected().length, 4)
})
test('a cancelled or unrelated method cannot inherit a planned failure exemption', async () => {
  const page = new EventEmitter()
  const errors = errorRecorder(new Map([[1, 'Albums']]))
  let phase = 'fault'
  errors.watch(page, { context: () => phase, expected: row => row.context === 'fault' && row.kind === 'response' && row.message === 'planned refusal' })
  refusal(page); resource(page)
  await errors.settle()
  phase = 'retry'
  refusal(page); resource(page)
  await errors.settle()
  assert.equal(errors.unexpected().length, 2)
})
test('transport failures and nonlocal custom.js errors cannot be silently discarded', async () => {
  const page = new EventEmitter()
  const errors = errorRecorder()
  errors.watch(page)
  page.emit('requestfailed', { url: () => url, failure: () => ({ errorText: 'net::ERR_FAILED' }) })
  page.emit('console', { type: () => 'error', text: () => 'Failed to load resource: the server responded with a status of 404 (Not Found)', location: () => ({ url: 'https://example.org/wails/custom.js' }), args: () => [] })
  page.emit('console', { type: () => 'error', text: () => 'Failed to load resource: the server responded with a status of 404 (Not Found)', location: () => ({ url: 'http://wails.localhost/wails/custom.js' }), args: () => [] })
  await errors.settle()
  assert.equal(errors.unexpected().length, 2)
})

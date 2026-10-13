import assert from 'node:assert/strict'
import test from 'node:test'
import { finaliseRequest, quantile } from './thumbs-metrics.mjs'

test('A raw 304 is a backend validation even when Chromium exposes a cached 200.', () => {
  const result = finaliseRequest({
    wireStatus: 304,
    responseStatus: 200,
    disk: true,
    memoryEvent: true,
    start: 10,
    end: 10.025,
  })
  assert.equal(result.status, 304)
  assert.equal(result.source, 'network-revalidation')
  assert.ok(Math.abs(result.durationMs - 25) < 0.00001)
})

test('Unvalidated disk and memory responses stay distinct from backend responses.', () => {
  const common = { responseStatus: 200, start: 10, end: 11 }
  assert.equal(finaliseRequest({ ...common, disk: true, memoryEvent: true }).source, 'disk-cache')
  assert.equal(finaliseRequest({ ...common, memoryEvent: true }).source, 'memory-cache')
  assert.equal(finaliseRequest(common).source, 'network')
})

test('A refusal and an unfinished request do not become successful cache hits.', () => {
  const refusal = finaliseRequest({ responseStatus: 404, start: 0, end: 0.1 })
  assert.equal(refusal.status, 404)
  assert.equal(refusal.source, 'network')
  const unfinished = finaliseRequest({ start: 0 })
  assert.equal(unfinished.durationMs, null)
  assert.equal(unfinished.status, null)
})

test('Percentiles sort numerically and preserve empty samples.', () => {
  const values = [20, 1, 100, 3]
  assert.equal(quantile(values, 0.5), 20)
  assert.equal(quantile(values, 0.95), 100)
  assert.equal(quantile([], 0.5), null)
  assert.deepEqual(values, [20, 1, 100, 3])
})

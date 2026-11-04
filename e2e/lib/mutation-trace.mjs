// Observe real bridge calls, with optional baseline frontend assets and a
// held inverse request for deterministic native cancellation acceptance.
import { existsSync, readFileSync } from 'node:fs'
import { extname, join, relative, resolve } from 'node:path'
import { ROOT } from './suite.mjs'
import { sleep } from './app.mjs'

export function bindingID(service, name) {
  const source = readFileSync(join(ROOT, 'frontend', 'bindings', 'lumen', 'services', `${service}.ts`), 'utf8')
  const method = source.split(`export function ${name}(`)[1]?.split('export function ')[0]
  const id = /\$Call\.ByID\((\d+)/.exec(method ?? '')?.[1]
  if (!id) throw new Error(`missing generated binding: ${service}.${name}`)
  return Number(id)
}

const methods = new Map([
  [bindingID('libraryservice', 'ApplyBatch'), 'ApplyBatch'],
  [bindingID('libraryservice', 'SetFavourite'), 'SetFavourite'],
  [bindingID('libraryservice', 'ValidateSelection'), 'ValidateSelection'],
  [bindingID('searchservice', 'Hits'), 'Hits'],
  [bindingID('libraryservice', 'Asset'), 'Asset'],
  [bindingID('mediaservice', 'Inspect'), 'Inspect'],
  [bindingID('libraryservice', 'SetTakenAt'), 'SetTakenAt'],
  [bindingID('libraryservice', 'RestoreTakenAt'), 'RestoreTakenAt'],
  [bindingID('libraryservice', 'SetLocation'), 'SetLocation'],
  [bindingID('libraryservice', 'RestoreLocation'), 'RestoreLocation'],
])
const types = {
  '.js': 'text/javascript',
  '.css': 'text/css',
  '.svg': 'image/svg+xml',
  '.jpg': 'image/jpeg',
  '.png': 'image/png',
  '.webp': 'image/webp',
  '.woff2': 'font/woff2',
}

// Extra read methods opt into response-byte and latency measurements. Their
// arguments contain only the explicitly observed read contracts, never PINs.
export async function mutationTrace(page, baselineDir, readMethods = new Map()) {
  let calls = []
  const observed = new Map([...methods, ...readMethods])
  const extraReads = new Set(readMethods.values())
  const responseReads = new Set()
  const pending = new Map()
  let maximum = 0
  let hold = null
  let held = null
  const errors = []
  await page.setRequestInterception(true)
  async function intercept(request) {
    const path = new URL(request.url()).pathname
    if (baselineDir && (request.isNavigationRequest() || path.startsWith('/assets/'))) {
      const file = resolve(baselineDir, request.isNavigationRequest() ? 'index.html' : `.${path}`)
      if (relative(baselineDir, file).startsWith('..') || !existsSync(file))
        throw new Error(`missing baseline asset: ${path}`)
      await request.respond({
        status: 200,
        contentType: request.isNavigationRequest() ? 'text/html' : (types[extname(file)] ?? 'application/octet-stream'),
        body: readFileSync(file),
      })
      return
    }
    let body
    try {
      body = JSON.parse(request.postData() ?? 'null')
    } catch {
      /* A media request is not a bridge call. */
    }
    const name = body?.object === 0 && body.method === 0 ? observed.get(body.args?.methodID) : undefined
    if (name) {
      const args = body.args.args
      const ids = extraReads.has(name) ? [] : ['SetFavourite', 'Asset', 'Inspect'].includes(name) ? [args[0]] : name === 'ValidateSelection' ? (args[0].ids ?? []) : args[0]
      const record = {
        name,
        write: !extraReads.has(name) && !['Asset', 'Inspect', 'Hits', 'ValidateSelection'].includes(name),
        parameters: extraReads.has(name) ? args : undefined,
        ids,
        action: args[1],
        album: name === 'ApplyBatch' ? args[2] : undefined,
        bytes: Buffer.byteLength(request.postData()),
        start: performance.now(),
      }
      calls.push(record)
      pending.set(request, record)
      maximum = Math.max(maximum, [...pending.values()].filter((row) => row.write).length)
      if (hold?.(record)) {
        hold = null
        held = request
        return
      }
    }
    await request.continue()
  }
  page.on('request', (request) => {
    void intercept(request).catch((error) => {
      errors.push(String(error))
      void request.abort().catch(() => {})
    })
  })
  page.on('response', (response) => {
    const record = pending.get(response.request())
    if (!record || !extraReads.has(record.name)) return
    record.status = response.status()
    const read = response.text().then(
      (body) => {
        record.responseBytes = Buffer.byteLength(body)
        record.milliseconds = performance.now() - record.start
      },
      (error) => { record.responseError = String(error) },
    )
    responseReads.add(read)
    void read.finally(() => responseReads.delete(read))
  })
  const ended = (request) => pending.delete(request)
  page.on('requestfinished', ended)
  page.on('requestfailed', ended)
  return {
    errors,
    get calls() {
      return calls
    },
    get held() {
      return held !== null
    },
    reset() {
      calls = []
      maximum = 0
    },
    holdNext(match) {
      hold = match
    },
    holdUndo() {
      hold = (record) => record.name === 'ApplyBatch' && record.action === 'unfavourite'
    },
    async release() {
      const request = held
      held = null
      if (request) await request.continue()
    },
    async idle() {
      const until = Date.now() + 30_000
      while (pending.size && Date.now() < until) await sleep(20)
      if (pending.size) throw new Error('mutation calls did not settle')
      await Promise.all(responseReads)
    },
    summary() {
      return {
        calls: calls.length,
        writes: calls.filter((row) => row.write).length,
        reads: calls.filter((row) => ['Hits', 'Asset', 'Inspect'].includes(row.name)).length,
        admissions: calls.filter((row) => row.name === 'ValidateSelection').length,
        maxOutstandingWrites: maximum,
        maxIDs: Math.max(0, ...calls.map((row) => row.ids.length)),
        payloadBytes: calls.reduce((total, row) => total + row.bytes, 0),
        parts: calls.filter((row) => row.name === 'ApplyBatch').map((row) => row.ids.length),
      }
    },
  }
}

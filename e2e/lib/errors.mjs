// Keep browser resource failures tied to the actual failing request. A planned
// refusal must not hide another request, an uncaught error or a media failure.
export function errorRecorder(methods = new Map()) {
  const records = []
  const pending = new Set()
  function track(promise) {
    pending.add(promise)
    void promise.finally(() => pending.delete(promise))
  }
  function watch(page, { context = () => '', expected = () => false } = {}) {
    const append = row => { row.expected = expected(row); records.push(row) }
    page.on('pageerror', error => append({ kind: 'page', context: context(), message: String(error) }))
    page.on('console', message => {
      if (message.type() !== 'error') return
      const row = { kind: 'console', context: context(), url: message.location().url, message: message.text() }
      track(Promise.all(message.args().map(arg => arg.evaluate(value =>
        value && typeof value.message === 'string' ? value.message : String(value)).catch(() => message.text())))
        .then(parts => append({ ...row, message: parts.join(' ') || row.message })))
    })
    page.on('requestfailed', request => append({ kind: 'transport', context: context(), url: request.url(),
      message: request.failure()?.errorText }))
    page.on('response', response => {
      if (response.status() < 400) return
      const row = { kind: 'response', context: context(), url: response.url(), status: response.status() }
      if (new URL(row.url).pathname !== '/wails/runtime') { append(row); return }
      let request
      try { request = JSON.parse(response.request().postData() ?? 'null') } catch { /* Invalid requests remain failures. */ }
      row.method = methods.get(request?.args?.methodID) ?? request?.args?.methodName?.split('.').at(-1) ?? request?.args?.methodID
      track(response.json().then(value => append({ ...row, errorKind: value?.kind, code: value?.cause?.code,
        message: value?.message }), error => append({ ...row, message: String(error) })))
    })
  }
  function optionalScript(row) {
    if (!row.url) return false
    const url = new URL(row.url)
    return url.hostname === 'wails.localhost' && url.pathname === '/wails/custom.js' &&
      (row.kind === 'response' ? row.status === 404 : row.message === 'Failed to load resource: the server responded with a status of 404 (Not Found)')
  }
  function unexpected() {
    const allowedResources = records.filter(row => row.kind === 'response' && row.expected)
    return records.filter(row => {
      if (optionalScript(row)) return false
      // Page errors are always uncaught, even during a deliberately failed call.
      if (row.kind === 'page') return true
      if (row.expected) return false
      if (row.kind !== 'console') return true
      const status = /^Failed to load resource: the server responded with a status of (\d+) \([^)]+\)$/.exec(row.message)?.[1]
      const match = allowedResources.findIndex(response => response.context === row.context && response.url === row.url && response.status === Number(status))
      if (match < 0) return true
      allowedResources.splice(match, 1) // One refusal accounts for one browser resource message.
      return false
    }).map(row => JSON.stringify(row))
  }
  return { watch, records, unexpected, async settle() { while (pending.size) await Promise.all([...pending]) } }
}

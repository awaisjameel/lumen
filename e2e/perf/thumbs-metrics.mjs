// Chromium can expose a cached 200 alongside the 304 received during validation.
export function finaliseRequest(row) {
  const status = row.wireStatus ?? row.responseStatus ?? null
  const source =
    status === 304 ? 'network-revalidation' : row.disk ? 'disk-cache' : row.memoryEvent ? 'memory-cache' : 'network'
  return { ...row, status, source, durationMs: row.end === undefined ? null : (row.end - row.start) * 1000 }
}

export function quantile(values, p) {
  const sorted = [...values].sort((a, b) => a - b)
  return sorted.length ? sorted[Math.min(sorted.length - 1, Math.floor(sorted.length * p))] : null
}

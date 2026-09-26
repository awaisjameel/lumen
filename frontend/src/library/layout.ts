// Pure layout maths for the virtualized grid. The whole library's layout is
// built from per-day counts alone, so the scroll height is right before a
// single item is loaded, and scrolling to 2014 is instant.

import type { DayBucket } from '../bridge'

const HEADER_H = 48
export const GAP = 4

export type Row =
  | { kind: 'header'; day: string; count: number }
  | { kind: 'cells'; day: string; start: number; n: number } // items [start, start+n) of that day

export interface Layout {
  rows: Row[]
  cols: number
  cell: number // square cell size in px
  offsets: number[] // top of each row
  total: number
  rowOfDay: Map<string, number> // day -> header row index
}

export function rowHeight(row: Row, cell: number): number {
  return row.kind === 'header' ? HEADER_H : cell + GAP
}

/** Lay out day groups in square cells that fill the width exactly. */
export function buildLayout(buckets: DayBucket[], width: number, target: number): Layout {
  const cols = Math.max(1, Math.floor((width + GAP) / (target + GAP)))
  const cell = Math.max(40, Math.floor((width - GAP * (cols - 1)) / cols))
  const rows: Row[] = []
  const offsets: number[] = []
  const rowOfDay = new Map<string, number>()
  let y = 0
  const push = (r: Row) => {
    rows.push(r)
    offsets.push(y)
    y += rowHeight(r, cell)
  }
  for (const b of buckets) {
    rowOfDay.set(b.day, rows.length)
    push({ kind: 'header', day: b.day, count: b.count })
    for (let start = 0; start < b.count; start += cols) {
      push({ kind: 'cells', day: b.day, start, n: Math.min(cols, b.count - start) })
    }
  }
  return { rows, cols, cell, offsets, total: y, rowOfDay }
}

/** Index of the row containing y (binary search over offsets). */
export function rowAt(layout: Layout, y: number): number {
  const o = layout.offsets
  let lo = 0
  let hi = o.length - 1
  while (lo < hi) {
    const mid = (lo + hi + 1) >> 1
    if (o[mid] <= y) lo = mid
    else hi = mid - 1
  }
  return lo
}

/** Row index for item i of a day. */
export function rowOfItem(layout: Layout, day: string, i: number): number {
  const h = layout.rowOfDay.get(day)
  return h === undefined ? -1 : h + 1 + Math.floor(i / layout.cols)
}

export interface Pos {
  day: string
  i: number
}

/** Keyboard movement: left/right walk the timeline, up/down keep the column. */
export function move(layout: Layout, from: Pos, dir: 'left' | 'right' | 'up' | 'down'): Pos | null {
  const r = rowOfItem(layout, from.day, from.i)
  if (r < 0) return null
  const row = layout.rows[r] as Extract<Row, { kind: 'cells' }>
  const col = from.i - row.start

  if (dir === 'left' || dir === 'right') {
    const step = dir === 'right' ? 1 : -1
    const i = from.i + step
    const headerRow = layout.rowOfDay.get(from.day)!
    const count = (layout.rows[headerRow] as Extract<Row, { kind: 'header' }>).count
    if (i >= 0 && i < count) return { day: from.day, i }
    // Cross into the neighbouring day.
    for (let k = r + step; k >= 0 && k < layout.rows.length; k += step) {
      const nr = layout.rows[k]
      if (nr.kind === 'cells') return { day: nr.day, i: step > 0 ? nr.start : nr.start + nr.n - 1 }
    }
    return null
  }

  const step = dir === 'down' ? 1 : -1
  for (let k = r + step; k >= 0 && k < layout.rows.length; k += step) {
    const nr = layout.rows[k]
    if (nr.kind === 'cells') return { day: nr.day, i: nr.start + Math.min(col, nr.n - 1) }
  }
  return null
}

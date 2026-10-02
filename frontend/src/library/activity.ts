// What indexing is doing now and what it does next, read from one progress
// event: the sidebar's status line and the list behind it.
import type { Progress } from '../bridge'

/** A piece of background work, in the order the pipeline runs them. */
export type TaskKind = 'scan' | 'metadata' | 'thumbnail' | 'embedding' | 'faces' | 'videos' | 'ocr'

export interface Task {
  kind: TaskKind
  /** Files still to do, or for a scan, found so far. */
  count: number
  /** At work right now; otherwise waiting its turn. */
  now: boolean
}

/** The work running or waiting, in pipeline order: none when up to date. */
export function tasks(p: Progress): Task[] {
  const w = p.working
  const list: Task[] = []
  if (p.scanning) list.push({ kind: 'scan', count: p.scanSeen, now: true }) // scans go on while paused
  const stages: [TaskKind, number, boolean][] = [
    ['metadata', p.metadata, w.metadata],
    ['thumbnail', p.thumbnail, w.thumbnail],
    ['embedding', p.embedding, w.embedding],
    ['faces', p.faces, w.faces],
    ['videos', p.videos, w.videos],
    ['ocr', p.ocr, w.ocr],
  ]
  for (const [kind, count, now] of stages) {
    if (count > 0 || now) list.push({ kind, count, now })
  }
  return list
}

/** The AI's work, which waits for mains power on battery. */
export function isAI(kind: TaskKind): boolean {
  return kind === 'embedding' || kind === 'faces' || kind === 'videos' || kind === 'ocr'
}

/**
 * The task the status line names: the first at work, else the first waiting
 * (for its stage's model to retry, or between batches).
 */
export function lead(list: Task[]): Task | undefined {
  return list.find((t) => t.now) ?? list[0]
}

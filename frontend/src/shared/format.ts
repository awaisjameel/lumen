import { LOCALE, t } from './i18n'

// Dates from the engine are "wall-clock" values: the time as it read on the
// camera, encoded as UTC. So everything here formats in UTC, which shows the
// original clock time no matter which time zone this PC is in.
const UTC = 'UTC'

const dayFmt = new Intl.DateTimeFormat(LOCALE, { weekday: 'long', day: 'numeric', month: 'long', timeZone: UTC })
const monthSpanFmt = new Intl.DateTimeFormat(LOCALE, { month: 'short', year: 'numeric', timeZone: UTC })
const daySpanFmt = new Intl.DateTimeFormat(LOCALE, { day: 'numeric', month: 'short', year: 'numeric', timeZone: UTC })
const dateFmt = new Intl.DateTimeFormat(LOCALE, { day: 'numeric', month: 'long', year: 'numeric', timeZone: UTC })
const monthFmt = new Intl.DateTimeFormat(LOCALE, { month: 'long', year: 'numeric', timeZone: UTC })
const fullFmt = new Intl.DateTimeFormat(LOCALE, { dateStyle: 'full', timeStyle: 'short', timeZone: UTC })

/** "YYYY-MM-DD" -> Date at midnight UTC. */
function parseDay(day: string): Date {
  return new Date(`${day}T00:00:00Z`)
}

/** Local calendar day as YYYY-MM-DD (for "today" comparisons). */
function localDay(d: Date): string {
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
}

/** Header label for a day in the grid: Today, Yesterday, "Saturday 14 May", "Saturday 14 May 2022". */
export function dayLabel(day: string): string {
  const now = new Date()
  if (day === localDay(now)) return t('day.today')
  // The calendar day before, not 24 hours before: a day can be 23 or 25 hours long.
  if (day === localDay(new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1))) return t('day.yesterday')
  const d = parseDay(day)
  // The year is appended rather than asked of Intl, which adds a comma
  // after the weekday once a year is in the pattern.
  return d.getUTCFullYear() === now.getFullYear() ? dayFmt.format(d) : `${dayFmt.format(d)} ${d.getUTCFullYear()}`
}

/** The days a collection spans, by month ("Jan 2021 – Oct 2026", "Jun – Oct 2026"),
 * or by day for an event ("3–4 Oct 2026"). Each is a "YYYY-MM-DD" day. */
export function daySpan(first: string, last: string, byDay = false): string {
  return (byDay ? daySpanFmt : monthSpanFmt).formatRange(parseDay(first), parseDay(last))
}

/** The span of a collection's days, newest first as the timeline lists them; "" without days. */
export function spanOfDays(days: readonly string[], byDay = false): string {
  const dates = days.filter(Boolean)
  const first = dates.at(-1)
  const last = dates[0]
  return first && last ? daySpan(first, last, byDay) : ''
}

/** A day as a date, without its weekday: "15 October 2026". */
export function dateLabel(day: string): string {
  return dateFmt.format(parseDay(day))
}

export function monthLabel(day: string): string {
  return monthFmt.format(parseDay(day))
}

/** Full date and time for the details panel, from wall-clock seconds. */
export function dateTime(wallClockSeconds: number): string {
  return fullFmt.format(new Date(wallClockSeconds * 1000))
}

// Sizes in the locale's words for units ("3.2 MB"), one decimal below ten.
const sizeUnits = ['kilobyte', 'megabyte', 'gigabyte', 'terabyte'] as const
const sizeFmts = sizeUnits.map((unit) => [
  new Intl.NumberFormat(LOCALE, { style: 'unit', unit, unitDisplay: 'short', maximumFractionDigits: 1 }),
  new Intl.NumberFormat(LOCALE, { style: 'unit', unit, unitDisplay: 'short', maximumFractionDigits: 0 }),
])

/** A file size, e.g. "3.2 MB" (powers of 1024, as Windows shows them). */
export function bytes(n: number): string {
  if (n < 1024) return t('format.bytes', { n }) // Intl would say "512 byte"
  let v = n / 1024
  let i = 0
  while (v >= 1024 && i < sizeUnits.length - 1) {
    v /= 1024
    i++
  }
  const [fine, whole] = sizeFmts[i] ?? []
  return (v < 10 ? fine : whole)?.format(v) ?? t('format.bytes', { n })
}

const megapixelFmt = new Intl.NumberFormat(LOCALE, { minimumFractionDigits: 1, maximumFractionDigits: 1 })

export function megapixels(w: number, h: number): string {
  return t('format.megapixels', { mp: megapixelFmt.format((w * h) / 1e6) })
}

/** Video time in milliseconds as m:ss or h:mm:ss. */
export function duration(ms: number): string {
  const seconds = Math.floor(Math.max(0, ms) / 1000)
  const s = String(seconds % 60).padStart(2, '0')
  const m = String(Math.floor(seconds / 60) % 60).padStart(2, '0')
  const h = Math.floor(seconds / 3600)
  return h ? `${h}:${m}:${s}` : `${Math.floor(seconds / 60)}:${s}`
}

/** The last part of a Windows or POSIX path: a file's name, or a folder's ("D:" for a drive). */
export function fileName(path: string): string {
  return path.split(/[\\/]/).filter(Boolean).pop() ?? path
}

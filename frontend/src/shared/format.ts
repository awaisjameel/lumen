import { LOCALE, t } from './i18n'

// Dates from the engine are "wall-clock" values: the time as it read on the
// camera, encoded as UTC. So everything here formats in UTC, which shows the
// original clock time no matter which time zone this PC is in.
const UTC = 'UTC'

const dayFmt = new Intl.DateTimeFormat(LOCALE, { weekday: 'short', day: 'numeric', month: 'short', timeZone: UTC })
const dayYearFmt = new Intl.DateTimeFormat(LOCALE, {
  weekday: 'short',
  day: 'numeric',
  month: 'short',
  year: 'numeric',
  timeZone: UTC,
})
const monthFmt = new Intl.DateTimeFormat(LOCALE, { month: 'long', year: 'numeric', timeZone: UTC })
const fullFmt = new Intl.DateTimeFormat(LOCALE, { dateStyle: 'full', timeStyle: 'short', timeZone: UTC })

/** "YYYY-MM-DD" -> Date at midnight UTC. */
function parseDay(day: string): Date {
  return new Date(day + 'T00:00:00Z')
}

/** Local calendar day as YYYY-MM-DD (for "today" comparisons). */
function localDay(d: Date): string {
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
}

/** Header label for a day in the grid: Today, Yesterday, "Sat 14 May", "Sat 14 May 2022". */
export function dayLabel(day: string): string {
  const now = new Date()
  if (day === localDay(now)) return t('day.today')
  if (day === localDay(new Date(now.getTime() - 86_400_000))) return t('day.yesterday')
  const d = parseDay(day)
  return d.getUTCFullYear() === now.getFullYear() ? dayFmt.format(d) : dayYearFmt.format(d)
}

export function monthLabel(day: string): string {
  return monthFmt.format(parseDay(day))
}

/** Full date and time for the details panel, from wall-clock seconds. */
export function dateTime(wallClockSeconds: number): string {
  return fullFmt.format(new Date(wallClockSeconds * 1000))
}

export function bytes(n: number): string {
  if (n < 1024) return `${n} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  let v = n / 1024
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(v < 10 ? 1 : 0)} ${units[i]}`
}

export function megapixels(w: number, h: number): string {
  return `${((w * h) / 1e6).toFixed(1)} MP`
}

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ErrorCode, logUIError } from '../bridge'
import type * as Bridge from '../bridge'
import { ERROR_NOTIFICATION, errorText, logError, reportUnexpected } from './errors'
import { dismiss } from './notify'
import { recordNotifications } from './testing'
import type { RecordedNotifications } from './testing'

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  logUIError: vi.fn<(e: Bridge.UIError) => void>(),
}))

/** A failed engine call, as the Wails runtime rejects it. */
function failure(message: string, code?: string): Error {
  return new Error(message, code ? { cause: { code } } : undefined)
}

it('explains failures the engine names in the UI’s own words', () => {
  expect(errorText(failure('someone already has that name', ErrorCode.PersonExists))).toBe(
    'Someone already has that name.',
  )
  expect(errorText(failure('invalid album: start date follows end date', ErrorCode.AlbumDateOrder))).toBe(
    'The start date must not follow the end date.',
  )
  expect(errorText(failure('private engine details', ErrorCode.InvalidAlbum))).toBe('This album can’t be saved.')
  expect(errorText(failure('a name must be 1 to 80 characters', ErrorCode.InvalidName))).toBe(
    'A name must be 1 to 80 characters.',
  )
})

it('shows the message of other failures, and of codes it does not know', () => {
  expect(errorText(failure('disk full'))).toBe('Something went wrong: disk full')
  expect(errorText(failure('odd', 'not_a_code'))).toBe('Something went wrong: odd')
  expect(errorText('plain')).toBe('Something went wrong: plain')
})

describe('failures reach Lumen’s log', () => {
  let notified: RecordedNotifications
  beforeEach(() => {
    notified = recordNotifications()
    vi.useFakeTimers()
    vi.mocked(logUIError).mockClear()
    vi.spyOn(console, 'error').mockImplementation(() => undefined)
  })
  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
    notified.stop()
  })

  it('logs a failure once a minute at most, however often it repeats', () => {
    for (let i = 0; i < 5; i++) logError(new Error('day failed to load'))
    expect(logUIError).toHaveBeenCalledExactlyOnceWith({
      message: 'Error: day failed to load',
      stack: expect.stringContaining('day failed to load') as unknown,
      where: 'logError',
    })
    vi.advanceTimersByTime(60_000)
    logError(new Error('day failed to load'))
    expect(logUIError).toHaveBeenCalledTimes(2)
  })

  it('names the engine’s code, and logs what isn’t an Error as text', () => {
    logError(failure('someone already has that name', ErrorCode.PersonExists))
    logError('plain')
    expect(vi.mocked(logUIError).mock.calls.map(([e]) => e.message)).toEqual([
      'Error: someone already has that name (person_exists)',
      'plain',
    ])
  })

  it('says something went wrong when a bug surfaces, unless asked only to log it', () => {
    reportUnexpected(new TypeError('x is undefined'), 'vue: render function')
    expect(notified.find(ERROR_NOTIFICATION)).toMatchObject({
      kind: 'error',
      title: 'Something went wrong. Lumen saved the details in its log.',
    })
    expect(logUIError).toHaveBeenCalledWith(expect.objectContaining({ where: 'vue: render function' }))
    dismiss(ERROR_NOTIFICATION)
    reportUnexpected('ResizeObserver loop completed with undelivered notifications', 'window', false)
    expect(notified.shown).toEqual([])
    expect(logUIError).toHaveBeenCalledTimes(2)
  })

  it('ignores a read the UI cancelled', async () => {
    const { CancelError } = await vi.importActual<{ CancelError: new (message: string) => Error }>('@wailsio/runtime')
    reportUnexpected(new CancelError('superseded'), 'promise')
    expect(logUIError).not.toHaveBeenCalled()
    expect(notified.shown).toEqual([])
  })
})

import { expect, it } from 'vitest'
import { ErrorCode } from '../bridge'
import { errorText } from './errors'

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

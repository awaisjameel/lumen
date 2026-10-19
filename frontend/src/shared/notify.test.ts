import { afterEach, expect, it, vi } from 'vitest'
import { dismiss, notify, setPresenter } from './notify'
import type { Notification, Presenter } from './notify'

function presenter() {
  const show = vi.fn<Presenter['show']>()
  const dismissed = vi.fn<Presenter['dismiss']>()
  return { show, dismissed, presenter: { show, dismiss: dismissed } satisfies Presenter }
}

const failure: Notification = { id: 'error', kind: 'error', title: 'It failed' }

afterEach(() => setPresenter(null))

it('keeps what is notified before the shell mounts and shows it once it does', () => {
  notify(failure)
  notify({ ...failure, title: 'It failed again' }) // the same id: only the newest waits
  notify({ id: 'other', kind: 'info', title: 'By the way' })
  const p = presenter()
  setPresenter(p.presenter)
  expect(p.show.mock.calls.map(([n]) => n.title)).toEqual(['It failed again', 'By the way'])
  // Shown once: a presenter registered later does not get them again.
  const later = presenter()
  setPresenter(later.presenter)
  expect(later.show).not.toHaveBeenCalled()
})

it('shows a notification at once while the shell is mounted, and takes it down when dismissed', () => {
  const p = presenter()
  setPresenter(p.presenter)
  notify(failure)
  expect(p.show).toHaveBeenCalledExactlyOnceWith(failure)
  dismiss('error')
  expect(p.dismissed).toHaveBeenCalledExactlyOnceWith('error')
})

it('drops a waiting notification that is dismissed before the shell mounts', () => {
  notify(failure)
  dismiss('error')
  const p = presenter()
  setPresenter(p.presenter)
  expect(p.show).not.toHaveBeenCalled()
})

it('calls the closed callback exactly once when a queued notice is dismissed', () => {
  const closed = vi.fn()
  notify({ ...failure, id: 'queued', closed })
  dismiss('queued')
  dismiss('queued')
  expect(closed).toHaveBeenCalledOnce()
})

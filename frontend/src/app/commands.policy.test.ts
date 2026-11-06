import type * as Errors from '../shared/errors'
import { ErrorCode } from '../bridge'
import { afterEach, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import { defineComponent, effectScope, h, ref } from 'vue'
import UButton from '@nuxt/ui/components/Button.vue'
import NavDropdownMenu from '../shared/navigation/NavDropdownMenu.vue'
import CommandPalette from './CommandPalette.vue'
import SelectionDock from '../shared/SelectionDock.vue'
import {
  commandById,
  itemCommand,
  useItemCommands,
  dispatchKey,
  menuItems,
  registerCommands,
  runCommand,
  sameKeys,
  setShortcutOverrides,
} from '../shared/commands'
import type { Command } from '../shared/commands'
import { reportError, reportUnexpected } from '../shared/errors'
import { deferred, mountInApp } from '../shared/testing'

vi.mock('../shared/errors', async (original) => ({
  ...(await original<typeof Errors>()),
  reportError: vi.fn(),
  reportUnexpected: vi.fn(),
}))

const remove: (() => void)[] = []
let wrapper: VueWrapper | undefined
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  for (const dispose of remove.splice(0)) dispose()
  setShortcutOverrides({})
  vi.clearAllMocks()
})

it('distinguishes an invalid runtime identity from an absent contextual command', async () => {
  expect(await runCommand('media.open')).toBe(false)
  await expect(Reflect.apply(runCommand, undefined, ['media.favourtie'])).rejects.toThrow('Invalid command identity')
})

it('keeps a newer registration when two owners reuse the same binding object', () => {
  const binding: Command = { id: 'media.open', title: () => 'Open', run: vi.fn() }
  const first = registerCommands([binding])
  const second = registerCommands([binding])
  remove.push(first, second)
  first()
  expect(commandById('media.open')).toBeDefined()
})

it('compares imported symbol chords by the same Shift rule as keyboard dispatch', () => {
  expect(sameKeys('Shift+?', '?')).toBe(true)
  expect(sameKeys('Ctrl+Shift+?', 'Ctrl+?')).toBe(true)
  expect(sameKeys('Shift+A', 'A')).toBe(false)
})

it('admits a non-repeatable pending action only once without blocking another command', async () => {
  const waiting = deferred<undefined>()
  const save = vi.fn(() => waiting.promise)
  const other = vi.fn()
  remove.push(
    registerCommands([
      { id: 'about.report', title: () => 'Save report', run: save },
      { id: 'app.shortcuts', title: () => 'Shortcut help', run: other },
    ]),
  )
  const first = runCommand('about.report')
  const duplicate = runCommand('about.report')
  const unrelated = runCommand('app.shortcuts')
  waiting.resolve(undefined)
  await Promise.allSettled([first, duplicate, unrelated])
  expect(save).toHaveBeenCalledOnce()
  expect(other).toHaveBeenCalledOnce()
})

it.each(['button', 'menu', 'palette', 'keyboard'] as const)(
  'reports the same rejected action through the runner from a %s',
  async (surface) => {
    const failure = new Error('Deliberate command rejection')
    const run = vi.fn(() => {
      const rejected = Promise.reject(failure)
      // Observe a raw callback too, so the pre-repair keyboard bypass does
      // not leave the fixture itself with an unobserved root promise.
      void rejected.catch(() => undefined)
      return rejected
    })
    const command: Command = {
      id: 'viewer.rotate',
      title: () => 'Policy action',
      run,
    }
    remove.push(registerCommands([command]))
    setShortcutOverrides({ 'viewer.rotate': 'Ctrl+Shift+R' })
    const open = ref(true)
    const Host = defineComponent({
      setup: () => () =>
        surface === 'palette'
          ? h(CommandPalette, {
              open: open.value,
              'onUpdate:open': (value: boolean) => {
                open.value = value
              },
            })
          : surface === 'menu'
            ? h(
                NavDropdownMenu,
                { items: menuItems([command]) },
                {
                  default: () => h(UButton, { 'data-policy-menu': '' }, () => 'Command menu'),
                },
              )
            : h(SelectionDock, { count: 1, summary: 'One selected', actions: [command] }),
    })
    wrapper = mountInApp(Host)
    const globalFailures: unknown[] = []
    wrapper.vm.$.appContext.app.config.errorHandler = (error) => {
      globalFailures.push(error)
    }
    await flushPromises()
    if (surface === 'keyboard') {
      expect(
        dispatchKey(
          new KeyboardEvent('keydown', {
            key: 'R',
            ctrlKey: true,
            shiftKey: true,
            cancelable: true,
          }),
          'viewer',
        ),
      ).toBe(true)
    } else if (surface === 'palette') {
      const option = [...document.querySelectorAll<HTMLElement>('[role="option"]')].find((element) =>
        element.textContent.includes('Policy action'),
      )
      if (!option) throw new Error('The policy command is missing from the palette')
      option.click()
    } else if (surface === 'menu') {
      document.querySelector<HTMLButtonElement>('[data-policy-menu]')?.click()
      await flushPromises()
      const item = [...document.querySelectorAll<HTMLElement>('[role="menuitem"]')].find((element) =>
        element.textContent.includes('Policy action'),
      )
      if (!item) throw new Error('The policy command is missing from its menu')
      item.click()
    } else {
      const button = document.querySelector<HTMLButtonElement>('[data-command="viewer.rotate"]')
      if (!button) throw new Error('The policy command button is missing')
      button.click()
    }
    await vi.waitFor(() => expect(run).toHaveBeenCalledOnce())
    await flushPromises()
    expect({
      surface,
      central: vi.mocked(reportUnexpected).mock.calls.length,
      local: vi.mocked(reportError).mock.calls.length,
      global: globalFailures.length,
    }).toEqual({ surface, central: 1, local: 0, global: 0 })
    expect(reportUnexpected).toHaveBeenCalledWith(failure, 'command:viewer.rotate')
  },
)

it('does not let an old completion unlock a replacement registration', async () => {
  const old = deferred<undefined>(),
    fresh = deferred<undefined>()
  const run = vi.fn(() => fresh.promise)
  const retire = registerCommands([{ id: 'about.report', run: () => old.promise }])
  remove.push(retire)
  const first = runCommand('about.report')
  remove.push(registerCommands([{ id: 'about.report', run }]))
  const second = runCommand('about.report')
  retire()
  old.resolve(undefined)
  await first
  expect(await runCommand('about.report')).toBe(false)
  expect(commandById('about.report')?.enabled()).toBe(false)
  expect(run).toHaveBeenCalledOnce()
  fresh.resolve(undefined)
  await second
  expect(commandById('about.report')?.enabled()).toBe(true)
})

it('admits repeatable controls while pending and consumes held keys for single actions', async () => {
  const waiting = deferred<undefined>()
  const step = vi.fn(() => waiting.promise),
    save = vi.fn()
  remove.push(
    registerCommands([
      { id: 'viewer.next', run: step },
      { id: 'about.report', run: save },
    ]),
  )
  setShortcutOverrides({ 'about.report': 'Ctrl+R' })
  for (let i = 0; i < 3; i++) {
    expect(
      dispatchKey(new KeyboardEvent('keydown', { key: 'ArrowRight', repeat: i > 0, cancelable: true }), 'viewer'),
    ).toBe(true)
    expect(
      dispatchKey(new KeyboardEvent('keydown', { key: 'r', ctrlKey: true, repeat: i > 0, cancelable: true }), 'global'),
    ).toBe(true)
  }
  expect(step).toHaveBeenCalledTimes(3)
  expect(save).toHaveBeenCalledOnce()
  waiting.resolve(undefined)
  await flushPromises()
})

it('does not clear the synchronous re-entry guard when a duplicate returns early', async () => {
  let duplicates: Promise<boolean>[] = []
  const run = vi.fn(() => {
    if (run.mock.calls.length > 1) throw new Error('A recursive command entered twice')
    duplicates = [runCommand('about.report'), runCommand('about.report')]
  })
  remove.push(registerCommands([{ id: 'about.report', run }]))
  expect(await runCommand('about.report')).toBe(true)
  expect(await Promise.all(duplicates)).toEqual([false, false])
  expect(run).toHaveBeenCalledOnce()
})

it('reports coded failures once, keeps cancellation quiet, and permits retry', async () => {
  const coded = new Error('Missing', { cause: { code: ErrorCode.NotFound } })
  const { CancelError } = await vi.importActual<{ CancelError: new (message: string) => Error }>('@wailsio/runtime')
  const cancelled = new CancelError('superseded')
  const run = vi.fn().mockRejectedValueOnce(coded).mockRejectedValueOnce(cancelled).mockResolvedValueOnce(undefined)
  remove.push(registerCommands([{ id: 'about.report', run }]))
  expect(await runCommand('about.report')).toBe(false)
  expect(reportError).toHaveBeenCalledExactlyOnceWith(coded)
  expect(await runCommand('about.report')).toBe(false)
  expect(reportError).toHaveBeenCalledOnce()
  expect(reportUnexpected).not.toHaveBeenCalled()
  expect(await runCommand('about.report')).toBe(true)
})

it('disposes replaced item callbacks synchronously before a stale menu can activate them', async () => {
  const items = ref([{ id: 1, name: 'Before' }])
  const scope = effectScope()
  const run = vi.fn()
  const menu = scope.run(() =>
    useItemCommands(
      items,
      (item) => item.id,
      (item) => [
        {
          id: itemCommand('album.edit', item.id),
          title: () => item.name,
          run: () => run(item.name),
        },
      ],
    ),
  )
  if (!menu) throw new Error('No item command scope')
  const stale = menu(1)[0]
  items.value = [{ id: 1, name: 'After' }]
  await stale?.onSelect?.(new Event('select'))
  expect(run).toHaveBeenCalledExactlyOnceWith('After')
  items.value = []
  await stale?.onSelect?.(new Event('select'))
  expect(run).toHaveBeenCalledOnce()
  scope.stop()
})

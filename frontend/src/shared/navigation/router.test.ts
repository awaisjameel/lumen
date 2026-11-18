import { expect, it } from 'vitest'
import { createMemoryHistory } from 'vue-router'
import { createNavigation } from './router'
import { destination } from './contract'
import { createTestNavigation } from './testing'

it('keeps replace out of the back stack and restores exact destinations in both directions', async () => {
  const navigation = createTestNavigation()
  await navigation.push(destination({ name: 'library' }))
  await navigation.push(destination({ name: 'people' }))
  await navigation.replace(destination({ name: 'things' }))
  async function travel(go: () => void, expected: string) {
    const arrived = new Promise<void>((resolve) => {
      const stop = navigation.afterEach(() => {
        stop()
        resolve()
      })
    })
    go()
    await arrived
    expect(navigation.fullPath.value).toBe(expected)
  }
  await travel(navigation.back, '/')
  await travel(navigation.forward, '/things')
})

it('reports a failed lazy view without exposing incoming queries or the underlying private error', async () => {
  const failures: string[] = []
  const navigation = createNavigation({
    history: createMemoryHistory(),
    development: false,
    view: (name) =>
      name === 'people' ? () => Promise.reject(new Error('private diagnostic')) : { render: () => null },
    failure: (error) => failures.push(error.message),
  })
  await navigation.push(destination({ name: 'library' }))
  expect(await navigation.push(destination({ name: 'people' }))).toBe(false)
  expect(navigation.fullPath.value).toBe('/')
  expect(failures).toEqual(['Application navigation failed'])
})

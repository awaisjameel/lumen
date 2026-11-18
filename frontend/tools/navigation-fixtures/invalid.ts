import { destination } from '../../src/shared/navigation/contract'
import type { Navigation } from '../../src/shared/navigation/router'

export function rejected(navigation: Navigation) {
  // expect-rejection
  destination({ name: 'missing' })
  // expect-rejection
  destination({ name: 'person' })
  // expect-rejection
  destination({ name: 'person', params: { id: '1' } })
  // expect-rejection
  destination({ name: 'person', params: { id: 1, other: 2 } })
  // expect-rejection
  destination({ name: 'library', params: { id: 1 } })
  // expect-rejection
  destination({ name: 'settings', query: { section: 'privcy' } })
  // expect-rejection
  destination({ name: 'settings', query: { section: 'about' } })
  // expect-rejection
  destination({ name: 'person', params: { id: 1 }, query: { picture: '1' } })
  // expect-rejection
  destination({ name: 'search', query: { media: 'audio' } })
  // expect-rejection
  destination({ name: 'blocked', query: { section: 'privacy' } })
  const params = { id: 1, surplus: 2 }
  // expect-rejection
  destination({ name: 'person', params })
  const query = { section: 'privacy', surplus: true } as const
  // expect-rejection
  destination({ name: 'settings', query })
  // expect-rejection
  void navigation.push('/blocked')
  // expect-rejection
  void navigation.replace({ name: 'library' })
}

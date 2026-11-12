import { createMemoryHistory } from 'vue-router'
import { createNavigation } from './router'
import type { NavigationView } from './router'
import type { ViewName } from './contract'

/** Production definitions, isolated history and explicit optional view bindings. */
export function createTestNavigation(views: Partial<Record<ViewName, NavigationView>> = {}, development = false) {
  return createNavigation({
    history: createMemoryHistory(),
    development,
    view: (name) => views[name] ?? { render: () => null },
    failure(error) {
      throw error
    },
  })
}

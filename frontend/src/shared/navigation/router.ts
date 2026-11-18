import { computed, inject } from 'vue'
import type { App, Component, InjectionKey } from 'vue'
import { createRouter } from 'vue-router'
import type { RouterHistory, RouteRecordRaw } from 'vue-router'
import { decode, destination, InvalidNavigation, routes } from './contract'
import type { Destination, Location, ViewName } from './contract'

declare module 'vue-router' {
  interface RouteMeta {
    navigation?: Location
  }
}

export type NavigationView = Component | (() => Promise<{ default: Component }>)
interface Options {
  history: RouterHistory
  development: boolean
  view: (name: ViewName) => NavigationView
  /** Report only this fixed message, never the rejected URL or query. */
  failure: (error: Error) => void
}
const navigationBrand = Symbol('navigation plugin')

function serialise(target: Destination) {
  const query: Record<string, string> = {}
  for (const [key, value] of Object.entries(target.query)) {
    if (value !== undefined) query[key] = String(value)
  }
  return { name: target.name, params: target.params, query }
}

/** Registration and all raw Router access stay inside this boundary. */
export function createNavigation(options: Options) {
  const records: RouteRecordRaw[] = routes
    .filter((entry) => options.development || !('development' in entry.meta))
    .map((entry) => ({ path: entry.path, name: entry.name, component: options.view(entry.meta.view) }))
  records.push({ path: '/:rest(.*)*', component: { render: () => null } })
  const router = createRouter({ history: options.history, routes: records })
  const fail = () => options.failure(new Error('Application navigation failed'))
  router.onError(fail)
  router.beforeEach((to) => {
    if (to.name === undefined) return serialise(destination({ name: 'library' }))
    to.meta.navigation = decode(to.name, to.params, to.query, options.development)
    if (to.hash) to.meta.navigation = { kind: 'invalid' }
    if (to.meta.navigation.kind === 'valid') {
      const canonical = serialise(to.meta.navigation.target)
      if (
        Object.entries(canonical.query).some(([key, value]) => to.query[key] !== value) ||
        Object.keys(to.query).length !== Object.keys(canonical.query).length ||
        Object.entries(canonical.params).some(([key, value]) => to.params[key] !== String(value))
      ) {
        return { ...canonical, replace: true }
      }
    }
  })
  const location = computed<Location>(() => router.currentRoute.value.meta.navigation ?? { kind: 'pending' })
  function raw(target: Destination) {
    const checked = decode(target.name, target.params, target.query, options.development)
    if (checked.kind !== 'valid') throw new InvalidNavigation()
    const result = serialise(checked.target)
    if (router.resolve(result).matched.at(-1)?.name !== target.name) throw new InvalidNavigation()
    return result
  }
  const api = {
    [navigationBrand]: true,
    location,
    /** Includes invalid incoming locations, so the drawer still closes. Never log it. */
    fullPath: computed(() => router.currentRoute.value.fullPath),
    async push(target: Destination) {
      const next = raw(target)
      try {
        return !(await router.push(next))
      } catch {
        return false
      }
    },
    async replace(target: Destination) {
      const next = raw(target)
      try {
        return !(await router.replace(next))
      } catch {
        return false
      }
    },
    resolve(target: Destination) {
      const resolved = router.resolve(raw(target))
      return { href: resolved.href, name: resolved.name, matched: resolved.matched.map((record) => record.name) }
    },
    back: () => router.back(),
    forward: () => router.forward(),
    ready: () => router.isReady(),
    afterEach: (fn: (hadPrevious: boolean) => void) =>
      router.afterEach((_to, from, failure) => {
        if (!failure) fn(from.matched.length > 0)
      }),
    install(app: App) {
      app.use(router)
      app.provide(navigationKey, api)
    },
    /** Explicit raw-input boundary for startup/native URLs and negative tests. */
    async visit(incoming: string) {
      await router.push(incoming)
    },
  }
  return api
}
export type Navigation = ReturnType<typeof createNavigation>
type NavigationAPI = Omit<Navigation, 'visit'>
const navigationKey: InjectionKey<NavigationAPI> = Symbol('application navigation')
export function isNavigationPlugin(value: unknown): value is NavigationAPI {
  return typeof value === 'object' && value !== null && navigationBrand in value
}
export function useNavigation() {
  const navigation = inject(navigationKey)
  if (!navigation) throw new Error('Application navigation provider is missing')
  return navigation
}
/** Consumers narrow by identity before reading destination-specific data. */
export function useLocation() {
  const navigation = useNavigation()
  return computed(() => (navigation.location.value.kind === 'valid' ? navigation.location.value.target : undefined))
}
export function useDestination<const N extends Destination['name']>(name: N) {
  const location = useLocation()
  return computed(() => {
    const target = location.value
    return target && hasName(target, name) ? target : undefined
  })
}
function hasName<N extends Destination['name']>(
  target: Destination,
  name: N,
): target is Extract<Destination, { name: N }> {
  return target.name === name
}

import { afterAll } from 'vitest'

// @wailsio/runtime, once imported, polls the window every 50 ms until its
// drag handlers are set up, which happens on the first tick in a desktop
// browser. A test file that finishes sooner tears jsdom down first, and that
// tick then throws "window is not defined", failing the run at random.
// Waiting a little longer than one tick before each file ends lets it run
// while the window still exists: timers fire in deadline order.
afterAll(() => new Promise<void>((resolve) => setTimeout(resolve, 60)))

// jsdom has no pointer capture and no scrolling into view, which Nuxt UI's
// select and menus call when they open: there is no pointer to capture and
// nothing to scroll in a test.
const missing = (name: string) => !(name in Element.prototype)
if (missing('hasPointerCapture')) {
  Object.assign(Element.prototype, {
    hasPointerCapture: () => false,
    setPointerCapture: () => undefined,
    releasePointerCapture: () => undefined,
  })
}
if (missing('scrollIntoView')) Object.assign(Element.prototype, { scrollIntoView: () => undefined })

// jsdom lays nothing out, so it has no IntersectionObserver either; an open
// select watches its list with one. In a test nothing ever intersects.
if (typeof window.IntersectionObserver !== 'function') {
  window.IntersectionObserver = class implements IntersectionObserver {
    readonly root = null
    readonly rootMargin = '0px'
    readonly scrollMargin = '0px'
    readonly thresholds: readonly number[] = [0]
    observe(): void {
      /* nothing to watch */
    }
    unobserve(): void {
      /* nothing to watch */
    }
    disconnect(): void {
      /* nothing to watch */
    }
    takeRecords(): IntersectionObserverEntry[] {
      return []
    }
  }
}

// Nor a ResizeObserver, which sliders and scroll areas measure themselves
// with. A test that needs sizes reported installs its own.
if (typeof window.ResizeObserver !== 'function') {
  window.ResizeObserver = class implements ResizeObserver {
    observe(): void {
      /* nothing is laid out */
    }
    unobserve(): void {
      /* nothing is laid out */
    }
    disconnect(): void {
      /* nothing is laid out */
    }
  }
}

// jsdom has no matchMedia, which the theme follows Windows with
// (design/appearance.ts): tests see a light system that never changes.
if (typeof window.matchMedia !== 'function') {
  window.matchMedia = (query: string): MediaQueryList => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => undefined,
    removeListener: () => undefined,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
    dispatchEvent: () => false,
  })
}

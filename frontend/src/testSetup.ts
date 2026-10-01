import { afterAll } from 'vitest'

// @wailsio/runtime, once imported, polls the window every 50 ms until its
// drag handlers are set up, which happens on the first tick in a desktop
// browser. A test file that finishes sooner tears jsdom down first, and that
// tick then throws "window is not defined", failing the run at random.
// Waiting a little longer than one tick before each file ends lets it run
// while the window still exists: timers fire in deadline order.
afterAll(() => new Promise<void>((resolve) => setTimeout(resolve, 60)))

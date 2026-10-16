import { expect, it } from 'vitest'
import { constrain, fitScale, zoomAt } from './viewport'

const viewport = { width: 1000, height: 600, imageWidth: 2000, imageHeight: 1200, scale: 0.5, x: 0, y: 0 }

it('fits source pixels and keeps the image centred when smaller than the viewport', () => {
  expect(fitScale(viewport)).toBe(0.5)
  expect(constrain({ ...viewport, x: 200, y: -50 })).toEqual(viewport)
})
it('keeps the source point beneath the zoom pointer fixed', () => {
  const next = zoomAt(viewport, 1, 750, 300)
  expect(next.x).toBe(-250)
  expect(next.y).toBe(0)
  expect((750 - 500 - next.x) / next.scale).toBe((750 - 500) / viewport.scale)
})
it('constrains the pan after a viewport resize', () => {
  expect(constrain({ ...viewport, width: 1900, height: 1100, scale: 1, x: 500, y: -500 })).toMatchObject({
    x: 50,
    y: -50,
  })
})

it('bounds repeated zooming at the existing minimum and eight times source size', () => {
  expect(zoomAt(viewport, 100).scale).toBe(8)
  expect(zoomAt(viewport, 0).scale).toBe(0.1)
  const small = { ...viewport, width: 100, height: 60 }
  expect(zoomAt(small, 0).scale).toBe(fitScale(small))
})

import { expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { DateSource, engine, Kind } from '../bridge'
import type * as Bridge from '../bridge'
import type { AssetDetail, RenderInfo } from '../bridge'
import ImageSurface from './ImageSurface.vue'

vi.mock('../bridge', async (original) => ({
  ...(await original<typeof Bridge>()),
  engine: { render: vi.fn() },
}))
vi.mock('../shared/observeResize', () => ({ observeResize: () => () => undefined }))

function deferred<T>() {
  let resolve: (value: T) => void = () => {
    throw new Error('Promise is not initialised')
  }
  const promise = new Promise<T>((done) => {
    resolve = done
  })
  return { promise, resolve }
}
function detail(id: number): AssetDetail {
  return {
    id,
    kind: Kind.KindPhoto,
    takenAt: 0,
    dateFrom: DateSource.DateFromFile,
    width: 6000,
    height: 4000,
    camera: '',
    lat: null,
    lon: null,
    placeId: 0,
    favourite: false,
    paths: [],
    size: 1000,
    ext: '.tiff',
    video: null,
    caps: { preview: true, original: false, render: true, directPlay: false, convertPlay: false },
  }
}
const render: RenderInfo = {
  width: 4096,
  height: 2731,
  limited: true,
  revision: 'new',
}

it('cancels navigation and refuses a late render from the previous asset', async () => {
  const first = deferred<RenderInfo>(),
    second = deferred<RenderInfo>()
  const signals: AbortSignal[] = []
  vi.mocked(engine.render).mockImplementation((id, signal) => {
    if (signal) signals.push(signal)
    return id === 1 ? first.promise : second.promise
  })
  const wrapper = mount(ImageSurface, { props: { id: 1, thumb: 1, width: 6000, height: 4000, detail: detail(1) } })
  try {
    await wrapper.setProps({ id: 2, detail: detail(2) })
    expect(signals[0]?.aborted).toBe(true)
    second.resolve(render)
    await flushPromises()
    first.resolve({ ...render, revision: 'old' })
    await flushPromises()
    expect(wrapper.get('.original').attributes('src')).toContain('/render/2/4096?v=new')
    expect(wrapper.text()).toContain('4,096')
  } finally {
    wrapper.unmount()
  }
  expect(signals[1]?.aborted).toBe(true)
})

it('keeps an already loaded original visible after presentation metadata refreshes', async () => {
  const photo = { ...detail(1), width: 600, height: 400, caps: { ...detail(1).caps, original: true } }
  const wrapper = mount(ImageSurface, { props: { id: 1, thumb: 1, width: 600, height: 400, detail: photo } })
  try {
    await wrapper.get('img.original').trigger('load')
    expect(wrapper.get('img.original').isVisible()).toBe(true)
    await wrapper.setProps({ thumb: 2, detail: { ...photo, adjustment: { rotation: 1, flip: false } } })
    expect(wrapper.get('img.original').isVisible()).toBe(true)
    expect(wrapper.get('img.original').attributes('style')).toContain('rotate(90deg)')
    expect(wrapper.get('img.preview').isVisible()).toBe(false)
  } finally {
    wrapper.unmount()
  }
})

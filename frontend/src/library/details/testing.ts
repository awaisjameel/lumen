import { createTestNavigation } from '../../shared/navigation/testing'
import { vi } from 'vitest'
import { createPinia } from 'pinia'
import type { Pinia } from 'pinia'
import type { Component } from 'vue'
import { mountInApp } from '../../shared/testing'
import { DOMWrapper, flushPromises } from '@vue/test-utils'
import { DateSource, Kind, engine } from '../../bridge'
import type { AssetDetail, Selection } from '../../bridge'

export function asset(id = 1): AssetDetail {
  return {
    id,
    camera: '',
    kind: Kind.KindPhoto,
    takenAt: 0,
    dateFrom: DateSource.DateFromEXIF,
    width: 400,
    height: 300,
    lat: null,
    lon: null,
    placeId: 0,
    favourite: false,
    paths: ['C:/Photos/photo.jpg'],
    size: 100,
    ext: '.jpg',
    video: null,
    caps: { preview: true, original: true, render: false, paged: false, directPlay: false, convertPlay: false },
  }
}
export const body = () => new DOMWrapper(document.body)
export async function press(label: string) {
  const button = body()
    .findAll('button')
    .find((button) => button.text() === label)
  if (!button) throw new Error(`Missing ${label} action`)
  await button.trigger('click')
  await flushPromises()
}

/** Correction fixtures return the initial authoritative metadata through the real bridge facade. */
export function mountCorrection(
  component: Component,
  options: {
    props: { open: boolean; assets: AssetDetail[]; selection?: Selection | null }
    pinia?: Pinia
  },
) {
  vi.mocked(engine.asset).mockImplementation((id) => {
    const value = options.props.assets.find((asset) => asset.id === id)
    return value ? Promise.resolve(value) : Promise.reject(new Error('Unknown fixture asset'))
  })
  const router = createTestNavigation()
  return mountInApp(component, { props: options.props, plugins: [options.pinia ?? createPinia(), router] })
}

export { undo } from '../../shared/testing'

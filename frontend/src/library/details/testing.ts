import { DOMWrapper, flushPromises } from '@vue/test-utils'
import { DateSource, Kind } from '../../bridge'
import type { AssetDetail } from '../../bridge'

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

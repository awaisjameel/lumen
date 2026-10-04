import lucide from '@iconify-json/lucide/icons.json'
import { expect, it } from 'vitest'
import { bundledIcons, icons } from './icons'

it('names only icons the local collection has, so none is ever fetched', () => {
  const known = new Set([...Object.keys(lucide.icons), ...Object.keys(lucide.aliases)])
  const missing = Object.entries(icons)
    .filter(([, name]) => !name.startsWith('i-lucide-') || !known.has(name.slice('i-lucide-'.length)))
    .map(([key, name]) => `${key}: ${name}`)
  expect(missing).toEqual([])
})

it('bundles every icon of the manifest, each once', () => {
  expect(new Set(bundledIcons)).toEqual(new Set(Object.values(icons)))
  expect(bundledIcons.length).toBe(new Set(bundledIcons).size)
})

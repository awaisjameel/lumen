// The one place that talks to the Go engine. Components and stores call these
// functions, never the generated bindings directly, so engine communication
// can be understood (and mocked) from this file alone.

import { Events } from '@wailsio/runtime'
import { LibraryService } from '../../bindings/lumen/services'
import type { Settings } from '../../bindings/lumen/services'
import { DateSource, Filter, Kind } from '../../bindings/lumen/internal/core'
import type { AssetDetail, DayBucket, DayItems, GridItem, Progress, Source } from '../../bindings/lumen/internal/core'

export { DateSource, Filter, Kind }
export type { AssetDetail, DayBucket, DayItems, GridItem, Progress, Source, Settings }

// Thumbnail sizes (longest edge). Must match core.ThumbSmall / core.ThumbLarge;
// the media route is plain HTTP, so they are not part of the generated bindings.
export const THUMB_SMALL = 256
export const THUMB_LARGE = 1024
export type ThumbSize = typeof THUMB_SMALL | typeof THUMB_LARGE

export const engine = {
  sources: async (): Promise<Source[]> => (await LibraryService.Sources()) ?? [],
  suggestedFolders: async (): Promise<string[]> => (await LibraryService.SuggestedFolders()) ?? [],
  pickFolder: (): Promise<string> => LibraryService.PickFolder(),
  addSources: async (paths: string[]): Promise<Source[]> => (await LibraryService.AddSources(paths)) ?? [],
  removeSource: (id: number): Promise<void> => LibraryService.RemoveSource(id),
  rescan: (): Promise<void> => LibraryService.Rescan(),

  timeline: async (filter: Filter): Promise<DayBucket[]> => (await LibraryService.Timeline(filter)) ?? [],
  days: async (from: string, to: string, filter: Filter): Promise<DayItems[]> =>
    (await LibraryService.Days(from, to, filter)) ?? [],
  focus: (from: string, to: string): Promise<void> => LibraryService.Focus(from, to),

  asset: (id: number): Promise<AssetDetail> => LibraryService.Asset(id),
  setFavourite: (id: number, fav: boolean): Promise<void> => LibraryService.SetFavourite(id, fav),
  showInFolder: (id: number): Promise<void> => LibraryService.ShowInFolder(id),

  progress: (): Promise<Progress> => LibraryService.Progress(),
  setPaused: (paused: boolean): Promise<void> => LibraryService.SetPaused(paused),
  settings: (): Promise<Settings> => LibraryService.Settings(),
  setVideos: (on: boolean): Promise<void> => LibraryService.SetVideos(on),
}

/** Fires whenever indexing progress changes. Returns an unsubscribe function. */
export function onProgress(cb: (p: Progress) => void): () => void {
  return Events.On('index:progress', (e) => cb(e.data))
}

/**
 * Fires (at most once a second) when assets were added, removed or updated,
 * with how many assets are now visible. Returns an unsubscribe function.
 */
export function onLibraryChanged(cb: (visible: number) => void): () => void {
  return Events.On('library:changed', (e) => cb(e.data))
}

/** Thumbnail URL. The version makes the WebView re-fetch when a thumbnail is rebuilt. */
export function thumbUrl(id: number, version: number, size: ThumbSize = THUMB_SMALL): string {
  return `/media/thumb/${id}/${size}?v=${version}`
}

/** The original file, for formats the WebView can display. */
export function originalUrl(id: number): string {
  return `/media/original/${id}`
}

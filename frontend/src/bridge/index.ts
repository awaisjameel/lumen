// The one place that talks to the Go engine. Components and stores call these
// functions, never the generated bindings directly, so engine communication
// can be understood (and mocked) from this file alone.

import { Events } from '@wailsio/runtime'
import { LibraryService, SearchService } from '../../bindings/lumen/services'
import type { Settings } from '../../bindings/lumen/services'
import { DateSource, Filter, Kind, TagKind, TagSource } from '../../bindings/lumen/internal/core'
import type {
  AssetDetail,
  AssetTag,
  DayBucket,
  DayItems,
  GridItem,
  Progress,
  SearchResult,
  Source,
  Tag,
  View,
} from '../../bindings/lumen/internal/core'

export { DateSource, Filter, Kind, TagKind, TagSource }
export type { AssetDetail, AssetTag, DayBucket, DayItems, GridItem, Progress, Settings, Source, Tag, View }

/** A search's results (never null, unlike the raw binding). */
export type SearchResults = Omit<SearchResult, 'items'> & { items: GridItem[] }

// Thumbnail sizes (longest edge). Must match core.ThumbSmall / core.ThumbLarge;
// the media route is plain HTTP, so they are not part of the generated bindings.
export const THUMB_SMALL = 256
export const THUMB_LARGE = 1024
export type ThumbSize = typeof THUMB_SMALL | typeof THUMB_LARGE

// Tag confidence range. Must match core.TagConfidenceMin / Max.
export const TAG_CONFIDENCE_MIN = 0.002
export const TAG_CONFIDENCE_MAX = 0.3

export const engine = {
  sources: async (): Promise<Source[]> => (await LibraryService.Sources()) ?? [],
  suggestedFolders: async (): Promise<string[]> => (await LibraryService.SuggestedFolders()) ?? [],
  pickFolder: (): Promise<string> => LibraryService.PickFolder(),
  addSources: async (paths: string[]): Promise<Source[]> => (await LibraryService.AddSources(paths)) ?? [],
  removeSource: (id: number): Promise<void> => LibraryService.RemoveSource(id),
  rescan: (): Promise<void> => LibraryService.Rescan(),

  timeline: async (view: View): Promise<DayBucket[]> => (await LibraryService.Timeline(view)) ?? [],
  days: async (from: string, to: string, view: View): Promise<DayItems[]> =>
    (await LibraryService.Days(from, to, view)) ?? [],
  focus: (from: string, to: string): Promise<void> => LibraryService.Focus(from, to),

  asset: (id: number): Promise<AssetDetail> => LibraryService.Asset(id),
  setFavourite: (id: number, fav: boolean): Promise<void> => LibraryService.SetFavourite(id, fav),
  showInFolder: (id: number): Promise<void> => LibraryService.ShowInFolder(id),

  progress: (): Promise<Progress> => LibraryService.Progress(),
  setPaused: (paused: boolean): Promise<void> => LibraryService.SetPaused(paused),
  settings: (): Promise<Settings> => LibraryService.Settings(),
  setVideos: (on: boolean): Promise<void> => LibraryService.SetVideos(on),
  setAIOnBattery: (on: boolean): Promise<void> => LibraryService.SetAIOnBattery(on),
  setTagConfidence: (v: number): Promise<void> => LibraryService.SetTagConfidence(v),

  search: async (query: string): Promise<SearchResults> => {
    const r = await SearchService.Search(query)
    return { ...r, items: r.items ?? [] }
  },
  suggestTags: async (text: string): Promise<Tag[]> => (await SearchService.SuggestTags(text)) ?? [],
  things: async (): Promise<Tag[]> => (await SearchService.Things()) ?? [],
  createLabel: (name: string): Promise<number> => SearchService.CreateLabel(name),
  deleteTag: (id: number): Promise<void> => SearchService.DeleteTag(id),
  setTagHidden: (id: number, hidden: boolean): Promise<void> => SearchService.SetTagHidden(id, hidden),
  assetTags: async (id: number): Promise<AssetTag[]> => (await SearchService.AssetTags(id)) ?? [],
  tagAsset: (id: number, name: string): Promise<void> => SearchService.TagAsset(id, name),
  untagAsset: (id: number, tagID: number): Promise<void> => SearchService.UntagAsset(id, tagID),
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

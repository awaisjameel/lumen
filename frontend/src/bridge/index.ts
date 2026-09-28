// The one place that talks to the Go engine. Components and stores call these
// functions, never the generated bindings directly, so engine communication
// can be understood (and mocked) from this file alone.

import { Events } from '@wailsio/runtime'
import { LibraryService, PeopleService, SearchService } from '../../bindings/lumen/services'
import {
  AlbumKind,
  DateSource,
  Device,
  DuplicateKind,
  Filter,
  Kind,
  Playback,
  TagKind,
  TagSource,
} from '../../bindings/lumen/internal/core'
import type {
  Album,
  AlbumRule,
  AssetDetail as RawAssetDetail,
  AssetTag,
  DayBucket,
  DayItems as RawDayItems,
  DuplicateGroup as RawDuplicateGroup,
  DuplicateItem as RawDuplicateItem,
  Face,
  GridItem,
  Person,
  Progress,
  SearchResult,
  Settings,
  Source,
  Tag,
  View,
} from '../../bindings/lumen/internal/core'

export { AlbumKind, DateSource, Device, DuplicateKind, Filter, Kind, Playback, TagKind, TagSource }
export type { Album, AlbumRule, AssetTag, DayBucket, Face, GridItem, Person, Progress, Settings, Source, Tag, View }

// Go sends an empty list as null; the generated types say so. These are the
// same types with their lists made non-null, which this file guarantees, so
// the rest of the UI never checks.
type NonNullLists<T, K extends keyof T> = Omit<T, K> & { [P in K]-?: NonNullable<T[P]> }

/** A photo's details. */
export type AssetDetail = NonNullLists<RawAssetDetail, 'paths'>
/** One day of the grid. */
export type DayItems = NonNullLists<RawDayItems, 'items'>
/** A search's results, best first. */
export type SearchResults = NonNullLists<SearchResult, 'items' | 'people'>
/** One asset in a duplicate group, with all its file paths. */
export type DuplicateItem = NonNullLists<RawDuplicateItem, 'paths'>
/** Exact copies or similar photos to review. */
export type DuplicateGroup = Omit<RawDuplicateGroup, 'items'> & { items: DuplicateItem[] }

// Thumbnail sizes (longest edge). Must match core.ThumbSmall / core.ThumbLarge;
// the media route is plain HTTP, so they are not part of the generated bindings.
export const THUMB_SMALL = 256
export const THUMB_LARGE = 1024
export const SPRITE_COLS = 4
export const SPRITE_ROWS = 4
export type ThumbSize = typeof THUMB_SMALL | typeof THUMB_LARGE

// Tag confidence: default and range. Must match core.TagConfidenceDefault /
// Min / Max (constants are not part of the generated bindings).
export const TAG_CONFIDENCE_DEFAULT = 0.02
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
    ((await LibraryService.Days(from, to, view)) ?? []).map((d) => ({ ...d, items: d.items ?? [] })),
  focus: (from: string, to: string): Promise<void> => LibraryService.Focus(from, to),

  asset: async (id: number): Promise<AssetDetail> => {
    const d = await LibraryService.Asset(id)
    return { ...d, paths: d.paths ?? [] }
  },
  duplicateGroups: async (): Promise<DuplicateGroup[]> =>
    ((await LibraryService.DuplicateGroups()) ?? []).map((g) => ({
      ...g,
      items: (g.items ?? []).map((it) => ({ ...it, paths: it.paths ?? [] })),
    })),
  setFavourite: (id: number, fav: boolean): Promise<void> => LibraryService.SetFavourite(id, fav),
  albums: async (): Promise<Album[]> => (await LibraryService.Albums()) ?? [],
  createAlbum: (name: string, kind: AlbumKind, rule: AlbumRule): Promise<number> =>
    LibraryService.CreateAlbum(name, kind, rule),
  updateAlbum: (id: number, name: string, rule: AlbumRule): Promise<void> => LibraryService.UpdateAlbum(id, name, rule),
  deleteAlbum: (id: number): Promise<void> => LibraryService.DeleteAlbum(id),
  assetAlbums: async (id: number): Promise<number[]> => (await LibraryService.AssetAlbums(id)) ?? [],
  setAlbumItem: (album: number, asset: number, include: boolean): Promise<void> =>
    LibraryService.SetAlbumItem(album, asset, include),
  showInFolder: (id: number): Promise<void> => LibraryService.ShowInFolder(id),
  showFileInFolder: (id: number, path: string): Promise<void> => LibraryService.ShowFileInFolder(id, path),

  progress: (): Promise<Progress> => LibraryService.Progress(),
  setPaused: (paused: boolean): Promise<void> => LibraryService.SetPaused(paused),
  settings: (): Promise<Settings> => LibraryService.Settings(),
  setVideos: (on: boolean): Promise<void> => LibraryService.SetVideos(on),
  setAIOnBattery: (on: boolean): Promise<void> => LibraryService.SetAIOnBattery(on),
  setTagConfidence: (v: number): Promise<void> => LibraryService.SetTagConfidence(v),

  search: async (query: string): Promise<SearchResults> => {
    const r = await SearchService.Search(query)
    return { ...r, items: r.items ?? [], people: r.people ?? [] }
  },
  suggestTags: async (text: string): Promise<Tag[]> => (await SearchService.SuggestTags(text)) ?? [],
  things: async (): Promise<Tag[]> => (await SearchService.Things()) ?? [],
  createLabel: (name: string): Promise<number> => SearchService.CreateLabel(name),
  deleteTag: (id: number): Promise<void> => SearchService.DeleteTag(id),
  setTagHidden: (id: number, hidden: boolean): Promise<void> => SearchService.SetTagHidden(id, hidden),
  assetTags: async (id: number): Promise<AssetTag[]> => (await SearchService.AssetTags(id)) ?? [],
  tagAsset: (id: number, name: string): Promise<void> => SearchService.TagAsset(id, name),
  untagAsset: (id: number, tagID: number): Promise<void> => SearchService.UntagAsset(id, tagID),

  people: async (): Promise<Person[]> => (await PeopleService.People()) ?? [],
  person: (id: number): Promise<Person> => PeopleService.Person(id),
  personFaces: async (id: number): Promise<Face[]> => (await PeopleService.PersonFaces(id)) ?? [],
  assetFaces: async (id: number): Promise<Face[]> => (await PeopleService.AssetFaces(id)) ?? [],
  suggestPeople: async (text: string): Promise<Person[]> => (await PeopleService.SuggestPeople(text)) ?? [],
  personByName: (name: string): Promise<number> => PeopleService.PersonByName(name),
  namePerson: (id: number, name: string): Promise<void> => PeopleService.NamePerson(id, name),
  mergePeople: (from: number, into: number): Promise<void> => PeopleService.MergePeople(from, into),
  setPersonHidden: (id: number, hidden: boolean): Promise<void> => PeopleService.SetPersonHidden(id, hidden),
  setPersonCover: (id: number, face: number): Promise<void> => PeopleService.SetPersonCover(id, face),
  rejectFaces: (person: number, faces: number[]): Promise<void> => PeopleService.RejectFaces(person, faces),
  nameFaces: (faces: number[], name: string): Promise<number> => PeopleService.NameFaces(faces, name),
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

export function spriteUrl(id: number, version: number): string {
  return `/media/sprite/${id}?v=${version}`
}

export function playUrl(id: number, at = 0): string {
  return `/media/play/${id}?at=${Math.max(0, Math.round(at))}`
}

export function frameUrl(id: number, at: number): string {
  return `/media/frame/${id}/${Math.max(0, Math.round(at))}`
}

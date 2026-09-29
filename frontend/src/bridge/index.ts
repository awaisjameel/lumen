// The one place that talks to the Go engine. Components and stores call these
// functions, never the generated bindings directly (ESLint enforces it), so
// engine communication can be understood (and mocked) from this file alone.

import { Events } from '@wailsio/runtime'
import { LibraryService, MediaService, PeopleService, SearchService } from '../../bindings/lumen/services'
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
  Changes,
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
export type {
  Album,
  AlbumRule,
  AssetTag,
  Changes,
  DayBucket,
  Face,
  GridItem,
  Person,
  Progress,
  Settings,
  Source,
  Tag,
  View,
}

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

// Constants of the engine's that are not part of the generated bindings.
// services/contract_test.go checks each against its Go original.
export const THUMB_SMALL = 256 // core.ThumbSmall
export const THUMB_LARGE = 1024 // core.ThumbLarge
export const SPRITE_COLS = 4 // core.SpriteCols
export const SPRITE_ROWS = 4 // core.SpriteRows
export const TAG_CONFIDENCE_DEFAULT = 0.02 // core.TagConfidenceDefault
export const TAG_CONFIDENCE_MIN = 0.002 // core.TagConfidenceMin
export const TAG_CONFIDENCE_MAX = 0.3 // core.TagConfidenceMax
export const MAX_NAME_LEN = 80 // core.MaxNameLen: people, Things and albums
export type ThumbSize = typeof THUMB_SMALL | typeof THUMB_LARGE

/**
 * Why an engine call failed, when the UI explains it in its own words
 * (services.ErrorCode). Other failures carry only a message.
 */
export const ErrorCode = {
  NotFound: 'not_found',
  NoAI: 'no_ai',
  InvalidName: 'invalid_name',
  AlbumExists: 'album_exists',
  InvalidAlbum: 'invalid_album',
  PersonExists: 'person_exists',
  TagExists: 'tag_exists',
  BuiltinTag: 'builtin_tag',
  TagInAlbum: 'tag_in_album',
  AlreadyCovered: 'already_covered',
  FolderNotFound: 'folder_not_found',
} as const
export type ErrorCode = (typeof ErrorCode)[keyof typeof ErrorCode]

const errorCodes = new Set<string>(Object.values(ErrorCode))

/** The code of a failed engine call (see ErrorCode), if it has one. */
export function errorCode(e: unknown): ErrorCode | undefined {
  const cause: unknown = e instanceof Error ? e.cause : undefined
  if (typeof cause !== 'object' || cause === null || !('code' in cause)) return undefined
  const code = cause.code
  return typeof code === 'string' && errorCodes.has(code) ? (code as ErrorCode) : undefined
}

export const engine = {
  sources: async (): Promise<Source[]> => (await LibraryService.Sources()) ?? [],
  suggestedFolders: async (): Promise<string[]> => (await LibraryService.SuggestedFolders()) ?? [],
  /** The folder the user picked, or "" if they closed the picker. */
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
  /** Creates a manual album holding items, or a smart album (no items). */
  createAlbum: (name: string, kind: AlbumKind, rule: AlbumRule, items: number[] = []): Promise<number> =>
    LibraryService.CreateAlbum(name, kind, rule, items),
  updateAlbum: (id: number, name: string, rule: AlbumRule): Promise<void> => LibraryService.UpdateAlbum(id, name, rule),
  deleteAlbum: (id: number): Promise<void> => LibraryService.DeleteAlbum(id),
  assetAlbums: async (id: number): Promise<number[]> => (await LibraryService.AssetAlbums(id)) ?? [],
  setAlbumItem: (album: number, asset: number, include: boolean): Promise<void> =>
    LibraryService.SetAlbumItem(album, asset, include),
  /** Opens Explorer on one of an asset's files: path, or "" for the first one there. */
  showInFolder: (id: number, path = ''): Promise<void> => LibraryService.ShowInFolder(id, path),

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

// The engine's events (services.EventProgress, services.EventLibraryChanged),
// typed by the generated event declarations: a name they don't know fails
// to compile.
const EVENT_PROGRESS = 'index:progress' satisfies keyof Events.CustomEvents
const EVENT_LIBRARY_CHANGED = 'library:changed' satisfies keyof Events.CustomEvents

/** Fires whenever indexing progress changes. Returns an unsubscribe function. */
export function onProgress(cb: (p: Progress) => void): () => void {
  return Events.On(EVENT_PROGRESS, (e) => cb(e.data))
}

/**
 * Fires (at most once a second) when the library changed, saying what:
 * assets, their tags, or who is in them. Returns an unsubscribe function.
 */
export function onLibraryChanged(cb: (c: Changes) => void): () => void {
  return Events.On(EVENT_LIBRARY_CHANGED, (e) => cb(e.data))
}

/**
 * Thumbnail URL, or undefined while the asset has none (version 0). The
 * version makes the WebView fetch again when a thumbnail is rebuilt.
 */
export function thumbUrl(id: number, version: number, size: ThumbSize = THUMB_SMALL): string | undefined {
  return version ? `/media/thumb/${id}/${size}?v=${version}` : undefined
}

/** The original file, for photos the WebView can display. */
export function originalUrl(id: number): string {
  return `/media/original/${id}`
}

export function spriteUrl(id: number, version: number): string {
  return `/media/sprite/${id}?v=${version}`
}

export function frameUrl(id: number, at: number): string {
  return `/media/frame/${id}/${Math.max(0, Math.round(at))}`
}

// Videos stream from the engine's own server (MediaService.VideoBase), which
// startMedia asks for once.
let videoBase = ''

/** Learns where videos stream from. Call once at startup. */
export async function startMedia(): Promise<void> {
  videoBase = await MediaService.VideoBase()
}

/** A video file the WebView plays as it is. */
export function videoUrl(id: number): string {
  return `${videoBase}/video/${id}`
}

/** A video converted to H.264 while it plays, from at ms. */
export function playUrl(id: number, at = 0): string {
  return `${videoBase}/play/${id}?at=${Math.max(0, Math.round(at))}`
}

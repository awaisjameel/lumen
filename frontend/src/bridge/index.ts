// The one place that talks to the Go engine. Components and stores call these
// functions, never the generated bindings directly, so engine communication
// can be understood (and mocked) from this file alone.

import { Events } from '@wailsio/runtime'
import { LibraryService, PeopleService, SearchService } from '../../bindings/lumen/services'
import { DateSource, Filter, Kind, TagKind, TagSource } from '../../bindings/lumen/internal/core'
import type {
  AssetDetail as RawAssetDetail,
  AssetTag,
  DayBucket,
  DayItems as RawDayItems,
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

export { DateSource, Filter, Kind, TagKind, TagSource }
export type { AssetTag, DayBucket, Face, GridItem, Person, Progress, Settings, Source, Tag, View }

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

// Thumbnail sizes (longest edge). Must match core.ThumbSmall / core.ThumbLarge;
// the media route is plain HTTP, so they are not part of the generated bindings.
export const THUMB_SMALL = 256
export const THUMB_LARGE = 1024
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
  moveFaces: (faces: number[], person: number): Promise<void> => PeopleService.MoveFaces(faces, person),
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

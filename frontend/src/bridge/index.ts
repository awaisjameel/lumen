// The one place that talks to the Go engine. Components and stores call these
// functions, never the generated bindings directly (ESLint enforces it), so
// engine communication can be understood (and mocked) from this file alone.

import { CancelError, Events } from '@wailsio/runtime'
import type { CancellablePromise } from '@wailsio/runtime'
import { LibraryService, MediaService, PeopleService, SearchService } from '../../bindings/lumen/services'
import {
  AlbumKind,
  BlockKind,
  BlockMode,
  DateSource,
  Device,
  DuplicateKind,
  Filter,
  Kind,
  OrganiseMoveState,
  OrganiseRunState,
  Playback,
  TagKind,
  TagSource,
} from '../../bindings/lumen/internal/core'
import type {
  Album,
  Block,
  BlockChange,
  CoveredItem as RawCoveredItem,
  OrganiseMove,
  OrganisePlan as RawOrganisePlan,
  OrganiseProgress,
  OrganiseRun,
  XMPExport,
  AlbumRule,
  AssetDetail as RawAssetDetail,
  AssetTag,
  Changes,
  DayBucket,
  DayItems as RawDayItems,
  DiskDeletion as RawDiskDeletion,
  DuplicateGroup as RawDuplicateGroup,
  DuplicateItem as RawDuplicateItem,
  Event,
  Face,
  GridItem,
  Person,
  Place,
  Progress,
  SearchResult,
  Settings,
  Source,
  Tag,
  View,
} from '../../bindings/lumen/internal/core'

export {
  AlbumKind,
  BlockKind,
  BlockMode,
  DateSource,
  Device,
  DuplicateKind,
  Filter,
  Kind,
  OrganiseMoveState,
  OrganiseRunState,
  Playback,
  TagKind,
  TagSource,
}
export type {
  Album,
  AlbumRule,
  Block,
  BlockChange,
  AssetTag,
  Changes,
  DayBucket,
  Face,
  GridItem,
  Person,
  Event,
  OrganiseMove,
  OrganiseProgress,
  OrganiseRun,
  Place,
  Progress,
  Settings,
  Source,
  Tag,
  View,
  XMPExport,
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
/** One asset in a duplicate group, with all its file paths (and each one's mtime). */
export type DuplicateItem = NonNullLists<RawDuplicateItem, 'paths' | 'mtimes'>
/** Exact copies or similar photos to review. */
export type DuplicateGroup = Omit<RawDuplicateGroup, 'items'> & { items: DuplicateItem[] }
/** What moving files to the Recycle Bin did; kept files were left as they were. */
export type DiskDeletion = NonNullLists<RawDiskDeletion, 'kept'>
/** A photo a block covers, with the other blocks that also hide it. */
export type CoveredItem = NonNullLists<RawCoveredItem, 'hiddenBy'>
/** An organise preview: the moves a template would make. */
export type OrganisePlan = NonNullLists<RawOrganisePlan, 'moves'>

// Constants of the engine's that are not part of the generated bindings.
// services/contract_test.go checks each against its Go original.
export const THUMB_SMALL = 256 // core.ThumbSmall
export const THUMB_LARGE = 1024 // core.ThumbLarge
export const SPRITE_COLS = 4 // core.SpriteCols
export const SPRITE_ROWS = 4 // core.SpriteRows
export const TAG_CONFIDENCE_DEFAULT = 0.02 // core.TagConfidenceDefault
export const TAG_CONFIDENCE_MIN = 0.002 // core.TagConfidenceMin
export const TAG_CONFIDENCE_MAX = 0.3 // core.TagConfidenceMax
export const EVENT_GAP_DEFAULT = 12 // core.EventGapDefault
export const EVENT_GAP_MIN = 1 // core.EventGapMin
export const EVENT_GAP_MAX = 72 // core.EventGapMax
export const MAX_NAME_LEN = 80 // core.MaxNameLen: people, Things and albums
export const PIN_MIN_LEN = 6 // core.PINMinLen
export const BLOCK_UNLOCK_IDLE_MINUTES = 10 // services.blockUnlockIdle
export const PIN_MAX_LEN = 12 // core.PINMaxLen
export const PIN_COOLDOWN_SECONDS = 30 // services.pinCooldown: the wait after too many wrong PINs
export const THUMB_CACHE_DEFAULT_MB = 2048 // core.ThumbCacheDefaultMB
export const THUMB_CACHE_MIN_MB = 256 // core.ThumbCacheMinMB
export const THUMB_CACHE_MAX_MB = 102400 // core.ThumbCacheMaxMB
export const ORGANISE_DEFAULT_TEMPLATE = '{Year}/{Month}/{Event}/{Name}' // fileops.DefaultTemplate
/** The tokens an organise scheme may hold, in the order the editor offers them (fileops.Tokens). */
export const ORGANISE_TOKENS = ['Year', 'Month', 'MonthName', 'Day', 'Event', 'Place', 'Camera', 'Name', 'Ext'] as const
/** What a token stands for when a photo has no value for it (fileops.FallbackEvent, FallbackPlace, FallbackCamera). */
export const ORGANISE_FALLBACKS = { Event: 'Unknown event', Place: 'Unknown place', Camera: 'Unknown camera' } as const
/**
 * Why a file is left out of an organise run (fileops.Reason*): a move's error
 * is one of these, or else a message from Windows.
 */
export const OrganiseReason = {
  Missing: 'missing',
  NotVisible: 'not_visible',
  BlockedFolder: 'blocked_folder',
  Changed: 'changed',
  Taken: 'taken',
  TooLong: 'too_long',
  NotAFolder: 'not_a_folder',
  SidecarTaken: 'sidecar_taken',
} as const
export type OrganiseReason = (typeof OrganiseReason)[keyof typeof OrganiseReason]
/** The pattern attribute of a PIN field: PIN_MIN_LEN to PIN_MAX_LEN digits. */
export const PIN_PATTERN = `[0-9]{${PIN_MIN_LEN},${PIN_MAX_LEN}}`
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
  AlbumMediaKind: 'album_media_kind',
  AlbumSelection: 'album_selection',
  AlbumDate: 'album_date',
  AlbumDateOrder: 'album_date_order',
  AlbumSmartItems: 'album_smart_items',
  AlbumKind: 'album_kind',
  AlbumReference: 'album_reference',
  AlbumManualOnly: 'album_manual_only',
  PersonExists: 'person_exists',
  BlockedName: 'blocked_name',
  TagExists: 'tag_exists',
  BuiltinTag: 'builtin_tag',
  TagInAlbum: 'tag_in_album',
  AlreadyCovered: 'already_covered',
  FolderNotFound: 'folder_not_found',
  InvalidPIN: 'invalid_pin',
  PINExists: 'pin_exists',
  PINRateLimited: 'pin_rate_limited',
  BlocksLocked: 'blocks_locked',
  UndoExpired: 'undo_expired',
  InvalidBlock: 'invalid_block',
  DeleteFailed: 'delete_failed',
  XMPOff: 'xmp_off',
  InvalidTemplate: 'invalid_template',
  OrganiseBusy: 'organise_busy',
  OrganiseState: 'organise_state',
  SigninFailed: 'signin_failed',
  SigninUnavailable: 'signin_unavailable',
} as const
export type ErrorCode = (typeof ErrorCode)[keyof typeof ErrorCode]

const errorCodes: ReadonlySet<unknown> = new Set<ErrorCode>(Object.values(ErrorCode))

function isErrorCode(code: unknown): code is ErrorCode {
  return errorCodes.has(code)
}

/** The code of a failed engine call (see ErrorCode), if it has one. */
export function errorCode(e: unknown): ErrorCode | undefined {
  const cause: unknown = e instanceof Error ? e.cause : undefined
  if (typeof cause !== 'object' || cause === null || !('code' in cause)) return undefined
  return isErrorCode(cause.code) ? cause.code : undefined
}

/**
 * Whether an engine call failed because its signal aborted it: the caller
 * no longer wanted the answer, so there is nothing to report.
 */
export function isCancelled(e: unknown): boolean {
  return e instanceof CancelError
}

/** call, cancelled in the engine too (its context ends) if signal aborts. */
function cancellable<T>(call: CancellablePromise<T>, signal: AbortSignal | undefined): Promise<T> {
  return signal ? call.cancelOn(signal) : call
}

let blockSession = Promise.resolve()

function queueBlockSession(action: () => Promise<void>): Promise<void> {
  const next = blockSession.then(action)
  blockSession = next.catch(() => undefined)
  return next
}

export const engine = {
  sources: async (): Promise<Source[]> => (await LibraryService.Sources()) ?? [],
  suggestedFolders: async (): Promise<string[]> => (await LibraryService.SuggestedFolders()) ?? [],
  /** The folder the user picked, or "" if they closed the picker. */
  pickFolder: (): Promise<string> => LibraryService.PickFolder(),
  addSources: async (paths: string[]): Promise<Source[]> => (await LibraryService.AddSources(paths)) ?? [],
  removeSource: (id: number): Promise<void> => LibraryService.RemoveSource(id),
  rescan: (): Promise<void> => LibraryService.Rescan(),

  // The reads a scroll or a new search supersedes take a signal that cancels
  // them in the engine too (see isCancelled).
  timeline: async (view: View, signal?: AbortSignal): Promise<DayBucket[]> =>
    (await cancellable(LibraryService.Timeline(view), signal)) ?? [],
  days: async (from: string, to: string, view: View, signal?: AbortSignal): Promise<DayItems[]> =>
    ((await cancellable(LibraryService.Days(from, to, view), signal)) ?? []).map((d) => ({
      ...d,
      items: d.items ?? [],
    })),
  focus: (from: string, to: string): Promise<void> => LibraryService.Focus(from, to),

  asset: async (id: number): Promise<AssetDetail> => {
    const d = await LibraryService.Asset(id)
    return { ...d, paths: d.paths ?? [] }
  },
  duplicateGroups: async (): Promise<DuplicateGroup[]> =>
    ((await LibraryService.DuplicateGroups()) ?? []).map((g) => ({
      ...g,
      items: (g.items ?? []).map((it) => ({ ...it, paths: it.paths ?? [], mtimes: it.mtimes ?? [] })),
    })),
  places: async (): Promise<Place[]> => (await LibraryService.Places()) ?? [],
  place: (id: number): Promise<Place> => LibraryService.Place(id),
  events: async (): Promise<Event[]> => (await LibraryService.Events()) ?? [],
  event: (id: number): Promise<Event> => LibraryService.Event(id),
  setEventName: (id: number, name: string): Promise<void> => LibraryService.SetEventName(id, name),
  setEventPinned: (id: number, pinned: boolean): Promise<void> => LibraryService.SetEventPinned(id, pinned),
  setFavourite: (id: number, fav: boolean): Promise<void> => LibraryService.SetFavourite(id, fav),
  albums: async (): Promise<Album[]> => (await LibraryService.Albums()) ?? [],
  /** Creates a manual album holding items, or a smart album (no items). */
  createAlbum: (name: string, kind: AlbumKind, rule: AlbumRule, items: number[] = []): Promise<number> =>
    LibraryService.CreateAlbum(name, kind, rule, items),
  updateAlbum: (id: number, name: string, rule: AlbumRule): Promise<void> => LibraryService.UpdateAlbum(id, name, rule),
  deleteAlbum: (id: number): Promise<void> => LibraryService.DeleteAlbum(id),
  assetAlbums: async (id: number): Promise<number[]> => (await LibraryService.AssetAlbums(id)) ?? [],
  setAlbumItem: (assetID: number, albumID: number, include: boolean): Promise<void> =>
    LibraryService.SetAlbumItem(assetID, albumID, include),
  /** Opens Explorer on one of an asset's files: path, or "" for the first one there. */
  showInFolder: (id: number, path = ''): Promise<void> => LibraryService.ShowInFolder(id, path),

  progress: (): Promise<Progress> => LibraryService.Progress(),
  setPaused: (paused: boolean): Promise<void> => LibraryService.SetPaused(paused),
  settings: (): Promise<Settings> => LibraryService.Settings(),
  setVideos: (on: boolean): Promise<void> => LibraryService.SetVideos(on),
  setAIOnBattery: (on: boolean): Promise<void> => LibraryService.SetAIOnBattery(on),
  setTagConfidence: (v: number): Promise<void> => LibraryService.SetTagConfidence(v),
  setEventGapHours: (hours: number): Promise<void> => LibraryService.SetEventGapHours(hours),
  setThumbCacheMB: (mb: number): Promise<void> => LibraryService.SetThumbCacheMB(mb),
  /** How many CPU threads indexing may keep busy; the engine holds it to the machine's. */
  setCPUThreads: (n: number): Promise<void> => LibraryService.SetCPUThreads(n),
  thumbCacheBytes: (): Promise<number> => MediaService.ThumbCacheBytes(),
  setOCR: (on: boolean): Promise<void> => LibraryService.SetOCR(on),
  setXMP: (on: boolean): Promise<void> => LibraryService.SetXMP(on),
  /** Writes every out-of-date XMP sidecar now. */
  exportXMPNow: (): Promise<XMPExport> => LibraryService.ExportXMPNow(),

  /** Previews a template's moves (source 0: every source), saved as a planned run. */
  planOrganise: async (template: string, sourceID: number): Promise<OrganisePlan> => {
    const r = await LibraryService.PlanOrganise(template, sourceID)
    return { ...r, moves: r.moves ?? [] }
  },
  startOrganise: (runID: number): Promise<void> => LibraryService.StartOrganise(runID),
  undoOrganise: (runID: number): Promise<void> => LibraryService.UndoOrganise(runID),
  organiseRuns: async (): Promise<OrganiseRun[]> => (await LibraryService.OrganiseRuns()) ?? [],

  trashAssets: (ids: number[]): Promise<void> => LibraryService.TrashAssets(ids),
  restoreAssets: (ids: number[]): Promise<void> => LibraryService.RestoreAssets(ids),
  restoreAllTrash: (): Promise<void> => LibraryService.RestoreAllTrash(),
  trashCount: (): Promise<number> => LibraryService.TrashCount(),
  /** How many files Delete from disk would move for these trashed assets (none given: all of the Trash). */
  trashFileCount: (ids: number[]): Promise<number> => LibraryService.TrashFileCount(ids),
  /** Moves trashed assets' files to the Windows Recycle Bin. */
  deleteFromDisk: async (ids: number[]): Promise<DiskDeletion> => {
    const r = await LibraryService.DeleteFromDisk(ids)
    return { ...r, kept: r.kept ?? [] }
  },
  deleteAllTrashFromDisk: async (): Promise<DiskDeletion> => {
    const r = await LibraryService.DeleteAllTrashFromDisk()
    return { ...r, kept: r.kept ?? [] }
  },

  blockPINConfigured: (): Promise<boolean> => LibraryService.BlockPINConfigured(),
  createBlockPIN: (pin: string): Promise<void> => LibraryService.CreateBlockPIN(pin),
  /** Replaces a forgotten PIN once the user signs in to Windows; the list stays locked. */
  resetBlockPIN: (pin: string): Promise<void> => LibraryService.ResetBlockPIN(pin),
  unlockBlocks: (pin: string): Promise<void> => queueBlockSession(() => LibraryService.UnlockBlocks(pin)),
  lockBlocks: (): Promise<void> => queueBlockSession(() => LibraryService.LockBlocks()),
  blocks: async (): Promise<Block[]> => (await LibraryService.Blocks()) ?? [],
  setBlock: (kind: BlockKind, targetID: number, path: string, mode: BlockMode): Promise<BlockChange> =>
    LibraryService.SetBlock(kind, targetID, path, mode),
  /** Changes what a block hides, by the block's own id (SetBlock's target is not that for events and faces). */
  setBlockMode: (blockID: number, mode: BlockMode): Promise<void> => LibraryService.SetBlockMode(blockID, mode),
  removeBlock: (id: number): Promise<void> => LibraryService.RemoveBlock(id),
  undoBlock: (id: number): Promise<void> => LibraryService.UndoBlock(id),
  /** Blocks one face, and with lookalikes the faces like it too. */
  blockFace: (faceID: number, mode: BlockMode, lookalikes: boolean): Promise<BlockChange> =>
    LibraryService.BlockFace(faceID, mode, lookalikes),
  /** The photos a block covers (needs the PIN unlock). */
  blockedItems: async (blockID: number): Promise<CoveredItem[]> =>
    ((await LibraryService.BlockedItems(blockID)) ?? []).map((it) => ({ ...it, hiddenBy: it.hiddenBy ?? [] })),
  trashCovered: (ids: number[]): Promise<void> => LibraryService.TrashCovered(ids),
  /** Recycles the listed files of an asset, keeping keep: the extra files of exact copies. */
  deleteCopies: async (assetID: number, keep: string, recycle: string[]): Promise<DiskDeletion> => {
    const r = await LibraryService.DeleteCopies(assetID, keep, recycle)
    return { ...r, kept: r.kept ?? [] }
  },

  search: async (query: string, signal?: AbortSignal): Promise<SearchResults> => {
    const r = await cancellable(SearchService.Search(query), signal)
    return { ...r, items: r.items ?? [], people: r.people ?? [] }
  },
  visibleAssets: async (ids: number[], signal?: AbortSignal): Promise<number[]> =>
    (await cancellable(SearchService.VisibleAssets(ids), signal)) ?? [],
  suggestTags: async (text: string): Promise<Tag[]> => (await SearchService.SuggestTags(text)) ?? [],
  prepareSearch: (): Promise<void> => SearchService.PrepareSearch(),
  things: async (): Promise<Tag[]> => (await SearchService.Things()) ?? [],
  createLabel: (name: string): Promise<number> => SearchService.CreateLabel(name),
  deleteTag: (id: number): Promise<void> => SearchService.DeleteTag(id),
  renameThing: (id: number, name: string): Promise<void> => SearchService.RenameThing(id, name),
  setThingPinned: (id: number, pinned: boolean): Promise<void> => SearchService.SetThingPinned(id, pinned),
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
  setPersonCover: (id: number, face: number): Promise<void> => PeopleService.SetPersonCover(id, face),
  rejectFaces: (person: number, faces: number[]): Promise<void> => PeopleService.RejectFaces(person, faces),
  nameFaces: (faces: number[], name: string): Promise<number> => PeopleService.NameFaces(faces, name),
}

// The engine's events (services.EventProgress, EventLibraryChanged, EventOrganiseProgress),
// typed by the generated event declarations: a name they don't know fails
// to compile.
const EVENT_PROGRESS = 'index:progress' satisfies keyof Events.CustomEvents
const EVENT_LIBRARY_CHANGED = 'library:changed' satisfies keyof Events.CustomEvents
const EVENT_ORGANISE_PROGRESS = 'organise:progress' satisfies keyof Events.CustomEvents

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

/** Fires while an organise run moves or undoes files. Returns an unsubscribe function. */
export function onOrganiseProgress(cb: (p: OrganiseProgress) => void): () => void {
  return Events.On(EVENT_ORGANISE_PROGRESS, (e) => cb(e.data))
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

/** Whether videos can play: false if the engine could not start its video server. */
export function videosAvailable(): boolean {
  return videoBase !== ''
}

/** A video file the WebView plays as it is. */
export function videoUrl(id: number): string {
  return `${videoBase}/video/${id}`
}

/** A video converted to H.264 while it plays, from at ms. */
export function playUrl(id: number, at = 0): string {
  return `${videoBase}/play/${id}?at=${Math.max(0, Math.round(at))}`
}

// The one place that talks to the Go engine. Components and stores call these
// functions, never the generated bindings directly (ESLint enforces it), so
// engine communication can be understood (and mocked) from this file alone.

import { Call, CancelError, CancelledRejectionError, Events } from '@wailsio/runtime'
import type { CancellablePromise } from '@wailsio/runtime'
import {
  CompressionService,
  LibraryService,
  MediaService,
  PeopleService,
  SearchService,
  SystemService,
  UpdateService,
} from '../../bindings/lumen/services'
import {
  Accent,
  AlbumKind,
  BatchAction,
  BlockKind,
  BlockMode,
  CompressionEffort,
  CompressionMode,
  CompressionState,
  DateSource,
  Density,
  Device,
  DuplicateKind,
  ExportFormat,
  ExportState,
  FailReason,
  Family,
  Filter,
  FocusRing,
  Kind,
  Link,
  Motion,
  OrganiseMoveState,
  OrganiseRunState,
  Playback,
  PrefCategory,
  SidecarState,
  SourceAvailability,
  StartView,
  TagKind,
  TagSource,
  Theme,
  ThumbFit,
  TrackKind,
  ViewerBackground,
  ViewerOpen,
  ViewerWheel,
  WaitReason,
  UpdateChannel,
  UpdateFailure,
  UpdatePhase,
} from '../../bindings/lumen/internal/core'
import type {
  CityMatch,
  LibraryCounts,
  OriginalDate,
  OriginalLocation,
  About,
  Album,
  BatchOutcome,
  Block,
  BlockChange,
  CoveredItem as RawCoveredItem,
  OrganiseMove,
  OrganisePlan as RawOrganisePlan,
  OrganiseProgress,
  OrganiseRun,
  XMPDelete,
  XMPExport,
  AlbumRule,
  AssetDetail as RawAssetDetail,
  AssetTag,
  Changes,
  CompressionInput,
  CompressionJob,
  CompressionOptions,
  CompressionPage as RawCompressionPage,
  CompressionRun,
  DayBucket,
  DayItems as RawDayItems,
  DiskDeletion as RawDiskDeletion,
  DuplicateGroup as RawDuplicateGroup,
  DuplicateItem as RawDuplicateItem,
  Event,
  FailedFile,
  FailureGroup as RawFailureGroup,
  Face,
  GridItem,
  LegacyPreferences,
  MediaCaps,
  NamedPlace,
  Person,
  PersonFacts as RawPersonFacts,
  Place,
  Preferences as RawPreferences,
  CustomisationPreview as RawCustomisationPreview,
  Progress,
  RenderInfo,
  DisplayAdjustment,
  ViewerPreferences,
  MediaInspection as RawMediaInspection,
  MediaTrack,
  Selection as RawSelection,
  ExportOptions,
  ExportJob,
  ExportPreset,
  SearchPerson,
  SearchResult,
  Settings as RawSettings,
  Source,
  Tag,
  TrashSelection as RawTrashSelection,
  UIError,
  UpdateStatus,
  UpdatePolicy,
  View,
} from '../../bindings/lumen/internal/core'

export {
  UpdateChannel,
  UpdateFailure,
  UpdatePhase,
  Accent,
  AlbumKind,
  BatchAction,
  BlockKind,
  BlockMode,
  CompressionEffort,
  CompressionMode,
  CompressionState,
  DateSource,
  Density,
  Device,
  DuplicateKind,
  ExportFormat,
  ExportState,
  FailReason,
  Family,
  Filter,
  FocusRing,
  Kind,
  Link,
  Motion,
  OrganiseMoveState,
  OrganiseRunState,
  Playback,
  PrefCategory,
  SidecarState,
  SourceAvailability,
  StartView,
  TagKind,
  TagSource,
  Theme,
  ThumbFit,
  TrackKind,
  ViewerBackground,
  ViewerOpen,
  ViewerWheel,
  WaitReason,
}
export type {
  CityMatch,
  LibraryCounts,
  OriginalDate,
  OriginalLocation,
  UpdateStatus,
  UpdatePolicy,
  About,
  Album,
  BatchOutcome,
  AlbumRule,
  Block,
  BlockChange,
  AssetTag,
  Changes,
  CompressionInput,
  CompressionJob,
  CompressionOptions,
  CompressionRun,
  DayBucket,
  Face,
  GridItem,
  NamedPlace,
  Person,
  Event,
  FailedFile,
  LegacyPreferences,
  MediaCaps,
  OrganiseMove,
  OrganiseProgress,
  OrganiseRun,
  Place,
  Progress,
  RenderInfo,
  SearchPerson,
  DisplayAdjustment,
  ViewerPreferences,
  MediaTrack,
  ExportOptions,
  ExportJob,
  ExportPreset,
  Source,
  Tag,
  UIError,
  View,
  XMPDelete,
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

export type PersonFacts = NonNullLists<RawPersonFacts, 'companions'>
/** One asset in a duplicate group, with all its file paths (and each one's mtime). */
export type DuplicateItem = NonNullLists<RawDuplicateItem, 'paths' | 'mtimes'>
/** Exact copies or similar photos to review. */
export type DuplicateGroup = Omit<RawDuplicateGroup, 'items'> & { items: DuplicateItem[] }
/** What moving files to the Recycle Bin did; kept files were left as they were. */
export type DiskDeletion = NonNullLists<RawDiskDeletion, 'kept'>
/** Trashed assets and how many files they have: what Delete from disk asks the user to confirm. */
export type TrashSelection = NonNullLists<RawTrashSelection, 'assets'>
/** A photo a block covers, with the other blocks that also hide it. */
export type CoveredItem = NonNullLists<RawCoveredItem, 'hiddenBy'>
/** What a file says about itself, with the audio and subtitle tracks it has. */
export type MediaInspection = NonNullLists<RawMediaInspection, 'tracks'>
/** Resolved ids and the library revision they were resolved at. */
export type Selection = NonNullLists<RawSelection, 'ids'>
/** An organise preview: the moves a template would make. */
export type OrganisePlan = NonNullLists<RawOrganisePlan, 'moves'>
/** One page of a compression run's outcomes, with how many there are in all. */
export type CompressionPage = NonNullLists<RawCompressionPage, 'jobs'>

/** The interface preferences, their lists never null. */
export type Preferences = Omit<RawPreferences, 'shortcuts' | 'exportPresets'> & {
  shortcuts: Record<string, string>
  exportPresets: ExportPreset[]
}
/** The engine's settings. */
export type Settings = NonNullLists<RawSettings, 'excludedFormats'>
/** A customisation file read for review: nothing of it is applied yet. */
export type CustomisationPreview = Omit<RawCustomisationPreview, 'preferences' | 'changed'> & {
  preferences: Preferences
  changed: string[]
}

function preferencesOf(p: RawPreferences): Preferences {
  const shortcuts: Record<string, string> = {}
  for (const [id, keys] of Object.entries(p.shortcuts ?? {})) if (keys !== undefined) shortcuts[id] = keys
  return { ...p, shortcuts, exportPresets: p.exportPresets ?? [] }
}

/** The files that couldn't be read for one reason. */
export type FailureGroup = NonNullLists<RawFailureGroup, 'files'>

// Constants of the engine's that are not part of the generated bindings.
// services/contract_test.go checks each against its Go original.
export const THUMB_SMALL = 256 // core.ThumbSmall
export const THUMB_LARGE = 1024 // core.ThumbLarge
export const RENDER_SIDE = 4096 // media.RenderSide
export const RENDER_SIDE_MIN = 256 // media.RenderSideMin
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
export const AWAY_MINUTES = 5 // index.AwayAfter: no input this long and idle-only indexing runs
export const TEXT_SCALE_MIN = 90 // core.TextScaleMin: percent of the UI's base text size
export const TEXT_SCALE_MAX = 130 // core.TextScaleMax
export const TEXT_SCALE_DEFAULT = 100 // core.TextScaleDefault
export const TILE_SIZE_MIN = 96 // core.TileSizeMin: the grid's target thumbnail edge
export const TILE_SIZE_MAX = 320 // core.TileSizeMax
export const TILE_SIZE_DEFAULT = 168 // core.TileSizeDefault
export const SIDEBAR_WIDTH_MIN = 200 // core.SidebarWidthMin
export const SIDEBAR_WIDTH_MAX = 360 // core.SidebarWidthMax
export const SIDEBAR_WIDTH_DEFAULT = 248 // core.SidebarWidthDefault
export const INSPECTOR_WIDTH_MIN = 280 // core.InspectorWidthMin
export const INSPECTOR_WIDTH_MAX = 480 // core.InspectorWidthMax
export const INSPECTOR_WIDTH_DEFAULT = 340 // core.InspectorWidthDefault
export const SLIDESHOW_SECONDS_MIN = 1 // core.SlideshowSecondsMin
export const SLIDESHOW_SECONDS_MAX = 60 // core.SlideshowSecondsMax
export const SKIP_SECONDS_MIN = 1 // core.SkipSecondsMin
export const SKIP_SECONDS_MAX = 60 // core.SkipSecondsMax
export const PLAYBACK_SPEED_MIN = 0.25 // core.PlaybackSpeedMin
export const PLAYBACK_SPEED_MAX = 2 // core.PlaybackSpeedMax
export const RENDER_SIDE_STANDARD = 2048 // core.RenderSideStandard: a viewing copy's longest edge
export const RENDER_SIDE_HIGH = 4096 // core.RenderSideHigh
export const EXPORT_QUALITY_MIN = 1 // core.ExportQualityMin
export const EXPORT_QUALITY_MAX = 100 // core.ExportQualityMax
export const EXPORT_QUALITY_DEFAULT = 92 // core.ExportQualityDefault
export const MAX_EXPORT_PRESETS = 12 // core.MaxExportPresets
export const MAX_SHORTCUTS = 64 // core.MaxShortcuts
export const COMPRESSION_PAGE_SIZE = 100 // core.CompressionPageSize: outcomes in one page of a run
export const COMPRESSION_SAVINGS_MAX = 90 // core.CompressionMinSavingsMax: percent
/**
 * The members of a generated enum that a mirrored list names, in the list's
 * order. The lists below are written as the engine's plain names, which
 * services/contract_test.go reads; here they become the enum's own values,
 * so no view compares a name to an enum.
 */
function members<E extends string, N extends string>(
  of: Readonly<Record<string, E>>,
  names: readonly N[],
): readonly Extract<E, N>[] {
  const wanted: ReadonlySet<string> = new Set(names)
  const found = Object.values(of).filter((member): member is Extract<E, N> => wanted.has(member))
  return names.flatMap((name) => found.find((member) => member === name) ?? [])
}
/** The photo formats indexing can leave out, in the order the setting offers them (core.ExcludableFamilies). */
export const EXCLUDABLE_FAMILIES = members(Family, [
  'jpeg',
  'png',
  'gif',
  'webp',
  'bmp',
  'tiff',
  'heif',
  'avif',
  'jxl',
  'raw',
])
/** The screens Lumen can open on, in the order the setting offers them (core.StartViews). */
export const START_VIEWS = members(StartView, [
  'library',
  'favourites',
  'videos',
  'people',
  'things',
  'places',
  'events',
  'albums',
])
/** The groups of settings a reset can name, in the order the settings show them (core.PrefCategories). */
export const PREF_CATEGORIES = members(PrefCategory, [
  'appearance',
  'library',
  'viewing',
  'video',
  'accessibility',
  'indexing',
  'search',
  'files',
  'compression',
])
export const ORGANISE_DEFAULT_TEMPLATE = '{Year}/{Month}/{Event}/{Name}' // fileops.DefaultTemplate
/** The tokens an organise scheme may hold, in the order the editor offers them (fileops.Tokens). */
export const ORGANISE_TOKENS = ['Year', 'Month', 'MonthName', 'Day', 'Event', 'Place', 'Camera', 'Name', 'Ext'] as const
/** What a token stands for when a photo has no value for it (fileops.FallbackEvent, FallbackPlace, FallbackCamera). */
export const ORGANISE_FALLBACKS = { Event: 'Unknown event', Place: 'Unknown place', Camera: 'Unknown camera' } as const
/**
 * Why a file is left out of a move, a rename or an export (fileops.Reason*):
 * the error of its row is one of these, or else a message from Windows.
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
  Unsupported: 'unsupported',
  Interrupted: 'interrupted',
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
  CorrectionSelection: 'correction_selection',
  CorrectionDate: 'correction_date',
  CorrectionLocation: 'correction_location',
  InvalidFilter: 'invalid_filter',
  UpdateUnavailable: 'update_unavailable',
  UpdateBusy: 'update_busy',
  UpdateInvalid: 'update_invalid',
  UpdateState: 'update_state',
  UpdateSpace: 'update_space',
  UpdatePublisher: 'update_publisher',
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
  TrashChanged: 'trash_changed',
  XMPOff: 'xmp_off',
  InvalidTemplate: 'invalid_template',
  OrganiseBusy: 'organise_busy',
  OrganiseState: 'organise_state',
  SelectionChanged: 'selection_changed',
  Stopping: 'stopping',
  InvalidFileName: 'invalid_file_name',
  DestinationOutside: 'destination_outside',
  DestinationFolder: 'destination_folder',
  DestinationExcluded: 'destination_excluded',
  DestinationBlocked: 'destination_blocked',
  DestinationTaken: 'destination_taken',
  SourceChanged: 'source_changed',
  Unsupported: 'unsupported',
  PresetInvalid: 'preset_invalid',
  PresetNewer: 'preset_newer',
  CompressionBusy: 'compression_busy',
  CompressionState: 'compression_state',
  CompressionPlan: 'compression_plan',
  Preservation: 'preservation',
  CopySpace: 'copy_space',
  SigninFailed: 'signin_failed',
  SigninUnavailable: 'signin_unavailable',
} as const
export type ErrorCode = (typeof ErrorCode)[keyof typeof ErrorCode]

const errorCodes: ReadonlySet<unknown> = new Set<ErrorCode>(Object.values(ErrorCode))

/** Whether a value is one of the engine's error codes. */
export function isErrorCode(code: unknown): code is ErrorCode {
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
  // Wails beta.30 reports a late Go cancellation response separately from
  // the CancelError already handled by the read's owner. Only that known
  // response is cancellation; other late rejections remain failures.
  return (
    e instanceof CancelError ||
    (e instanceof CancelledRejectionError &&
      e.cause instanceof Call.RuntimeError &&
      e.cause.message === 'context canceled')
  )
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
  updateStatus: (): Promise<UpdateStatus> => UpdateService.Status(),
  setUpdatePolicy: (p: UpdatePolicy): Promise<void> => UpdateService.SetPolicy(p),
  checkUpdates: (): Promise<void> => UpdateService.Check(),
  downloadUpdate: (): Promise<void> => UpdateService.Download(),
  cancelUpdate: (): Promise<void> => UpdateService.Cancel(),
  discardUpdate: (): Promise<void> => UpdateService.Discard(),
  dismissUpdate: (): Promise<void> => UpdateService.Dismiss(),
  restartUpdate: (): Promise<void> => UpdateService.Restart(),
  updateHealth: (): Promise<void> => UpdateService.Health(),
  compressionAvailable: (): Promise<boolean> => CompressionService.Available(),
  planCompression: (
    inputs: CompressionInput[],
    directory: string,
    options: CompressionOptions,
  ): Promise<CompressionRun> => CompressionService.Plan(inputs, directory, options),
  compressionRuns: async (): Promise<CompressionRun[]> => (await CompressionService.Runs()) ?? [],
  compressionJobs: async (run: number, offset = 0): Promise<CompressionPage> => {
    const page = await CompressionService.Jobs(run, offset)
    return { ...page, jobs: page.jobs ?? [] }
  },
  analyseCompression: (run: number): Promise<void> => CompressionService.Analyse(run),
  commitCompression: (run: number, jobs: number[]): Promise<void> => CompressionService.Commit(run, jobs),
  pauseCompression: (run: number): Promise<void> => CompressionService.Pause(run),
  cancelCompression: (run: number): Promise<void> => CompressionService.Cancel(run),
  restoreCompression: (run: number, job: number, directory: string): Promise<CompressionRun> =>
    CompressionService.Restore(run, job, directory),
  revealCompression: (run: number, job: number): Promise<void> => CompressionService.Reveal(run, job),
  selection: async (view: View, ids: number[], search: boolean): Promise<Selection> => {
    const result = await LibraryService.Selection(view, ids, search)
    return { ...result, ids: result.ids ?? [] }
  },
  validateSelection: (sel: Selection): Promise<void> => LibraryService.ValidateSelection(sel),
  /** Does one thing to each asset; one that fails is in the outcomes, not thrown. albumID is the album actions'. */
  applyBatch: async (ids: number[], action: BatchAction, albumID = 0): Promise<BatchOutcome[]> =>
    (await LibraryService.ApplyBatch(ids, action, albumID)) ?? [],
  planFileMoves: async (ids: number[], directory: string, name: string): Promise<OrganisePlan> => {
    const result = await LibraryService.PlanFileMoves(ids, directory, name)
    return { ...result, moves: result.moves ?? [] }
  },
  openMedia: (id: number, path = '', choose = false): Promise<void> => LibraryService.OpenMedia(id, path, choose),
  planExport: async (ids: number[], directory: string, options: ExportOptions): Promise<ExportJob[]> =>
    (await LibraryService.PlanExport(ids, directory, options)) ?? [],
  runExport: async (batch: number, signal?: AbortSignal): Promise<ExportJob[]> =>
    (await cancellable(LibraryService.RunExport(batch), signal)) ?? [],
  exportJobs: async (batch = 0): Promise<ExportJob[]> => (await LibraryService.ExportJobs(batch)) ?? [],
  cancelExport: (batch: number): Promise<void> => LibraryService.CancelExport(batch),
  inspect: async (id: number, signal?: AbortSignal): Promise<MediaInspection> => {
    const r = await cancellable(MediaService.Inspect(id), signal)
    return { ...r, tracks: r.tracks ?? [] }
  },
  setAdjustment: (id: number, rotation: number, flip: boolean): Promise<DisplayAdjustment> =>
    LibraryService.SetAdjustment(id, rotation, flip),
  playbackPosition: (id: number): Promise<number> => LibraryService.PlaybackPosition(id),
  setPlaybackPosition: (id: number, position: number): Promise<void> =>
    LibraryService.SetPlaybackPosition(id, Math.round(position)),
  clearPlaybackHistory: (): Promise<void> => LibraryService.ClearPlaybackHistory(),
  /** The viewing copy of a photo, or of one page (from 0) of a file that holds several. */
  render: (id: number, signal?: AbortSignal, page = 0, side = RENDER_SIDE): Promise<RenderInfo> =>
    cancellable(MediaService.RenderPage(id, side, page), signal),
  sources: async (): Promise<Source[]> => (await LibraryService.Sources()) ?? [],
  recheckSources: (): Promise<void> => LibraryService.RecheckSources(),
  suggestedFolders: async (): Promise<string[]> => (await LibraryService.SuggestedFolders()) ?? [],
  sourcePathKey: (path: string): Promise<string> => LibraryService.SourcePathKey(path),
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

  /** Kept sidebar/caption totals; refresh after mutations and onLibraryChanged. */
  counts: (): Promise<LibraryCounts> => LibraryService.Counts(),
  /** Correct wall-clock seconds atomically; reload detail and timeline after success. */
  setTakenAt: (ids: number[], takenAt: number): Promise<void> => LibraryService.SetTakenAt(ids, takenAt),
  /** Restore the latest metadata date, including changes read since correction. */
  restoreTakenAt: (ids: number[]): Promise<void> => LibraryService.RestoreTakenAt(ids),
  /** Correct GPS and resolve its offline place; matching blocks apply immediately. */
  setLocation: (ids: number[], lat: number, lon: number): Promise<void> => LibraryService.SetLocation(ids, lat, lon),
  /** Restore the latest metadata GPS, including restoring a file with no GPS. */
  restoreLocation: (ids: number[]): Promise<void> => LibraryService.RestoreLocation(ids),
  /** Offline city suggestions, exact/prefix first; cancellation supersedes old typing. */
  findCities: async (query: string, limit = 20, signal?: AbortSignal): Promise<CityMatch[]> =>
    (await cancellable(LibraryService.FindCities(query, limit), signal)) ?? [],

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
  topPlaces: async (view: View): Promise<NamedPlace[]> => (await LibraryService.TopPlaces(view)) ?? [],
  events: async (): Promise<Event[]> => (await LibraryService.Events()) ?? [],
  event: (id: number): Promise<Event> => LibraryService.Event(id),
  /** How many events a time gap would make, without saving it; a newer preview cancels this one. */
  eventCount: (hours: number, signal?: AbortSignal): Promise<number> =>
    cancellable(LibraryService.EventCount(hours), signal),
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
  failures: async (): Promise<FailureGroup[]> =>
    ((await LibraryService.Failures()) ?? []).map((g) => ({ ...g, files: g.files ?? [] })),
  settings: async (): Promise<Settings> => {
    const s = await LibraryService.Settings()
    return { ...s, excludedFormats: s.excludedFormats ?? [] }
  },
  /** Makes indexing leave a photo format out, or take it in again; what the library holds of it stays. */
  setFormatExcluded: (family: Family, excluded: boolean): Promise<void> =>
    LibraryService.SetFormatExcluded(family, excluded),
  setVideos: (on: boolean): Promise<void> => LibraryService.SetVideos(on),
  setAIOnBattery: (on: boolean): Promise<void> => LibraryService.SetAIOnBattery(on),
  setPauseFullScreen: (on: boolean): Promise<void> => LibraryService.SetPauseFullScreen(on),
  setPauseBusy: (on: boolean): Promise<void> => LibraryService.SetPauseBusy(on),
  setIdleOnly: (on: boolean): Promise<void> => LibraryService.SetIdleOnly(on),
  setTagConfidence: (v: number): Promise<void> => LibraryService.SetTagConfidence(v),
  setEventGapHours: (hours: number): Promise<void> => LibraryService.SetEventGapHours(hours),
  setThumbCacheMB: (mb: number): Promise<void> => LibraryService.SetThumbCacheMB(mb),
  /** How many CPU threads indexing may keep busy; the engine holds it to the machine's. */
  setCPUThreads: (n: number): Promise<void> => LibraryService.SetCPUThreads(n),
  thumbCacheBytes: (): Promise<number> => MediaService.ThumbCacheBytes(),
  /** How much disk the viewing copies take: they are made again when a photo is next opened. */
  viewingCacheBytes: (): Promise<number> => MediaService.ViewingCacheBytes(),
  /** Deletes the viewing copies; resolves to the bytes freed. */
  clearViewingCache: (): Promise<number> => MediaService.ClearViewingCache(),
  /** How the user likes the interface to look. */
  preferences: async (): Promise<Preferences> => preferencesOf(await LibraryService.Preferences()),
  /** Stores every interface preference; resolves to them as stored (held to their ranges). */
  setPreferences: async (p: Preferences): Promise<Preferences> => preferencesOf(await LibraryService.SetPreferences(p)),
  /** Offers the library, once, what an older Lumen kept in the WebView's storage; resolves to the preferences afterwards. */
  migratePreferences: async (legacy: LegacyPreferences): Promise<Preferences> =>
    preferencesOf(await LibraryService.MigratePreferences(legacy)),
  /** Puts the settings of these categories back to their defaults; the library's contents are never touched. */
  resetPreferences: (categories: PrefCategory[]): Promise<void> => LibraryService.ResetPreferences(categories),
  /** Asks where, then saves the interface preferences there; "" if the user cancelled. */
  exportCustomisation: (): Promise<string> => LibraryService.ExportCustomisation(),
  /** Asks for a customisation file and reads it for review, applying nothing; its name is "" if the user cancelled. */
  readCustomisation: async (): Promise<CustomisationPreview> => {
    const r = await LibraryService.ReadCustomisation()
    return { ...r, preferences: preferencesOf(r.preferences), changed: r.changed ?? [] }
  },
  setOCR: (on: boolean): Promise<void> => LibraryService.SetOCR(on),
  setXMP: (on: boolean): Promise<void> => LibraryService.SetXMP(on),
  /** Writes every out-of-date XMP sidecar now. */
  exportXMPNow: (): Promise<XMPExport> => LibraryService.ExportXMPNow(),
  /** Turns XMP sidecars off and moves the ones Lumen wrote to the Recycle Bin. */
  deleteXMP: (): Promise<XMPDelete> => LibraryService.DeleteXMP(),

  /** Previews a template's moves (source 0: every source), saved as a planned run. */
  planOrganise: async (template: string, sourceID: number): Promise<OrganisePlan> => {
    const r = await LibraryService.PlanOrganise(template, sourceID)
    return { ...r, moves: r.moves ?? [] }
  },
  startOrganise: (runID: number): Promise<void> => LibraryService.StartOrganise(runID),
  undoOrganise: (runID: number): Promise<void> => LibraryService.UndoOrganise(runID),
  organiseRuns: async (): Promise<OrganiseRun[]> => (await LibraryService.OrganiseRuns()) ?? [],
  fileMoveOutcomes: async (runID: number): Promise<OrganiseMove[]> =>
    (await LibraryService.FileMoveOutcomes(runID)) ?? [],

  trashAssets: (ids: number[]): Promise<void> => LibraryService.TrashAssets(ids),
  restoreAssets: (ids: number[]): Promise<void> => LibraryService.RestoreAssets(ids),
  restoreAllTrash: (): Promise<void> => LibraryService.RestoreAllTrash(),
  /** What Delete from disk would take for these trashed assets (none given: all of the Trash), to confirm. */
  trashSelection: async (ids: number[]): Promise<TrashSelection> => {
    const r = await LibraryService.TrashSelection(ids)
    return { ...r, assets: r.assets ?? [] }
  },
  /** Moves the files of a confirmed selection to the Windows Recycle Bin; refused if the Trash changed since. */
  deleteFromDisk: async (sel: TrashSelection): Promise<DiskDeletion> => {
    const r = await LibraryService.DeleteFromDisk(sel)
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
  /** Unblocks while unlocked, returning an opaque receipt for undoUnblock (not a block ID). */
  unblock: (id: number): Promise<number> => LibraryService.Unblock(id),
  /** Re-block the last unblocked rule during its Undo window, even after relocking. */
  undoUnblock: (receipt: number): Promise<void> => LibraryService.UndoUnblock(receipt),
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
  /** Search results again as they are now, in order: hidden ones left out, thumbnails current. */
  searchHits: async (ids: number[], signal?: AbortSignal): Promise<GridItem[]> =>
    (await cancellable(SearchService.Hits(ids), signal)) ?? [],
  suggestTags: async (text: string): Promise<Tag[]> => (await SearchService.SuggestTags(text)) ?? [],
  prepareSearch: (): Promise<void> => SearchService.PrepareSearch(),
  /** The searches run last, newest first, without any that name something blocked. */
  recentSearches: async (): Promise<string[]> => (await SearchService.RecentSearches()) ?? [],
  /** Keeps a search the user ran; returns the recent searches as recentSearches does. */
  rememberSearch: async (query: string): Promise<string[]> => (await SearchService.RememberSearch(query)) ?? [],
  clearRecentSearches: (): Promise<void> => SearchService.ClearRecentSearches(),
  things: async (): Promise<Tag[]> => (await SearchService.Things()) ?? [],
  createLabel: (name: string): Promise<number> => SearchService.CreateLabel(name),
  deleteTag: (id: number): Promise<void> => SearchService.DeleteTag(id),
  renameThing: (id: number, name: string): Promise<void> => SearchService.RenameThing(id, name),
  setThingPinned: (id: number, pinned: boolean): Promise<void> => SearchService.SetThingPinned(id, pinned),
  assetTags: async (id: number): Promise<AssetTag[]> => (await SearchService.AssetTags(id)) ?? [],
  tagAsset: (id: number, name: string): Promise<void> => SearchService.TagAsset(id, name),
  /** tagAsset for many photos, with an outcome for each (see applyBatch). */
  tagAssets: async (ids: number[], name: string): Promise<BatchOutcome[]> =>
    (await SearchService.TagAssets(ids, name)) ?? [],
  untagAsset: (id: number, tagID: number): Promise<void> => SearchService.UntagAsset(id, tagID),

  people: async (): Promise<Person[]> => (await PeopleService.People()) ?? [],
  person: (id: number): Promise<Person> => PeopleService.Person(id),
  personFacts: async (id: number): Promise<PersonFacts> => {
    const r = await PeopleService.PersonFacts(id)
    return { ...r, companions: r.companions ?? [] }
  },
  personFaces: async (id: number): Promise<Face[]> => (await PeopleService.PersonFaces(id)) ?? [],
  assetFaces: async (id: number): Promise<Face[]> => (await PeopleService.AssetFaces(id)) ?? [],
  suggestPeople: async (text: string): Promise<Person[]> => (await PeopleService.SuggestPeople(text)) ?? [],
  personByName: (name: string): Promise<number> => PeopleService.PersonByName(name),
  namePerson: (id: number, name: string): Promise<void> => PeopleService.NamePerson(id, name),
  mergePeople: (from: number, into: number): Promise<void> => PeopleService.MergePeople(from, into),
  setPersonCover: (id: number, face: number): Promise<void> => PeopleService.SetPersonCover(id, face),
  rejectFaces: (person: number, faces: number[]): Promise<void> => PeopleService.RejectFaces(person, faces),
  nameFaces: (faces: number[], name: string): Promise<number> => PeopleService.NameFaces(faces, name),

  about: (): Promise<About> => SystemService.About(),
  /** Opens a page outside Lumen in the user's browser; Lumen itself makes no request. */
  openLink: (link: Link): Promise<void> => SystemService.OpenLink(link),
  openNotices: (): Promise<void> => SystemService.OpenNotices(),
  openLogFolder: (): Promise<void> => SystemService.OpenLogFolder(),
  /** Asks where, then saves a problem report there; "" if the user cancelled. */
  saveProblemReport: (): Promise<string> => SystemService.SaveProblemReport(),
}

/**
 * Writes a failure in the UI to the engine's log, which has no other way to
 * see it. It never fails: a failure to log has nowhere left to go.
 */
export function logUIError(e: UIError): void {
  void Promise.resolve()
    .then(() => SystemService.LogUIError(e))
    .catch(() => undefined)
}

// The engine's events (services.EventProgress, EventLibraryChanged, EventOrganiseProgress),
// typed by the generated event declarations: a name they don't know fails
// to compile.
const EVENT_PROGRESS = 'index:progress' satisfies keyof Events.CustomEvents
const EVENT_LIBRARY_CHANGED = 'library:changed' satisfies keyof Events.CustomEvents
const EVENT_ORGANISE_PROGRESS = 'organise:progress' satisfies keyof Events.CustomEvents
const EVENT_UPDATE_STATUS = 'update:status' satisfies keyof Events.CustomEvents

/** Fires for each ordered installation update snapshot. */
export function onUpdateStatus(cb: (s: UpdateStatus) => void): () => void {
  return Events.On(EVENT_UPDATE_STATUS, (e) => cb(e.data))
}

/** Fires whenever indexing progress changes. Returns an unsubscribe function. */
export function onProgress(cb: (p: Progress) => void): () => void {
  return Events.On(EVENT_PROGRESS, (e) => cb(e.data))
}

/**
 * Fires (at most once a second) when the library changed, saying what:
 * assets, their tags, who is in them, or cached source availability (sources).
 * Read engine.sources() when sources is true. Returns an unsubscribe function.
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

export function renderUrl(id: number, revision: string, page = 0, side = RENDER_SIDE): string {
  const url = `/media/render/${id}/${side}?v=${encodeURIComponent(revision)}`
  return page > 0 ? `${url}&page=${page}` : url
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
export function playUrl(id: number, at = 0, audio = -1): string {
  return `${videoBase}/play/${id}?at=${Math.max(0, Math.round(at))}${audio >= 0 ? `&audio=${audio}` : ''}`
}

export function subtitleUrl(id: number, track: number, at: number): string {
  return `/media/subtitle/${id}?track=${track}&at=${Math.max(0, Math.round(at))}`
}

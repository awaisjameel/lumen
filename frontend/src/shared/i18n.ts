// Every user-facing string lives here, so another language (e.g. Urdu) can be
// added later as a second dictionary without touching components. A message
// that reads differently for one thing has a form per plural category of
// {n} (Intl.PluralRules: English uses one and other).

/** A message, or one form per plural category of its {n}. */
type Message = string | (Partial<Record<Intl.LDMLPluralRule, string>> & { other: string })

const en = {
  'app.name': 'Lumen',

  'nav.library': 'Library',
  'nav.people': 'People',
  'nav.favourites': 'Favourites',
  'nav.videos': 'Videos',
  'nav.things': 'Things',
  'nav.places': 'Places',
  'nav.events': 'Events',
  'nav.albums': 'Albums',
  'nav.duplicates': 'Duplicates',
  'nav.trash': 'Trash',
  'nav.settings': 'Settings',

  'blocked.title': 'Blocked',
  'blocked.back': 'Back to Settings',
  'blocked.block': 'Block',
  'blocked.saved': 'Blocked “{name}”.',
  'blocked.undo': 'Undo',
  'blocked.confirmTitle': 'Block “{name}”?',
  'blocked.confirmText': 'Choose what disappears from Lumen. Original files stay where they are.',
  'blocked.mode': 'What should disappear?',
  'blocked.labelOnly': 'Label only',
  'blocked.labelPhotos': 'Label and photos',
  'blocked.photosOnly': 'This hides the photo or folder and its media.',
  'blocked.createPINHint':
    'Create a {pinMin}–{pinMax} digit PIN to protect the Blocked screen. If you forget it, you can choose a new one by signing in to Windows.',
  'blocked.enterPINHint': 'Enter your PIN to open the Blocked screen.',
  'blocked.newPIN': 'New PIN',
  'blocked.confirmPIN': 'Confirm PIN',
  'blocked.enterPIN': 'PIN',
  'blocked.pinMismatch': 'The PINs do not match.',
  'blocked.unlock': 'Unlock',
  'blocked.privacy': 'Blocks only hide items in Lumen. Files and cached previews remain on this computer.',
  'blocked.empty': 'Nothing is blocked.',
  'blocked.asset': 'Photo or video',
  'blocked.blockPlace': 'Block this place',
  'blocked.blockFolder': 'Block the folder containing {path}',
  'blocked.blockFolderShort': 'Block folder',
  'blocked.switchMode': 'Switch mode',
  'blocked.unblock': 'Unblock',
  'blocked.unblockTitle': 'Unblock “{name}”?',
  'blocked.unblockText': 'It and any photos covered only by this block will reappear.',

  'search.placeholder': 'Search, e.g. “birthday cake”',
  'search.clear': 'Clear search',
  'search.shortcut': 'Search (Ctrl+F)',
  'search.thingsHeading': 'Things',
  'search.for': 'Search for “{q}”',
  'search.results': { one: '1 match', other: '{n} matches, best first' },
  'search.more': 'Showing the best {n}. Add words to narrow it down.',
  'search.pending': {
    one: 'Lumen is still understanding 1 photo, so results may grow.',
    other: 'Lumen is still understanding {n} photos, so results may grow.',
  },
  'search.searching': 'Searching…',
  'search.none': 'No matches',
  'search.noneHint': 'Try other words, or describe what’s in the photo, like “a dog on a sofa”.',
  'search.unavailable': 'Search isn’t available right now',
  'search.peopleHeading': 'People',
  'search.withPeople': 'Photos with {names}.',
  'search.onlyPeople': 'Only who is in them was searched: the rest needs the on-device AI, which isn’t available.',
  'search.title': '“{q}”',
  'search.blocked': 'This search names a blocked person, Thing, place, or album.',
  'search.openBlocked': 'Open Blocked',

  'people.intro':
    'Lumen finds the people in your photos, right here on this computer. Name someone once and every photo of them follows.',
  'people.unavailable': 'People isn’t available: {error}',
  'people.named': 'Named',
  'people.notNamed': 'Not named yet',
  'people.empty': 'No people yet',
  'people.emptyWorking': {
    one: 'Lumen is still looking for faces: 1 photo to go.',
    other: 'Lumen is still looking for faces: {n} photos to go.',
  },
  'people.emptyHint': 'Someone appears here once they are clearly in three or more photos.',

  'person.unnamed': 'Unnamed person',
  'person.addName': 'Add a name',
  'person.name': 'Name',
  'person.namePlaceholder': 'Who is this?',
  'person.rename': 'Change the name',
  'person.review': 'Review faces',
  'person.mergeTitle': 'Someone is already called “{name}”',
  'person.mergeText': 'Are they the same person? Their photos will be put together.',
  'person.merge': 'Same person',
  'person.empty': 'No photos of them right now',
  'person.emptyHint': 'Their photos may be in a folder that was removed, or their faces were moved to someone else.',

  'faces.title': 'Faces of {name}',
  'faces.back': 'Back to their photos',
  'faces.hint': 'Least alike first. Select any that aren’t {name}.',
  'faces.selected': '{n} selected',
  'faces.notThem': 'Not {name}',
  'faces.moveTo': 'Move to someone…',
  'faces.cover': 'Use as their picture',
  'faces.selectAll': 'Select all',
  'faces.clear': 'Clear',
  'faces.none': 'No faces left.',
  'faces.byYou': 'Placed by you',
  'faces.face': 'Face {n}',
  'faces.faceByYou': 'Face {n}, placed by you',

  'things.intro': 'Lumen sorts your photos by what’s in them, right here on this computer.',
  'things.new': 'Look for something new, like “Eiffel Tower”',
  'things.find': 'Find',
  'things.finding': 'Looking…',
  'things.empty': 'No things yet',
  'things.emptyWorking': 'Lumen is still understanding your photos: {n} to go.',
  'things.emptyHint': 'Things appear as Lumen understands your photos.',
  'things.delete': 'Delete',
  'things.deleteNamed': 'Delete “{name}”',
  'things.deleteTitle': 'Delete “{name}”?',
  'things.deleteText': 'Lumen stops looking for it, and it comes off every photo. Your photos stay as they are.',
  'things.yours': 'Made by you',
  'things.noPhotos': 'No photos yet',
  'things.viewEmpty': 'Nothing here right now',
  'things.viewEmptyHint': 'Lower “Tag confidence” in Settings to see less certain matches.',

  'albums.intro': 'Keep photos together in an album, or save rules that update as your library changes.',
  'albums.new': 'New album',
  'albums.name': 'Album name',
  'albums.manual': 'Manual album',
  'albums.smart': 'Smart album',
  'albums.create': 'Create album',
  'albums.save': 'Save changes',
  'albums.edit': 'Edit album',
  'albums.delete': 'Delete album',
  'albums.editNamed': 'Edit “{name}”',
  'albums.deleteNamed': 'Delete “{name}”',
  'albums.summary': '{items} · {kind}',
  'albums.deleteTitle': 'Delete “{name}”?',
  'albums.deleteText': 'The album will be removed. Your photos and videos stay in the library.',
  'albums.kind': 'Media',
  'albums.type': 'Album type',
  'albums.anyMedia': 'Photos and videos',
  'albums.photos': 'Photos',
  'albums.videos': 'Videos',
  'albums.favourites': 'Favourites only',
  'albums.from': 'Taken on or after',
  'albums.to': 'Taken on or before',
  'albums.tag': 'Thing',
  'albums.anyTag': 'Any Thing',
  'albums.person': 'Person',
  'albums.anyPerson': 'Anyone',
  'albums.empty': 'No albums yet',
  'albums.emptyHint': 'Create an album to collect photos or save a set of rules.',
  'albums.viewEmpty': 'No items in this album yet',
  'albums.manualEmptyHint': 'Select a photo or video and add it from Details.',
  'albums.smartEmptyHint': 'Edit this album’s rules to include more items.',
  'albums.createForItem': 'New album for this item',

  'places.intro': 'Find photos by nearby city. Place names come from the bundled GeoNames data and are approximate.',
  'places.empty': 'No places yet',
  'places.emptyHint': 'Places appear when photos with GPS locations finish indexing.',
  'places.viewEmpty': 'No photos here right now',
  'events.intro': 'Nearby photos are grouped by time and location. You can change the time gap in Settings.',
  'events.empty': 'No events yet',
  'events.emptyHint': 'Events appear as your photos are indexed.',
  'events.viewEmpty': 'No photos in this event right now',
  'events.name': 'Event name',
  'events.rename': 'Rename {name}',
  'events.save': 'Save',
  'events.reset': 'Use automatic name',

  'duplicates.intro':
    'Review files with identical content and photos that look alike. Nothing is changed automatically.',
  'duplicates.exact': 'Exact copies',
  'duplicates.similar': 'Similar photos',
  'duplicates.exactHint': 'These file paths contain identical content.',
  'duplicates.similarHint':
    'These are suggestions based on the photo previews. Check them before deciding what to keep.',
  'duplicates.copies': '{n} copies of {size}',
  'duplicates.empty': 'No duplicates found',
  'duplicates.emptyHint': 'Results appear as thumbnails finish processing.',
  'duplicates.showFile': 'Show “{name}” in folder',
  'duplicates.refresh': 'Check again',
  'duplicates.loading': 'Checking the library…',
  'duplicates.preview': 'Preview “{name}”',
  'duplicates.photoSize': '{w} × {h} · {size}',

  'welcome.title': 'Welcome to Lumen',
  'welcome.question': 'Which folders hold your photos and videos?',
  'welcome.none': 'We couldn’t find the usual photo folders. Add one to get started.',
  'welcome.addFolder': 'Add a folder…',
  'welcome.start': 'Start',
  'welcome.privacy':
    'Everything stays on this computer. Nothing is uploaded, and your files are never moved or changed.',

  'status.scanning': 'Looking for photos…',
  'status.preparing': 'Preparing your library… {left} left',
  'status.upToDate': 'Up to date',
  'status.preparingPhase': 'Preparing your library',
  'status.understandingPhase': 'Understanding your photos',
  'status.batteryPhase': 'Waiting for mains power',
  'status.pausedPhase': 'Indexing paused',
  'status.paused': 'Paused · {left} left',
  'status.pause': 'Pause',
  'status.resume': 'Resume',
  'status.understanding': 'Understanding your photos… {left} left',
  'status.battery': 'Waiting for mains power · {left} left',
  'status.aiOff': 'Search is off',
  'status.peopleOff': 'People is off',
  'status.errors': '{n} couldn’t be read',
  'status.skipped': '{n} can’t be shown on this PC yet',
  'status.items': { one: '1 item', other: '{n} items' },

  'library.emptyScanning': 'Looking for photos…',
  'library.empty': 'No photos here yet',
  'library.emptyHint': 'Add a folder in Settings, or check that your folders contain photos.',
  'favourites.empty': 'No favourites yet',
  'favourites.emptyHint': 'Press F or click the heart on any photo to add it here.',
  'videos.empty': 'No videos',
  'videos.emptyHint': 'Turn on “Include videos” in Settings to add them to your library.',
  'library.zoom': 'Thumbnail size',
  'library.info': 'Details',
  'library.photo': 'Photo',
  'library.video': 'Video',
  'library.photoOn': 'Photo, {day}',
  'library.videoOn': 'Video, {day}',
  'library.favouriteItem': '{item}, favourite',

  'format.megapixels': '{mp} MP',
  'format.bytes': '{n} B',

  'day.today': 'Today',
  'day.yesterday': 'Yesterday',

  'details.title': 'Details',
  'details.none': 'Select a photo to see its details.',
  'details.loading': 'Loading…',
  'details.dimensions': '{w} × {h} · {mp} · {size}',
  'details.taken': 'Taken',
  'details.dateFrom.exif': 'From the camera',
  'details.dateFrom.filename': 'From the file name',
  'details.dateFrom.file': 'From the file’s modified date',
  'details.size': 'Size',
  'details.camera': 'Camera',
  'details.location': 'Location',
  'details.files': 'Files',
  'details.copies': '{n} identical copies',
  'details.tags': 'Tags',
  'details.noTags': 'No tags yet.',
  'details.addTag': 'Add a tag…',
  'details.removeTag': 'Remove “{name}”',
  'details.byAI': 'Suggested by Lumen',
  'details.byYou': 'Added by you',
  'details.people': 'People',
  'details.whoIsThis': 'Who is this?',
  'details.notThisPerson': 'Not {name}',
  'details.favourite': 'Favourite',
  'details.unfavourite': 'Remove favourite',

  'viewer.title': 'Viewer',
  'viewer.prev': 'Previous',
  'viewer.next': 'Next',
  'viewer.noPreview': 'No preview for this format yet',
  'viewer.seek': 'Seek video',
  'viewer.videoFailed': 'This video could not be played.',
  'viewer.videoUnavailable': 'Videos can’t play: Lumen couldn’t start its video player. Restarting Lumen may help.',
  'details.duration': 'Duration',
  'details.videoCodec': 'Video codec',
  'details.audioCodec': 'Audio codec',

  'settings.title': 'Settings',
  'settings.folders': 'Folders',
  'settings.foldersHint':
    'Lumen looks for photos in these folders and keeps watching them for changes. Removing a folder never deletes anything on disk.',
  'settings.addFolder': 'Add a folder…',
  'settings.remove': 'Remove',
  'settings.removeNamed': 'Remove {path}',
  'settings.removeTitle': 'Remove {path}?',
  'settings.removeText':
    'Its photos leave the library. Nothing is deleted from the disk, and adding the folder again brings back their names, favourites and albums.',
  'settings.rescan': 'Check for changes now',
  'settings.videos': 'Include videos',
  'settings.videosHint':
    'Off by default because indexing videos takes more time. All processing stays on this computer.',
  'settings.indexing': 'Background work',
  'settings.pause': 'Pause indexing',
  'settings.ai': 'Search, Things and People',
  'settings.aiHint':
    'Lumen understands your photos on this computer, so you can search them by what’s in them and who is in them. Nothing is uploaded.',
  'settings.aiGpu': 'Working on the graphics card.',
  'settings.aiCpu': 'Working on the processor.',
  'settings.aiIdle': 'Ready.',
  'settings.aiError': 'Not available: {error}',
  'settings.peopleError': 'Recognising people isn’t available: {error}',
  'settings.aiOnBattery': 'Keep working on battery',
  'settings.aiOnBatteryHint': 'Off by default: understanding photos is heavy work, so Lumen waits for mains power.',
  'settings.advanced': 'Advanced',
  'settings.confidence': 'Tag confidence',
  'settings.confidenceHint': 'How sure Lumen must be before it tags a photo. Changes apply at once.',
  'settings.eventGap': 'Time between events (hours)',
  'settings.eventGapHint': 'A gap longer than this starts a new event. A large location jump also starts one.',
  'settings.confidenceFewer': 'Fewer, surer tags',
  'settings.confidenceMore': 'More tags, more mistakes',
  'settings.appearance': 'Appearance',
  'settings.theme': 'Theme',
  'settings.theme.system': 'Match Windows',
  'settings.theme.light': 'Light',
  'settings.theme.dark': 'Dark',
  'settings.privacy': 'Privacy',
  'settings.privacyText':
    'Lumen works entirely on this computer. It makes no network connections and collects no data.',

  'common.cancel': 'Cancel',
  'common.close': 'Close',
  'common.dismiss': 'Dismiss',
  'common.showInFolder': 'Show in folder',
  'common.withKey': '{label} ({key})',
  'common.labelValue': '{label}: {value}',

  'error.generic': 'Something went wrong: {msg}',
  'error.notFound': 'That isn’t there any more: it may have been deleted, or its file moved away.',
  'error.noAI': 'The on-device AI isn’t available on this computer.',
  'error.invalidName': 'A name must be 1 to {max} characters.',
  'error.albumExists': 'An album already has that name.',
  'error.invalidAlbum': 'This album can’t be saved.',
  'error.albumMediaKind': 'Choose photos, videos, or all media.',
  'error.albumSelection': 'Choose a valid Thing or person.',
  'error.albumDate': 'Enter a valid date in YYYY-MM-DD format.',
  'error.albumDateOrder': 'The start date must not follow the end date.',
  'error.albumSmartItems': 'Smart albums choose their own items.',
  'error.albumKind': 'Choose a manual or smart album.',
  'error.albumReference': 'The selected Thing or person no longer exists.',
  'error.albumManualOnly': 'Only manual albums accept items.',
  'error.personExists': 'Someone already has that name.',
  'error.blockedName': 'That name belongs to a blocked person or Thing. Open Blocked in Settings to manage it.',
  'error.tagExists': 'A Thing already has that name.',
  'error.builtinTag': 'Lumen’s own Things can be blocked, not deleted.',
  'error.tagInAlbum': 'A smart album uses this Thing. Change that album’s rule first.',
  'error.alreadyCovered': 'That folder is already in your library.',
  'error.folderNotFound': 'That folder can’t be found.',
  'error.invalidPIN': 'That PIN isn’t right. A PIN is {pinMin} to {pinMax} digits.',
  'error.pinExists': 'A PIN has already been set. Enter it to continue.',
  'error.pinRateLimited': 'Too many incorrect PINs. Try again in {pinCooldown} seconds.',
  'error.blocksLocked': 'Enter your PIN on the Blocked screen first.',
  'error.undoExpired': 'It’s too late to undo that block. Unblock it on the Blocked screen instead.',
  'error.invalidBlock': 'That can’t be blocked.',
  'error.deleteFailed': 'Lumen couldn’t use the Recycle Bin, so it left the remaining files where they are.',

  // Things and Events: rename and pin.
  'things.pin': 'Pin',
  'things.unpin': 'Unpin',
  'things.pinNamed': 'Pin “{name}”',
  'things.unpinNamed': 'Unpin “{name}”',
  'things.pinned': 'Pinned',
  'things.pinnedHeading': 'Pinned',
  'things.rename': 'Rename',
  'things.renameNamed': 'Rename “{name}”',
  'things.name': 'Thing name',
  'things.save': 'Save',
  'things.resetName': 'Reset name',
  'events.pin': 'Pin',
  'events.unpin': 'Unpin',
  'events.pinNamed': 'Pin {name}',
  'events.unpinNamed': 'Unpin {name}',
  'events.pinned': 'Pinned',

  // Trash.
  'trash.move': 'Move to Trash',
  'trash.moved': { one: 'Moved 1 item to the Trash.', other: 'Moved {n} items to the Trash.' },
  'trash.undo': 'Undo',
  'trash.restore': 'Restore',
  'trash.restoreAll': 'Restore all',
  'trash.deleteFromDisk': 'Delete from disk…',
  'trash.deleteAll': 'Delete all from disk…',
  'trash.summary': {
    one: '1 item is in the Trash. Nothing is removed from your disk until you delete it from here.',
    other: '{n} items are in the Trash. Nothing is removed from your disk until you delete them from here.',
  },
  'trash.confirmTitle': 'Delete from disk?',
  'trash.confirmFiles': {
    one: '1 file will be moved to the Windows Recycle Bin. You can still restore it from there.',
    other: '{n} files will be moved to the Windows Recycle Bin. You can still restore them from there.',
  },
  'trash.confirmOk': 'Move to Recycle Bin',
  'trash.kept': {
    one: '1 file couldn’t be moved to the Recycle Bin, so it was left where it is.',
    other: '{n} files couldn’t be moved to the Recycle Bin, so they were left where they are.',
  },
  'trash.empty': 'The Trash is empty',
  'trash.emptyHint':
    'Photos and videos you move to the Trash wait here until you restore them or delete them from disk.',

  // Thumbnail cache.
  'settings.cache': 'Thumbnail cache',
  'settings.cacheHint':
    'Small previews Lumen keeps so browsing stays quick. Past the size below, the ones you haven’t looked at for longest are removed, and made again when you need them.',
  'settings.cacheLimit': 'Keep at most (GB)',
  'settings.cacheUsed': 'In use now: {size}',

  // Text in photos (OCR).
  'settings.ocr': 'Find text in screenshots and documents',
  'settings.ocrHint':
    'Off by default. Lumen reads the words in photos it takes for screenshots or documents, so you can search for them. It works on the processor, only on those photos, and waits for mains power like the rest of the AI.',
  'status.reading': 'Reading text in your photos… {left} left',
  'status.readingPhase': 'Reading text in your photos',

  // Forgotten PIN.
  'blocked.forgotPIN': 'Forgot PIN?',
  'blocked.resetHint':
    'Choose a new PIN. Windows will then ask you to sign in, with Windows Hello or your PC password, to confirm it is you. Your blocks are kept.',
  'blocked.reset': 'Reset with Windows sign-in',
  'blocked.resetCancel': 'Cancel',
  'blocked.pinChanged': 'Your PIN was changed. Enter the new PIN to open the Blocked screen.',
  'error.signinFailed': 'Windows could not confirm it is you, so the PIN was not changed.',
  'error.signinUnavailable': 'This PC has no Windows sign-in that Lumen can use, so the PIN cannot be reset.',
} as const satisfies Record<string, Message>

export type MessageKey = keyof typeof en

/** The locale every date and number is formatted in (matches the dictionary). */
export const LOCALE = 'en-GB'

const numberFmt = new Intl.NumberFormat(LOCALE)
const plurals = new Intl.PluralRules(LOCALE)

/** Joins names for a sentence: "Ali, Sara and Omar". */
export const listFormat = new Intl.ListFormat(LOCALE, { type: 'conjunction' })

/** The names of the {placeholders} in a message string. */
type Placeholders<S> = S extends `${string}{${infer Name}}${infer Rest}` ? Name | Placeholders<Rest> : never

/** The placeholders of key's message: all its plural forms', which select by n. */
type VarNames<K extends MessageKey> = (typeof en)[K] extends infer M
  ? M extends string
    ? Placeholders<M>
    : Placeholders<M[keyof M]> | 'n'
  : never

type Vars = Record<string, string | number>

/**
 * What t takes after the key: every placeholder of that message, which the
 * compiler checks, so none is left showing as "{name}". A key only known as
 * some MessageKey (chosen at run time) is checked when it is chosen.
 */
type VarsArg<K extends MessageKey> = MessageKey extends K
  ? [vars?: Vars]
  : [VarNames<K>] extends [never]
    ? [vars?: Vars]
    : [vars: Record<VarNames<K>, string | number> & Vars]

/** Translate a key, filling {placeholders}. Numbers are locale-formatted. */
export function t<K extends MessageKey>(key: K, ...[vars]: VarsArg<K>): string {
  const m: Message = en[key]
  let s = typeof m === 'string' ? m : (m[plurals.select(Number(vars?.n))] ?? m.other)
  if (vars) {
    for (const [k, v] of Object.entries(vars)) {
      s = s.replaceAll(`{${k}}`, typeof v === 'number' ? numberFmt.format(v) : v)
    }
  }
  return s
}

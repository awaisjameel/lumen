// Every user-facing string lives here, so another language (e.g. Urdu) can be
// added later as a second dictionary without touching components.

const en = {
  'app.name': 'Lumen',

  'nav.library': 'Library',
  'nav.favourites': 'Favourites',
  'nav.videos': 'Videos',
  'nav.settings': 'Settings',

  'welcome.title': 'Welcome to Lumen',
  'welcome.question': 'Which folders hold your photos and videos?',
  'welcome.none': 'We couldn’t find the usual photo folders. Add one to get started.',
  'welcome.addFolder': 'Add a folder…',
  'welcome.start': 'Start',
  'welcome.privacy': 'Everything stays on this computer. Nothing is uploaded, and your files are never moved or changed.',

  'status.scanning': 'Looking for photos… {n} found',
  'status.preparing': 'Preparing your library… {left} left',
  'status.upToDate': 'Up to date',
  'status.paused': 'Paused · {left} left',
  'status.pause': 'Pause',
  'status.resume': 'Resume',
  'status.errors': '{n} couldn’t be read',
  'status.skipped': '{n} in formats coming soon (e.g. HEIC)',
  'status.items': '{n} items',

  'library.emptyScanning': 'Looking for photos…',
  'library.empty': 'No photos here yet',
  'library.emptyHint': 'Add a folder in Settings, or check that your folders contain photos.',
  'favourites.empty': 'No favourites yet',
  'favourites.emptyHint': 'Press F or click the heart on any photo to add it here.',
  'videos.empty': 'No videos',
  'videos.emptyHint': 'Turn on “Include videos” in Settings to add them to your library.',
  'library.zoom': 'Thumbnail size',
  'library.info': 'Details',

  'day.today': 'Today',
  'day.yesterday': 'Yesterday',

  'details.title': 'Details',
  'details.none': 'Select a photo to see its details.',
  'details.taken': 'Taken',
  'details.dateFrom.exif': 'From the camera',
  'details.dateFrom.filename': 'From the file name',
  'details.dateFrom.file': 'From the file’s modified date',
  'details.size': 'Size',
  'details.camera': 'Camera',
  'details.location': 'Location',
  'details.files': 'Files',
  'details.copies': '{n} identical copies',
  'details.showInFolder': 'Show in folder',
  'details.favourite': 'Favourite',
  'details.unfavourite': 'Remove favourite',

  'viewer.close': 'Close',
  'viewer.prev': 'Previous',
  'viewer.next': 'Next',
  'viewer.noPreview': 'No preview for this format yet',

  'settings.title': 'Settings',
  'settings.folders': 'Folders',
  'settings.foldersHint': 'Lumen looks for photos in these folders and keeps watching them for changes. Removing a folder never deletes anything on disk.',
  'settings.addFolder': 'Add a folder…',
  'settings.remove': 'Remove',
  'settings.rescan': 'Check for changes now',
  'settings.videos': 'Include videos',
  'settings.videosHint': 'Off by default. Video support is limited until video processing arrives.',
  'settings.indexing': 'Background work',
  'settings.pause': 'Pause indexing',
  'settings.appearance': 'Appearance',
  'settings.theme': 'Theme',
  'settings.theme.system': 'Match Windows',
  'settings.theme.light': 'Light',
  'settings.theme.dark': 'Dark',
  'settings.privacy': 'Privacy',
  'settings.privacyText': 'Lumen works entirely on this computer. It makes no network connections and collects no data.',

  'error.generic': 'Something went wrong: {msg}',
} as const

export type MessageKey = keyof typeof en

/** The locale every date and number is formatted in (matches the dictionary). */
export const LOCALE = 'en-GB'

const numberFmt = new Intl.NumberFormat(LOCALE)

/** Translate a key, filling {placeholders}. Numbers are locale-formatted. */
export function t(key: MessageKey, vars?: Record<string, string | number>): string {
  let s: string = en[key]
  if (vars) {
    for (const [k, v] of Object.entries(vars)) {
      s = s.replaceAll(`{${k}}`, typeof v === 'number' ? numberFmt.format(v) : v)
    }
  }
  return s
}
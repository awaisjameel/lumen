import type { ViewerPreferences } from '../bridge'

/** The fallback is core.DefaultViewerPreferences, until the library loads. */
export function viewerDefaults(): ViewerPreferences {
  return {
    filmstrip: false,
    autoHide: false,
    slideshowSeconds: 5,
    loop: false,
    autoplay: true,
    volume: 1,
    muted: false,
    speed: 1,
    skipSeconds: 10,
    resume: false,
  }
}

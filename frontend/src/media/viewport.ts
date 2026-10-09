/** Geometry uses upright source pixels and CSS viewport pixels. */
export interface Viewport {
  width: number
  height: number
  imageWidth: number
  imageHeight: number
  scale: number
  x: number
  y: number
}

/** Zoom and pan as two linked surfaces share them: the offset is in image sizes. */
export interface ViewGeometry {
  scale: number
  x: number
  y: number
}

export interface ImageControls {
  fit: () => void
  actual: () => void
  zoom: (factor: number) => void
  scale: number
  /** The page shown, from 0, of the pages the file holds (1 unless it is a TIFF of several). */
  page: number
  pages: number
  turnPage: (by: number) => void
  /** An animated image held on its first frame (the animation preference), and whether it is playing now. */
  animated: boolean
  playing: boolean
  toggleAnimation: () => void
}

export function fitScale(v: Viewport): number {
  return Math.min(1, v.width / Math.max(1, v.imageWidth), v.height / Math.max(1, v.imageHeight))
}

export function constrain(v: Viewport): Viewport {
  const mx = Math.max(0, (v.imageWidth * v.scale - v.width) / 2)
  const my = Math.max(0, (v.imageHeight * v.scale - v.height) / 2)
  return { ...v, x: mx ? Math.max(-mx, Math.min(mx, v.x)) : 0, y: my ? Math.max(-my, Math.min(my, v.y)) : 0 }
}

export function zoomAt(v: Viewport, scale: number, x = v.width / 2, y = v.height / 2): Viewport {
  scale = Math.max(Math.min(fitScale(v), 0.1), Math.min(8, scale))
  const factor = scale / Math.max(0.0001, v.scale)
  return constrain({
    ...v,
    scale,
    x: (x - v.width / 2) * (1 - factor) + v.x * factor,
    y: (y - v.height / 2) * (1 - factor) + v.y * factor,
  })
}

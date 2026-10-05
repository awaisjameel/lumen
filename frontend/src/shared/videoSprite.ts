import { SPRITE_COLS, SPRITE_ROWS, spriteUrl } from '../bridge'

/** Grid hover and playback scrubbing share the bounded indexed sprite. */
export function videoSpriteStyle(id: number, version: number, frame: number, width: number, height: number) {
  frame = Math.max(0, Math.min(SPRITE_COLS * SPRITE_ROWS - 1, Math.floor(frame)))
  return {
    width: `${width}px`,
    height: `${height}px`,
    backgroundImage: `url("${spriteUrl(id, version)}")`,
    backgroundSize: `${width * SPRITE_COLS}px ${height * SPRITE_ROWS}px`,
    backgroundPosition: `${-(frame % SPRITE_COLS) * width}px ${-Math.floor(frame / SPRITE_COLS) * height}px`,
  }
}

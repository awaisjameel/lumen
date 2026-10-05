/** FFmpeg supplies WebVTT; only valid timed cues enter the playback track. */
export function subtitleCues(vtt: string): { start: number; end: number; text: string }[] {
  const out: { start: number; end: number; text: string }[] = []
  const seconds = (value: string) => value.split(':').reduce((n, part) => n * 60 + Number(part), 0)
  for (const block of vtt.replaceAll('\r', '').split(/\n\s*\n/)) {
    const lines = block.split('\n')
    const index = lines.findIndex((line) => line.includes(' --> '))
    const timed = lines[index]?.match(/^(\d{2}:\d{2}(?::\d{2})?\.\d{3}) --> (\d{2}:\d{2}(?::\d{2})?\.\d{3})(?:\s|$)/)
    if (!timed?.[1] || !timed[2]) continue
    const start = seconds(timed[1])
    const end = seconds(timed[2])
    if (Number.isFinite(start) && Number.isFinite(end) && end > start)
      out.push({ start, end, text: lines.slice(index + 1).join('\n') })
  }
  return out
}

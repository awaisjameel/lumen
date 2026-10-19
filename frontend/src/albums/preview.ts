import { onScopeDispose, ref, shallowRef, watch } from 'vue'
import { engine, Filter, Kind, isCancelled } from '../bridge'
import type { AlbumRule, GridItem, View } from '../bridge'
import { errorText } from '../shared/errors'
import { onBlocksChanged } from '../shared/block'

/** Reads the same visible timeline as a saved album, without saving the draft. */
export function useAlbumPreview(rule: () => AlbumRule | null) {
  const count = ref<number | null>(null)
  const items = shallowRef<GridItem[]>([])
  const loading = ref(false)
  const error = ref('')
  let controller: AbortController | undefined
  let timer: ReturnType<typeof setTimeout> | undefined
  let revision = 0

  async function refresh() {
    const mine = ++revision
    controller?.abort()
    clearTimeout(timer)
    const draft = rule()
    count.value = null
    items.value = []
    error.value = ''
    loading.value = draft !== null
    if (!draft) return
    const snapshot = { ...draft }
    const request = new AbortController()
    controller = request
    const view: View = {
      filter: snapshot.favourites
        ? Filter.FilterFavourites
        : snapshot.kind === Kind.KindPhoto
          ? Filter.FilterAll
          : snapshot.kind === Kind.KindVideo
            ? Filter.FilterVideos
            : Filter.FilterAll,
      tag: snapshot.tag,
      person: snapshot.person,
    }
    try {
      const days = (await engine.timeline(view, request.signal)).filter(
        (day) => (!snapshot.from || day.day >= snapshot.from) && (!snapshot.to || day.day <= snapshot.to),
      )
      if (mine !== revision) return
      let total = days.reduce((n, day) => n + day.count, 0)
      const previews: GridItem[] = []
      // Favourites and media are separate timeline filters. Grid items carry
      // media kind, so only that conjunction needs every matching day read.
      const needsMedia = snapshot.kind === Kind.KindPhoto || (snapshot.favourites && snapshot.kind === Kind.KindVideo)
      if (needsMedia) total = 0
      for (const day of days) {
        if (!needsMedia && previews.length >= 3) break
        const result = await engine.days(day.day, day.day, view, request.signal)
        if (mine !== revision) return
        const matching = result
          .flatMap((row) => row.items)
          .filter((item) => !needsMedia || Boolean(item.v) === (snapshot.kind === Kind.KindVideo))
        if (needsMedia) total += matching.length
        previews.push(...matching.slice(0, 3 - previews.length))
      }
      if (mine !== revision) return
      count.value = total
      items.value = previews
    } catch (e) {
      if (mine === revision && !isCancelled(e)) error.value = errorText(e)
    } finally {
      if (mine === revision) loading.value = false
    }
  }

  watch(
    rule,
    () => {
      revision++
      controller?.abort()
      clearTimeout(timer)
      count.value = null
      items.value = []
      error.value = ''
      loading.value = rule() !== null
      timer = setTimeout(() => void refresh(), 200)
    },
    { deep: true, immediate: true },
  )
  onScopeDispose(() => {
    revision++
    controller?.abort()
    clearTimeout(timer)
  })
  onScopeDispose(onBlocksChanged(refresh))
  return { count, items, loading, error, refresh }
}

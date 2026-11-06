import { useAlbums } from '../albums/store'
import { refreshAll } from '../shared/listStore'
import { useLibrary } from './store'

/** Captures shared owners; every completion reads the currently mounted views. */
export function useUndoReconcile(): (changed?: () => void) => Promise<void> {
  const lib = useLibrary()
  const albums = useAlbums()
  return (changed = () => undefined) => {
    const views = refreshAll(lib.reconcileChanges(), albums.load())
    const details = views.catch(() => undefined).then(changed)
    return refreshAll(views, details)
  }
}

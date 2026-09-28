import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { engine } from '../bridge'
import type { Album } from '../bridge'

export const useAlbums = defineStore('albums', () => {
  const list = ref<Album[]>([])
  const loaded = ref(false)
  const byId = computed(() => new Map(list.value.map((album) => [album.id, album])))

  async function load() {
    list.value = await engine.albums()
    loaded.value = true
  }

  return { list, loaded, byId, load }
})

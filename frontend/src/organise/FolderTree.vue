<script setup lang="ts">
// The folders a set of destinations make, as a tree the user can open and
// close: the example of a scheme being written, or every folder a preview
// would fill. File names are left out; the list beside it shows them.
import { computed, ref, watch } from 'vue'
import UTree from '@nuxt/ui/components/Tree.vue'
import { icons } from '../design/icons'

const props = defineProps<{
  /** Destinations, with either kind of slash; the last name of each is the file. */
  paths: string[]
  label: string
}>()
defineSlots<{ default?: () => unknown }>()

interface Folder {
  label: string
  value: string
  icon: string
  children: Folder[]
}
// A large tree only keeps the rows in view; a small one sizes to its content.
const virtualAbove = 100
const tree = computed(() => {
  const roots: Folder[] = []
  const nodes = new Map<string, Folder>()
  for (const path of props.paths) {
    const parts = path.split(/[\\/]/).filter(Boolean).slice(0, -1)
    let siblings = roots
    let key = ''
    for (const label of parts) {
      key += `/${label}`
      let node = nodes.get(key)
      if (!node) {
        node = { label, value: key, icon: icons.folder, children: [] }
        nodes.set(key, node)
        siblings.push(node)
      }
      siblings = node.children
    }
  }
  return { roots, size: nodes.size, keys: new Set(nodes.keys()) }
})

// The first branch opens all the way down, so the depth of the scheme shows
// at once; the user's own choices are kept as the paths change underneath.
const expanded = ref<string[]>([])
let known = new Set<string>()
watch(
  tree,
  ({ roots, keys }) => {
    const open = new Set(expanded.value.filter((key) => keys.has(key)))
    for (let node = roots[0]; node; node = node.children[0]) if (!known.has(node.value)) open.add(node.value)
    for (const root of roots) if (!known.has(root.value)) open.add(root.value)
    expanded.value = [...open]
    known = keys
  },
  { immediate: true },
)
</script>

<template>
  <section class="folder-tree" :aria-label="label">
    <h3>{{ label }}</h3>
    <UTree
      v-model:expanded="expanded"
      :items="tree.roots"
      :get-key="(item) => item.value"
      :aria-label="label"
      :virtualize="tree.size > virtualAbove ? { estimateSize: 32 } : false"
      class="tree"
      :class="{ fixed: tree.size > virtualAbove }"
    />
    <p v-if="$slots.default" class="caption"><slot /></p>
  </section>
</template>

<style scoped>
.folder-tree {
  padding: 0.75rem;
  border: 1px solid var(--ui-border);
  border-radius: var(--lumen-r-lg);
  background: var(--lumen-bg-sunken);
  min-width: 0;
}
h3 {
  margin: 0.25rem 0.5rem 0.5rem;
  color: var(--ui-text-dimmed);
  font-size: 0.6875rem;
  font-weight: 500;
  text-transform: uppercase;
  letter-spacing: 0.06em;
}
.tree {
  max-height: 16rem;
  min-width: 0;
  overflow: auto;
}
.tree.fixed {
  height: 16rem;
}
.tree :deep(button) {
  min-height: 2rem;
}
.caption {
  margin: 0.5rem 0.5rem 0.25rem;
  color: var(--ui-text-dimmed);
  font-size: 0.75rem;
  line-height: 1.5;
}
</style>

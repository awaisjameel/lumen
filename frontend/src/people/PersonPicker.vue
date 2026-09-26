<script setup lang="ts">
// A name box that suggests people already named: pick one, or type a new
// name and press Enter. Emits the name either way (the engine finds or
// creates the person).
import { computed, ref, useId, watch } from 'vue'
import { engine } from '../bridge'
import type { Person } from '../bridge'
import { logError } from '../shared/errors'
import FaceThumb from './FaceThumb.vue'

const props = defineProps<{ placeholder: string; exclude?: number }>()
const emit = defineEmits<{ pick: [name: string] }>()

const id = useId()
const text = ref('')
const open = ref(false)
const found = ref<Person[]>([])
const active = ref(-1) // -1 = the typed text

const options = computed(() => found.value.filter((p) => p.id !== props.exclude))

let asked = 0
watch(text, async (v) => {
  active.value = -1
  const n = ++asked
  if (!v.trim()) {
    found.value = []
    return
  }
  try {
    const got = await engine.suggestPeople(v)
    if (n === asked) found.value = got
  } catch (e) {
    logError(e)
  }
})

function pick(name: string) {
  name = name.trim()
  if (!name) return
  emit('pick', name)
  text.value = ''
  open.value = false
}

function onKey(e: KeyboardEvent) {
  const n = options.value.length
  if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
    e.preventDefault()
    open.value = true
    if (n) active.value = e.key === 'ArrowDown' ? (active.value + 1) % n : (active.value - 1 + n) % n
  } else if (e.key === 'Enter') {
    e.preventDefault()
    pick(active.value >= 0 ? options.value[active.value].name : text.value)
  } else if (e.key === 'Escape' && open.value) {
    e.stopPropagation()
    open.value = false
  }
}
</script>

<template>
  <div class="picker" @focusout="open = false">
    <input
      v-model="text"
      role="combobox"
      aria-autocomplete="list"
      :aria-controls="`${id}-list`"
      :aria-expanded="open && options.length > 0"
      :aria-activedescendant="open && active >= 0 ? `${id}-${active}` : undefined"
      :aria-label="placeholder"
      :placeholder="placeholder"
      maxlength="60"
      spellcheck="false"
      autocomplete="off"
      @focus="open = true"
      @input="open = true"
      @keydown="onKey"
    />
    <ul v-if="open && options.length" :id="`${id}-list`" class="menu" role="listbox" @mousedown.prevent>
      <li
        v-for="(p, i) in options"
        :id="`${id}-${i}`"
        :key="p.id"
        role="option"
        :aria-selected="active === i"
        :class="{ active: active === i }"
        @mouseenter="active = i"
        @click="pick(p.name)"
      >
        <FaceThumb :face="p.cover" :size="24" round />
        <span class="name">{{ p.name }}</span>
      </li>
    </ul>
  </div>
</template>

<style scoped>
.picker {
  position: relative;
}
input {
  width: 100%;
  height: 30px;
  padding: 0 10px;
  border: 1px solid var(--border);
  border-radius: var(--radius-s);
  background: var(--surface-2);
  color: var(--text);
  font-size: 12px;
  outline: none;
}
input:focus {
  border-color: var(--accent);
}
.menu {
  position: absolute;
  top: 34px;
  left: 0;
  right: 0;
  z-index: 30;
  margin: 0;
  padding: 4px;
  list-style: none;
  border: 1px solid var(--border);
  border-radius: var(--radius-m);
  background: var(--surface);
  box-shadow: var(--shadow);
}
.menu li {
  display: flex;
  align-items: center;
  gap: 8px;
  height: 32px;
  padding: 0 8px;
  border-radius: var(--radius-s);
  cursor: pointer;
}
.menu li.active {
  background: var(--surface-hover);
}
.name {
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}
</style>

<script setup lang="ts">
import { t } from './i18n'

withDefaults(defineProps<{ decorative?: boolean }>(), { decorative: true })
</script>

<template>
  <img class="brand-mark" src="/lumen.png" :alt="decorative ? '' : t('app.name')" width="128" height="128" />
</template>

<style scoped>
.brand-mark {
  display: block;
  flex-shrink: 0;
  width: 1.5rem;
  height: 1.5rem;
  object-fit: contain;
}
</style>

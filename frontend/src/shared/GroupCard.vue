<script setup lang="ts">
import { RouterLink } from 'vue-router'
import { thumbUrl } from '../bridge'
import { t } from './i18n'
import Icon from './Icon.vue'
import type { IconName } from './Icon.vue'

defineProps<{
  id: number
  route: 'place' | 'event'
  title: string
  subtitle?: string
  count: number
  cover: number
  coverThumb: number
  icon: IconName
}>()
</script>

<template>
  <article class="group-card">
    <RouterLink :to="{ name: route, params: { id } }" class="image" :aria-label="title">
      <img v-if="cover && coverThumb" :src="thumbUrl(cover, coverThumb)" alt="" loading="lazy" />
      <Icon v-else :name="icon" :size="40" />
    </RouterLink>
    <div class="text">
      <RouterLink :to="{ name: route, params: { id } }" class="name">{{ title }}</RouterLink>
      <span v-if="subtitle" class="subtitle">{{ subtitle }}</span>
      <span class="count">{{ t('status.items', { n: count }) }}</span>
    </div>
    <slot />
  </article>
</template>

<style scoped>
.group-card {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 14px;
  min-width: 0;
  padding: 10px;
  border: 1px solid var(--border);
  border-radius: var(--radius-l);
  background: var(--surface-2);
}

.image {
  display: grid;
  place-items: center;
  flex: 0 0 88px;
  width: 88px;
  height: 88px;
  overflow: hidden;
  border-radius: var(--radius-m);
  color: var(--text-3);
  background: var(--surface-hover);
}

.image img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.text {
  display: flex;
  flex: 1;
  min-width: 0;
  flex-direction: column;
  gap: 3px;
}

.name {
  overflow: hidden;
  color: var(--text);
  font-weight: 600;
  text-decoration: none;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.name:hover {
  text-decoration: underline;
}

.subtitle,
.count {
  color: var(--text-2);
  font-size: 13px;
}

.subtitle {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>

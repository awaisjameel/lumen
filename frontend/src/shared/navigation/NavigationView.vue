<script setup lang="ts">
import UButton from '@nuxt/ui/components/Button.vue'
import { RouterView } from 'vue-router'
import FeatureState from '../FeatureState.vue'
import { t } from '../i18n'
import { destination } from './contract'
import { useNavigation } from './router'

const navigation = useNavigation()
const location = navigation.location
</script>

<template>
  <FeatureState
    v-if="location.kind === 'invalid'"
    kind="error"
    :title="t('navigation.unavailable')"
    :text="t('navigation.unavailableText')"
  >
    <UButton color="neutral" variant="outline" @click="navigation.replace(destination({ name: 'library' }))">
      {{ t('nav.library') }}
    </UButton>
  </FeatureState>
  <RouterView v-else-if="location.kind === 'valid'" />
</template>

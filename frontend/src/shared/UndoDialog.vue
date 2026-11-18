<script setup lang="ts">
import { computed } from 'vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import OutcomeList from './OutcomeList.vue'
import WorkProgress from './WorkProgress.vue'
import { t } from './i18n'
import { undoCaption, undoOutcomes } from './undo'

const open = computed({
  get: () => undoOutcomes.value !== null,
  set: (value) => {
    if (!value) undoOutcomes.value = null
  },
})
</script>

<template>
  <UModal v-model:open="open" :title="undoOutcomes?.title ?? t('undo.outcomes')" :description="t('undo.description')">
    <template v-if="undoOutcomes" #body>
      <WorkProgress
        :label="undoCaption(undoOutcomes)"
        :done="undoOutcomes.total - undoOutcomes.counts.pending"
        :total="undoOutcomes.total"
        :failed="!!undoOutcomes.counts.failed"
      />
      <UAlert
        v-if="undoOutcomes.refreshError"
        color="error"
        :title="t('undo.refreshFailed', { error: undoOutcomes.refreshError })"
      />
      <UAlert
        v-if="!undoOutcomes.available && (undoOutcomes.counts.failed || undoOutcomes.counts.cancelled)"
        color="warning"
        :title="t('undo.expired')"
      />
      <OutcomeList :key="undoOutcomes.id" v-slot="{ item }" :items="undoOutcomes.outcomes">
        <p :key="item.id" data-undo-item>
          {{ item.label ?? item.id }}: {{ t(`undo.state.${item.state}`) }} {{ item.error }}
        </p>
      </OutcomeList>
    </template>
    <template v-if="undoOutcomes" #footer>
      <div class="flex w-full flex-wrap justify-end gap-2">
        <UButton
          v-if="undoOutcomes.busy"
          :disabled="undoOutcomes.stopping"
          color="neutral"
          variant="outline"
          @click="undoOutcomes.cancel()"
          >{{ t('files.cancelRemaining') }}</UButton
        >
        <template v-else>
          <UButton
            v-if="undoOutcomes.counts.failed"
            :disabled="!undoOutcomes.available"
            @click="undoOutcomes.retry()"
            >{{ t('undo.retry') }}</UButton
          >
          <UButton
            v-if="undoOutcomes.counts.cancelled"
            :disabled="!undoOutcomes.available"
            @click="undoOutcomes.resume()"
            >{{ t('undo.resume') }}</UButton
          >
          <UButton
            v-if="undoOutcomes.refreshError"
            :disabled="!undoOutcomes.available"
            @click="undoOutcomes.refresh()"
            >{{ t('undo.refresh') }}</UButton
          >
        </template>
        <UButton color="neutral" variant="ghost" @click="open = false">{{ t('common.close') }}</UButton>
      </div>
    </template>
  </UModal>
</template>

import { computed, ref } from 'vue'
import type { Ref } from 'vue'
import { t } from './i18n'

/**
 * Hiding for a list of cards (People, Things): the items to show, and a
 * toggle that shows the hidden ones too, labelled with how many there are.
 */
export function useHidden<T extends { hidden: boolean }>(list: Readonly<Ref<readonly T[]>>) {
  const showHidden = ref(false)
  const hiddenCount = computed(() => list.value.filter((x) => x.hidden).length)
  const shown = computed(() => list.value.filter((x) => !x.hidden || showHidden.value))
  const toggleLabel = computed(() =>
    showHidden.value ? t('common.hideHidden') : t('common.showHidden', { n: hiddenCount.value }),
  )
  return { showHidden, hiddenCount, shown, toggleLabel }
}

import { shallowRef } from 'vue'
import { useRouter } from 'vue-router'
import { CompressionMode, CompressionEffort } from '../bridge'
import type { CompressionInput, Selection } from '../bridge'
import type { Command } from './commands'
import { t } from './i18n'

export interface CompressionRequest {
  inputs: CompressionInput[]
  selection: Selection | null
}

/** The selection handed to the tools page; it carries no durable history. */
export const compressionRequest = shallowRef<CompressionRequest | null>(null)

export const compressionModes = [
  CompressionMode.CompressCompatible,
  CompressionMode.CompressReversible,
  CompressionMode.CompressConvert,
] as const
export const compressionEfforts = [
  CompressionEffort.CompressFast,
  CompressionEffort.CompressBalanced,
  CompressionEffort.CompressMaximum,
] as const

/** One action definition, shared by the grid, inspector and viewer. */
export function compressionCommand(
  id: string,
  available: () => boolean,
  inputs: () => CompressionInput[],
  zone?: string,
  selection?: () => Selection | null,
): Command {
  const router = useRouter()
  return {
    id,
    group: 'media',
    title: () => t('compression.action'),
    icon: 'files',
    zone,
    available,
    run: async () => {
      compressionRequest.value = { inputs: inputs().map((input) => ({ ...input })), selection: selection?.() ?? null }
      await router.push('/compression')
    },
  }
}

// Why a file was left out of a move, a rename or an export, in words.
import { OrganiseReason } from '../bridge'
import { t } from './i18n'
import type { MessageKey } from './i18n'

const reasonKey: Record<OrganiseReason, MessageKey> = {
  [OrganiseReason.Missing]: 'organise.reason.missing',
  [OrganiseReason.NotVisible]: 'organise.reason.not_visible',
  [OrganiseReason.BlockedFolder]: 'organise.reason.blocked_folder',
  [OrganiseReason.Changed]: 'organise.reason.changed',
  [OrganiseReason.Taken]: 'organise.reason.taken',
  [OrganiseReason.TooLong]: 'organise.reason.too_long',
  [OrganiseReason.NotAFolder]: 'organise.reason.not_a_folder',
  [OrganiseReason.SidecarTaken]: 'organise.reason.sidecar_taken',
  [OrganiseReason.Unsupported]: 'organise.reason.unsupported',
  [OrganiseReason.Interrupted]: 'organise.reason.interrupted',
}

const isReason = (error: string): error is OrganiseReason => Object.hasOwn(reasonKey, error)

/** The error of a journal row as the user reads it: Windows's own message when it is not one of ours. */
export function reasonText(error = ''): string {
  return isReason(error) ? t(reasonKey[error]) : error
}

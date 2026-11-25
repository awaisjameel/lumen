import { shallowRef } from 'vue'

/** A question waiting for the user (shown by ConfirmDialog in App). */
export interface Question {
  title: string
  text: string
  ok: string // the confirming button's label
  /** It removes something for good: the confirming button is red. */
  danger: boolean
  answer: (yes: boolean) => void
  /** A second way to go on, beside ok (see choose). */
  other?: { label: string; choose: () => void }
}

export const question = shallowRef<Question | null>(null)

/**
 * Ask the user to confirm something. Resolves false if they decline or
 * dismiss it. Ask only when it cannot be undone; name the object in the
 * title and the action in ok ("Delete album", never "OK").
 */
export function confirm(title: string, text: string, ok: string, opts: { danger?: boolean } = {}): Promise<boolean> {
  question.value?.answer(false) // one question at a time
  return new Promise((resolve) => {
    question.value = {
      title,
      text,
      ok,
      danger: opts.danger ?? false,
      answer: (yes) => {
        question.value = null
        resolve(yes)
      },
    }
  })
}

/**
 * Ask the user to choose between two ways to go on ("Keep Friend" or "Keep
 * Brother"). Resolves 'ok' or 'other', or null if they decline or dismiss it.
 */
export function choose(title: string, text: string, ok: string, other: string): Promise<'ok' | 'other' | null> {
  question.value?.answer(false) // one question at a time
  return new Promise((resolve) => {
    const settle = (choice: 'ok' | 'other' | null) => {
      question.value = null
      resolve(choice)
    }
    question.value = {
      title,
      text,
      ok,
      danger: false,
      answer: (yes) => settle(yes ? 'ok' : null),
      other: { label: other, choose: () => settle('other') },
    }
  })
}

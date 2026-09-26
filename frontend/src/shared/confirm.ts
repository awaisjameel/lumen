import { shallowRef } from 'vue'

/** A question waiting for the user (shown by ConfirmDialog in App). */
export interface Question {
  title: string
  text: string
  ok: string // the confirming button's label
  answer: (yes: boolean) => void
}

export const question = shallowRef<Question | null>(null)

/** Ask the user to confirm something. Resolves false if they decline or dismiss it. */
export function confirm(title: string, text: string, ok: string): Promise<boolean> {
  question.value?.answer(false) // one question at a time
  return new Promise((resolve) => {
    question.value = {
      title,
      text,
      ok,
      answer: (yes) => {
        question.value = null
        resolve(yes)
      },
    }
  })
}

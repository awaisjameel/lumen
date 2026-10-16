import { expect, it } from 'vitest'
import { defineComponent, h, provide, ref } from 'vue'
import { mountInApp } from './testing'
import { t } from './i18n'
import PrefRow from './PrefRow.vue'

it('labels its control and marks modified settings without relying on colour', () => {
  const wrapper = mountInApp(PrefRow, {
    props: { label: 'Theme', description: 'Follow Windows.', modified: true },
    slots: { default: () => h('input', { 'aria-label': 'Theme' }) },
  })
  expect(wrapper.get('[data-pref="Theme"]').text()).toContain('Follow Windows.')
  expect(wrapper.get(`[aria-label="${t('settings.modified')}"]`).attributes('title')).toBe(t('settings.modified'))
  wrapper.unmount()
})

it('highlights the current search and keeps failures separate from save hints', async () => {
  const query = ref('theme')
  const Host = defineComponent({
    setup() {
      provide('settings-query', query)
      return () => h(PrefRow, { label: 'Theme', description: 'Choose a theme.', error: 'Try again.', hint: 'Saved' })
    },
  })
  const wrapper = mountInApp(Host)
  expect(wrapper.findAll('mark').map((mark) => mark.text())).toEqual(['Theme', 'theme'])
  expect(wrapper.get('[role="alert"]').text()).toBe('Try again.')
  expect(wrapper.text()).not.toContain('Saved')
  query.value = ''
  await wrapper.vm.$nextTick()
  expect(wrapper.find('mark').exists()).toBe(false)
  wrapper.unmount()
})

import { expect, it } from 'vitest'
import { defineComponent, h, nextTick, shallowRef } from 'vue'
import { mountInApp } from '../shared/testing'
import { t } from '../shared/i18n'
import OutcomeList from './OutcomeList.vue'

it('shows a long list a page at a time and returns to the first page when it shrinks', async () => {
  const items = shallowRef(Array.from({ length: 150 }, (_, i) => i))
  const Host = defineComponent({
    render: () =>
      h(
        OutcomeList<number>,
        { items: items.value },
        { default: ({ item }: { item: number }) => h('p', { class: 'row' }, String(item)) },
      ),
  })
  const wrapper = mountInApp(Host)
  try {
    expect(wrapper.findAll('.row')).toHaveLength(100)
    const next = wrapper.findAll('button').find((button) => button.text() === t('files.nextPage'))
    await next?.trigger('click')
    expect(wrapper.findAll('.row').map((row) => row.text())[0]).toBe('100')
    expect(wrapper.findAll('.row')).toHaveLength(50)
    items.value = items.value.slice(0, 3)
    await nextTick()
    await nextTick()
    expect(wrapper.findAll('.row')).toHaveLength(3)
    expect(wrapper.findAll('button')).toHaveLength(0)
  } finally {
    wrapper.unmount()
  }
})

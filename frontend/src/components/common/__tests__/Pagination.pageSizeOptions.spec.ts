import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import Pagination from '../Pagination.vue'
import Select from '../Select.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

describe('Pagination page size options', () => {
  beforeEach(() => {
    window.__APP_CONFIG__ = undefined
    localStorage.clear()
  })

  it('uses the explicit options exactly and emits 1000 unchanged', async () => {
    const wrapper = mount(Pagination, {
      props: {
        total: 2500,
        page: 1,
        pageSize: 20,
        pageSizeOptions: [20, 100, 1000]
      }
    })

    const select = wrapper.findComponent(Select)
    expect(select.props('options')).toEqual([
      { value: 20, label: '20' },
      { value: 100, label: '100' },
      { value: 1000, label: '1000' }
    ])

    await select.vm.$emit('update:modelValue', 1000)
    expect(wrapper.emitted('update:pageSize')).toEqual([[1000]])
  })

  it('does not leak globally configured values into an explicit list', () => {
    window.__APP_CONFIG__ = {
      table_page_size_options: [10, 20, 50, 100]
    } as typeof window.__APP_CONFIG__

    const wrapper = mount(Pagination, {
      props: {
        total: 48,
        page: 1,
        pageSize: 12,
        pageSizeOptions: [12, 24]
      }
    })

    expect(wrapper.findComponent(Select).props('options')).toEqual([
      { value: 12, label: '12' },
      { value: 24, label: '24' }
    ])
  })
})

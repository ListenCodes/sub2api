import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import AccountMonitorFilters from '@/views/admin/account-monitor/AccountMonitorFilters.vue'
import { parseAccountMonitorQuery } from '@/views/admin/account-monitor/useAccountMonitorFilters'

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key }),
}))

describe('AccountMonitorFilters', () => {
  it('applies custom dates as soon as the input is committed', async () => {
    const wrapper = mount(AccountMonitorFilters, {
      props: {
        state: {
          ...parseAccountMonitorQuery({}),
          range: 'custom',
          from: '2026-07-01T00:00:00.000Z',
          to: '2026-07-02T00:00:00.000Z',
        },
        groups: [],
      },
      global: { stubs: { Icon: true } },
    })

    const inputs = wrapper.findAll('input[type="datetime-local"]')
    await inputs[1].setValue('2026-07-03T00:00')

    expect(wrapper.emitted('apply')?.at(-1)?.[0]).toMatchObject({
      range: 'custom',
      to: new Date('2026-07-03T00:00').toISOString(),
    })
  })
})

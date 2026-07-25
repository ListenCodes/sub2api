import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import AccountMonitorFilters from '@/views/admin/account-monitor/AccountMonitorFilters.vue'
import { parseAccountMonitorQuery } from '@/views/admin/account-monitor/useAccountMonitorFilters'

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key }),
}))

describe('AccountMonitorFilters', () => {
	it('shows the identity of every filter before a value is chosen', () => {
		const wrapper = mount(AccountMonitorFilters, {
			props: { state: parseAccountMonitorQuery({}), groups: [] },
			global: { stubs: { Icon: true } },
		})

		for (const [testID, label] of [
			['account-filter-range-label', '时间范围'],
			['account-filter-query-label', '账号'],
			['account-filter-platform-label', '平台'],
			['account-filter-group-label', '分组'],
			['account-filter-model-label', '实际模型'],
			['account-filter-status-label', '账号状态'],
			['account-filter-result-label', '调用结果'],
			['account-filter-rollup-label', '账号口径'],
			['account-filter-risk-label', '风险分'],
		] as const) {
			expect(wrapper.get(`[data-testid="${testID}"]`).text()).toBe(label)
		}
	})

	it('debounces account name or identity searches into the existing apply event', async () => {
		vi.useFakeTimers()
		try {
			const wrapper = mount(AccountMonitorFilters, {
				props: { state: parseAccountMonitorQuery({}), groups: [] },
				global: { stubs: { Icon: true } },
			})
			const input = wrapper.get('input[placeholder="搜索账号名称或实际账号"]')
			await input.setValue('owner@example.com')
			await vi.advanceTimersByTimeAsync(350)

			expect(wrapper.emitted('apply')?.at(-1)?.[0]).toMatchObject({ query: 'owner@example.com' })
		} finally {
			vi.useRealTimers()
		}
	})

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

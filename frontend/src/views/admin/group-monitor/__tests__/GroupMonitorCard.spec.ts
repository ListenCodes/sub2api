import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import GroupMonitorCard from '@/views/admin/group-monitor/GroupMonitorCard.vue'

const base = {
  group_id: 7, name: 'OpenAI Primary', platform: 'openai', group_status: 'active',
  total_requests: 15, successes: 12, failures: 3, success_rate: 0.8,
  timeline: [
    { bucket_at: '2026-07-15T06:00:00Z', total: 5, successes: 5, failures: 0, status: 'normal' as const },
    { bucket_at: '2026-07-15T06:10:00Z', total: 10, successes: 7, failures: 3, status: 'partial_failure' as const },
    { bucket_at: '2026-07-15T06:20:00Z', total: 0, successes: 0, failures: 0, status: 'no_data' as const },
  ],
}

describe('GroupMonitorCard', () => {
  it.each([
    ['normal', '正常'], ['partial_failure', '部分失败'], ['all_failed', '全部失败'],
    ['recently_idle', '近期空闲'], ['no_data', '无调用'],
  ])('renders call state %s as %s', (callStatus, label) => {
    const wrapper = mount(GroupMonitorCard, { props: { group: { ...base, call_status: callStatus as typeof base.call_status } } })
    expect(wrapper.text()).toContain(label)
  })

  it('uses volume for bar height, semantic state colors, and no request identity text', () => {
		const wrapper = mount(GroupMonitorCard, { props: { group: { ...base, call_status: 'partial_failure' }, bucketSeconds: 3600 } })
    const bars = wrapper.findAll('[data-testid="group-timeline-bar"]')

    expect(bars[0].attributes('style')).toContain('50%')
    expect(bars[1].attributes('style')).toContain('100%')
    expect(bars[0].classes()).toContain('bg-emerald-500')
    expect(bars[1].classes()).toContain('bg-amber-500')
    expect(wrapper.text()).not.toContain('account_id')
    expect(wrapper.text()).not.toContain('provider_error')
		expect(wrapper.get('h3').classes()).toEqual(expect.arrayContaining(['text-base', 'font-semibold']))
		expect(wrapper.find('[data-platform="openai"]').exists()).toBe(true)
		expect(wrapper.get('[role="img"]').attributes('aria-label')).toContain('1 小时')
  })
})

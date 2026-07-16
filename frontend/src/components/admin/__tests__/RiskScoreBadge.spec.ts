import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import RiskScoreBadge from '@/components/admin/RiskScoreBadge.vue'

describe('RiskScoreBadge', () => {
  it.each([
    [0, '正常'],
    [19, '正常'],
    [20, '关注'],
    [39, '关注'],
    [40, '异常'],
    [69, '异常'],
    [70, '严重'],
    [100, '严重'],
  ])('maps score %i to %s', (score, label) => {
    const wrapper = mount(RiskScoreBadge, { props: { score, available: true } })

    expect(wrapper.text()).toContain(String(score))
    expect(wrapper.text()).toContain(label)
    expect(wrapper.attributes('aria-label')).toBe(`风险分 ${score}，${label}`)
  })

  it.each([
    ['low', '低风险'],
    ['medium', '中风险'],
    ['high', '高风险'],
    ['critical', '严重风险'],
  ])('keeps the user-risk level %s compatible', (explicitLevel, label) => {
    const wrapper = mount(RiskScoreBadge, {
      props: { score: 72, available: true, explicitLevel },
    })

    expect(wrapper.text()).toContain(label)
    expect(wrapper.attributes('aria-label')).toBe(`风险分 72，${label}`)
  })

  it('renders an unavailable state without presenting zero as normal', () => {
    const wrapper = mount(RiskScoreBadge, { props: { score: 0, available: false } })

    expect(wrapper.text()).toBe('暂无评分')
    expect(wrapper.text()).not.toContain('正常')
    expect(wrapper.attributes('aria-label')).toBe('暂无评分')
  })
})

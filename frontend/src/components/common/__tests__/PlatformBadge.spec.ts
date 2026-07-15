import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import PlatformBadge from '../PlatformBadge.vue'
import PlatformIcon from '../PlatformIcon.vue'
import * as platformColors from '@/utils/platformColors'

const supported = [
  { value: 'anthropic', label: 'Anthropic' },
  { value: 'openai', label: 'OpenAI' },
  { value: 'gemini', label: 'Gemini' },
  { value: 'antigravity', label: 'Antigravity' },
  { value: 'grok', label: 'Grok' }
]

describe('PlatformBadge', () => {
  it('exports every Sub2API platform in the approved order', () => {
    expect(platformColors.SUPPORTED_PLATFORM_OPTIONS).toEqual(supported)
  })

  for (const platform of supported) {
    it(`renders ${platform.label} with the shared icon and light color classes`, () => {
      const wrapper = mount(PlatformBadge, { props: { platform: platform.value } })

      expect(wrapper.text()).toBe(platform.label)
      expect(wrapper.findComponent(PlatformIcon).props('platform')).toBe(platform.value)
      expect(wrapper.classes()).toEqual(
        expect.arrayContaining(platformColors.platformBadgeLightClass(platform.value).split(' '))
      )
    })
  }

  it('keeps unknown future platforms readable with the neutral fallback', () => {
    const wrapper = mount(PlatformBadge, { props: { platform: 'future-ai' } })

    expect(wrapper.text()).toBe('future-ai')
    expect(wrapper.findComponent(PlatformIcon).exists()).toBe(true)
    expect(wrapper.classes()).toEqual(
      expect.arrayContaining(platformColors.platformBadgeLightClass('future-ai').split(' '))
    )
  })
})

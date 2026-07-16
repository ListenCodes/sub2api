import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import AccountMonitorView from '../AccountMonitorView.vue'
import AccountMonitorPanel from '../account-monitor/AccountMonitorPanel.vue'

describe('AccountMonitorView', () => {
  it('renders the native account monitor without an iframe or static asset URL', () => {
    const wrapper = mount(AccountMonitorView, { shallow: true })

    expect(wrapper.findComponent(AccountMonitorPanel).exists()).toBe(true)
    expect(wrapper.find('iframe').exists()).toBe(false)
    expect(wrapper.html()).not.toContain('/api/v1/extensions-self/account-monitor/')
  })
})

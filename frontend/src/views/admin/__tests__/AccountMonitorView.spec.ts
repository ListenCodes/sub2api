import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import AccountMonitorView from '../AccountMonitorView.vue'

vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key }),
}))

describe('AccountMonitorView', () => {
  const mountView = () => mount(AccountMonitorView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        Icon: true,
      },
    },
  })

  beforeEach(() => {
    vi.stubGlobal('open', vi.fn())
  })

  it('renders only the authenticated same-origin monitor shell', async () => {
    const wrapper = mountView()

    const iframe = wrapper.get('iframe')
    expect(iframe.attributes('src')).toBe('/api/v1/extensions-self/account-monitor/')
    expect(iframe.attributes('title')).toBe('admin.accountMonitor.iframeTitle')
    expect(iframe.attributes('sandbox')).toContain('allow-scripts')
    expect(iframe.attributes('sandbox')).toContain('allow-same-origin')
    expect(iframe.attributes('referrerpolicy')).toBe('same-origin')
    expect(wrapper.text()).toContain('common.loading')

    await iframe.trigger('load')
    expect(wrapper.text()).not.toContain('common.loading')
  })

  it('shows a recoverable error when the monitor frame cannot load', async () => {
    const wrapper = mountView()

    await wrapper.get('iframe').trigger('error')
    expect(wrapper.text()).toContain('admin.accountMonitor.loadFailed')

    await wrapper.get('[data-testid="account-monitor-reload"]').trigger('click')
    expect(wrapper.text()).toContain('common.loading')
    expect(wrapper.text()).not.toContain('admin.accountMonitor.loadFailed')
  })

  it('opens the monitor in a separate tab', async () => {
    const wrapper = mountView()
    await wrapper.get('[data-testid="account-monitor-open"]').trigger('click')

    expect(window.open).toHaveBeenCalledWith(
      '/api/v1/extensions-self/account-monitor/',
      '_blank',
      'noopener,noreferrer',
    )
  })
})

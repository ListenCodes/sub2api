import { defineComponent, nextTick } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const {
  createAccountMock,
  createFromSSOMock,
  exchangeCodeMock,
  generateAuthUrlMock,
  probeUpstreamBillingMock,
  refreshGrokTokenMock,
  showErrorMock,
} = vi.hoisted(() => ({
  createAccountMock: vi.fn(),
  createFromSSOMock: vi.fn(),
  exchangeCodeMock: vi.fn(),
  generateAuthUrlMock: vi.fn(),
  probeUpstreamBillingMock: vi.fn(),
  refreshGrokTokenMock: vi.fn(),
  showErrorMock: vi.fn(),
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: showErrorMock,
    showSuccess: vi.fn(),
    showWarning: vi.fn(),
  }),
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ isSimpleMode: true }),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      create: createAccountMock,
      probeUpstreamBilling: probeUpstreamBillingMock,
      checkMixedChannelRisk: vi.fn().mockResolvedValue({ has_risk: false }),
    },
    grok: {
      createFromSSO: createFromSSOMock,
      exchangeCode: exchangeCodeMock,
      generateAuthUrl: generateAuthUrlMock,
      refreshGrokToken: refreshGrokTokenMock,
    },
    settings: {
      getWebSearchEmulationConfig: vi.fn().mockResolvedValue({ enabled: false, providers: [] }),
      getSettings: vi.fn().mockResolvedValue({}),
    },
    tlsFingerprintProfiles: {
      list: vi.fn().mockResolvedValue([]),
    },
  },
}))

vi.mock('@/api/admin/accounts', () => ({
  getAntigravityDefaultModelMapping: vi.fn().mockResolvedValue([]),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

import CreateAccountModal from '../CreateAccountModal.vue'

const BaseDialogStub = defineComponent({
  name: 'BaseDialog',
  props: { show: { type: Boolean, default: false } },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>',
})

const OAuthAuthorizationFlowStub = defineComponent({
  name: 'OAuthAuthorizationFlow',
  props: {
    showEmailPasswordOption: { type: Boolean, default: true },
    showRefreshTokenOption: Boolean,
    showSsoOption: Boolean,
  },
  emits: ['generate-url', 'import-sso', 'validate-refresh-token'],
  data: () => ({
    authCode: '',
    inputMethod: 'manual',
    oauthState: 'oauth-state',
  }),
  methods: {
    reset() {
      this.authCode = ''
      this.inputMethod = 'manual'
      this.oauthState = ''
    },
  },
  template: `
    <div data-testid="grok-oauth-flow">
      <button data-testid="grok-generate-url" @click="$emit('generate-url')">generate</button>
      <button data-testid="grok-import-sso" @click="$emit('import-sso', 'sso-token')">sso</button>
      <button data-testid="grok-validate-rt" @click="$emit('validate-refresh-token', 'refresh-token')">rt</button>
    </div>
  `,
})

const HeaderOverrideEditorStub = defineComponent({
  name: 'HeaderOverrideEditor',
  props: { rows: { type: Array, default: () => [] } },
  emits: ['update:rows'],
  template: `
    <button
      type="button"
      data-testid="grok-header-row"
      @click="$emit('update:rows', [{ name: 'X-Test-Client', value: 'enabled' }])"
    >header</button>
  `,
})

function mountModal() {
  return mount(CreateAccountModal, {
    props: { show: true, proxies: [], groups: [] },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        HeaderOverrideEditor: HeaderOverrideEditorStub,
        OAuthAuthorizationFlow: OAuthAuthorizationFlowStub,
        ConfirmDialog: true,
        Select: true,
        Icon: true,
        PlatformIcon: true,
        ProxySelector: true,
        ProxyAdBanner: true,
        GroupSelector: true,
        ModelWhitelistSelector: true,
        QuotaLimitCard: true,
      },
    },
  })
}

async function selectButtonByText(wrapper: ReturnType<typeof mountModal>, text: string) {
  const button = wrapper.findAll('button').find((candidate) => candidate.text().includes(text))
  expect(button).toBeDefined()
  await button?.trigger('click')
}

async function selectGrok(wrapper: ReturnType<typeof mountModal>) {
  await selectButtonByText(wrapper, 'Grok')
  await nextTick()
}

async function openGrokOAuth(
  wrapper: ReturnType<typeof mountModal>,
  baseUrl = 'https://relay.example/v1',
  withHeaderOverride = false
) {
  await selectGrok(wrapper)
  await wrapper.get('[data-tour="account-form-name"]').setValue('Grok account')
  await wrapper.get('[data-testid="grok-custom-base-url-toggle"]').trigger('click')
  await wrapper.get('[data-testid="grok-custom-base-url-input"]').setValue(baseUrl)
  if (withHeaderOverride) {
    const toggle = wrapper
      .findAll('button')
      .find((button) =>
        button.element.parentElement?.textContent?.includes('admin.accounts.headerOverride.title')
      )
    expect(toggle).toBeDefined()
    await toggle?.trigger('click')
    await wrapper.get('[data-testid="grok-header-row"]').trigger('click')
  }
  await wrapper.get('form#create-account-form').trigger('submit.prevent')
  await nextTick()
}

type OAuthPath = 'authorization-code' | 'refresh-token' | 'sso'

async function triggerOAuthPath(
  wrapper: ReturnType<typeof mountModal>,
  path: OAuthPath
) {
  if (path === 'authorization-code') {
    await wrapper.get('[data-testid="grok-generate-url"]').trigger('click')
    await flushPromises()
    const flow = wrapper.getComponent(OAuthAuthorizationFlowStub)
    flow.vm.authCode = 'authorization-code'
    await nextTick()
    await selectButtonByText(wrapper, 'admin.accounts.oauth.completeAuth')
  } else if (path === 'refresh-token') {
    await wrapper.get('[data-testid="grok-validate-rt"]').trigger('click')
  } else {
    await wrapper.get('[data-testid="grok-import-sso"]').trigger('click')
  }
  await flushPromises()
}

const tokenInfo = {
  access_token: 'access-token',
  refresh_token: 'refresh-token',
  token_type: 'Bearer',
  expires_at: 1_900_000_000,
  email: 'grok@example.com',
}

describe('CreateAccountModal Grok account types', () => {
  beforeEach(() => {
    createAccountMock.mockReset().mockResolvedValue({ id: 42, platform: 'grok', type: 'oauth' })
    createFromSSOMock.mockReset().mockResolvedValue({ created: [{ id: 42 }], failed: [] })
    exchangeCodeMock.mockReset().mockResolvedValue(tokenInfo)
    generateAuthUrlMock.mockReset().mockResolvedValue({
      auth_url: 'https://accounts.x.ai/authorize',
      session_id: 'session-id',
      state: 'oauth-state',
    })
    probeUpstreamBillingMock.mockReset().mockResolvedValue({})
    refreshGrokTokenMock.mockReset().mockResolvedValue(tokenInfo)
    showErrorMock.mockReset()
  })

  it('creates a Grok API-key account with the official xAI defaults', async () => {
    const wrapper = mountModal()
    await selectGrok(wrapper)
    await wrapper.get('[data-testid="grok-account-type-api-key"]').trigger('click')

    const baseUrlInput = wrapper.get('input[placeholder="https://api.x.ai/v1"]')
    const apiKeyInput = wrapper.get('input[placeholder="xai-..."]')
    expect((baseUrlInput.element as HTMLInputElement).value).toBe('https://api.x.ai/v1')

    await wrapper.get('[data-tour="account-form-name"]').setValue('Grok API key')
    await apiKeyInput.setValue('xai-test-key')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    expect(createAccountMock.mock.calls[0]?.[0]).toMatchObject({
      platform: 'grok',
      type: 'apikey',
      credentials: {
        api_key: 'xai-test-key',
        base_url: 'https://api.x.ai/v1',
      },
    })
  })

  it('exposes custom upstream controls and keeps password authorization hidden', async () => {
    const wrapper = mountModal()
    await selectGrok(wrapper)

    expect(wrapper.find('[data-testid="grok-custom-base-url-input"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('admin.accounts.headerOverride.title')
    await wrapper.get('[data-testid="grok-custom-base-url-toggle"]').trigger('click')
    expect(wrapper.find('[data-testid="grok-custom-base-url-input"]').exists()).toBe(true)

    await wrapper.get('[data-tour="account-form-name"]').setValue('Grok OAuth')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    const flow = wrapper.getComponent(OAuthAuthorizationFlowStub)
    expect(flow.props('showRefreshTokenOption')).toBe(true)
    expect(flow.props('showSsoOption')).toBe(true)
    expect(flow.props('showEmailPasswordOption')).toBe(false)
  })

  it.each<OAuthPath>(['authorization-code', 'refresh-token', 'sso'])(
    'rejects an invalid custom URL before the %s path calls its downstream API',
    async (path) => {
      const wrapper = mountModal()
      await openGrokOAuth(wrapper, 'not-a-url')
      await triggerOAuthPath(wrapper, path)

      expect(showErrorMock).toHaveBeenCalledWith('admin.accounts.grokCustomBaseUrl.invalid')
      expect(exchangeCodeMock).not.toHaveBeenCalled()
      expect(refreshGrokTokenMock).not.toHaveBeenCalled()
      expect(createFromSSOMock).not.toHaveBeenCalled()
      expect(createAccountMock).not.toHaveBeenCalled()
    }
  )

  it.each<OAuthPath>(['authorization-code', 'refresh-token', 'sso'])(
    'adds the custom upstream URL to credentials on the %s path',
    async (path) => {
      const wrapper = mountModal()
      await openGrokOAuth(wrapper, 'https://relay.example/v1', path === 'authorization-code')
      await triggerOAuthPath(wrapper, path)

      if (path === 'authorization-code') {
        expect(generateAuthUrlMock).toHaveBeenCalledWith({})
        expect(exchangeCodeMock).toHaveBeenCalledTimes(1)
        expect(exchangeCodeMock).toHaveBeenCalledWith({
          session_id: 'session-id',
          state: 'oauth-state',
          code: 'authorization-code',
        })
        expect(refreshGrokTokenMock).not.toHaveBeenCalled()
        expect(createFromSSOMock).not.toHaveBeenCalled()
        expect(createAccountMock).toHaveBeenCalledTimes(1)
        expect(createAccountMock.mock.calls[0]?.[0]?.credentials).toMatchObject({
          base_url: 'https://relay.example/v1',
          header_override_enabled: true,
          header_overrides: { 'x-test-client': 'enabled' },
        })
      } else if (path === 'refresh-token') {
        expect(refreshGrokTokenMock).toHaveBeenCalledTimes(1)
        expect(refreshGrokTokenMock).toHaveBeenCalledWith('refresh-token', null)
        expect(exchangeCodeMock).not.toHaveBeenCalled()
        expect(createFromSSOMock).not.toHaveBeenCalled()
        expect(createAccountMock).toHaveBeenCalledTimes(1)
        expect(createAccountMock.mock.calls[0]?.[0]?.credentials).toMatchObject({
          base_url: 'https://relay.example/v1',
        })
      } else {
        expect(exchangeCodeMock).not.toHaveBeenCalled()
        expect(refreshGrokTokenMock).not.toHaveBeenCalled()
        expect(createFromSSOMock).toHaveBeenCalledTimes(1)
        expect(createAccountMock).not.toHaveBeenCalled()
        expect(createFromSSOMock.mock.calls[0]?.[0]).toMatchObject({
          sso_tokens: ['sso-token'],
          credentials: { base_url: 'https://relay.example/v1' },
        })
      }
    }
  )
})

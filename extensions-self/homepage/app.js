(() => {
  const fallbackName = 'Sub2API'
  const fallbackSubtitle = 'AI API Gateway Platform'
  const fallbackLogo = '/logo.svg'
  const heroValue = '一个密钥连接多种主流模型。统一管理访问、用量与调度，让 API 接入保持清晰、稳定、可控。'

  function safeText(value, fallback = '') {
    return typeof value === 'string' && value.trim() ? value.trim() : fallback
  }

  function safeLogo(value) {
    const candidate = safeText(value)
    if (candidate.startsWith('data:image/')) return candidate
    if (!candidate) return fallbackLogo
    try {
      const parsed = new URL(candidate, location.origin)
      if (parsed.origin === location.origin && !candidate.startsWith('//')) return parsed.href
    } catch (_) {
      return fallbackLogo
    }
    return fallbackLogo
  }

  function setText(id, value) {
    const node = document.getElementById(id)
    if (node) node.textContent = value
  }

  async function loadBranding() {
    try {
      const response = await fetch('/api/v1/settings/public', { cache: 'no-store', credentials: 'same-origin' })
      if (!response.ok) throw new Error('settings unavailable')
      const settingsPayload = await response.json()
      const settings = settingsPayload && typeof settingsPayload.data === 'object'
        ? settingsPayload.data
        : settingsPayload
      const name = safeText(settings && settings.site_name, fallbackName)
      const subtitle = safeText(settings && settings.site_subtitle, fallbackSubtitle)
      const logo = document.getElementById('site-logo')
      setText('site-name', name)
      setText('site-subtitle', subtitle)
      setText('hero-site-name', name)
      setText('hero-lead', `${subtitle} · ${heroValue}`)
      setText('footer-site-name', name)
      document.title = name
      if (logo) logo.src = safeLogo(settings && settings.site_logo)
    } catch (_) {
      document.title = fallbackName
    }
  }

  function element(tag, className, text) {
    const node = document.createElement(tag)
    if (className) node.className = className
    if (text !== undefined) node.textContent = text
    return node
  }

  function platformLabel(platform) {
    const labels = {
      anthropic: 'Anthropic',
      openai: 'OpenAI',
      gemini: 'Google Gemini',
      antigravity: 'Antigravity',
      grok: 'xAI Grok',
      composite: 'Composite'
    }
    return labels[platform] || platform || '其他平台'
  }

  function formatMultiplier(value) {
    const number = Number(value)
    if (!Number.isFinite(number)) return '--'
    return `${Number(number.toFixed(4))}x`
  }

  function renderGroups(groups) {
    const container = document.getElementById('rate-groups')
    const status = document.getElementById('rate-status')
    if (!container || !status) return
    container.replaceChildren()
    if (!groups.length) {
      status.dataset.state = 'empty'
      status.textContent = '暂无公开分组'
      return
    }
    const grouped = new Map()
    groups.forEach((group) => {
      const platform = safeText(group.platform, 'other').toLowerCase()
      if (!grouped.has(platform)) grouped.set(platform, [])
      grouped.get(platform).push(group)
    })
    grouped.forEach((items, platform) => {
      const card = element('article', 'hp-rate-group')
      const head = element('header', 'hp-rate-head')
      head.append(element('h3', '', platformLabel(platform)), element('span', 'hp-platform', `${items.length} 个分组`))
      card.append(head)
      items.forEach((group) => {
        const row = element('div', 'hp-rate-row')
        row.append(element('span', 'hp-rate-name', safeText(group.name, '未命名分组')))
        row.append(element('span', 'hp-rate-value', formatMultiplier(group.rate_multiplier)))
        if (group.peak_rate_enabled && group.peak_start && group.peak_end) {
          row.append(element('span', 'hp-peak', `高峰 ${group.peak_start}-${group.peak_end} · ${formatMultiplier(group.peak_rate_multiplier)}`))
        }
        card.append(row)
      })
      container.append(card)
    })
    status.dataset.state = 'ready'
    status.textContent = `实时数据 · ${groups.length} 个公开分组`
  }

  async function loadGroups() {
    const status = document.getElementById('rate-status')
    try {
      const response = await fetch('api/public-groups', { cache: 'no-store', credentials: 'same-origin' })
      if (!response.ok) throw new Error('groups unavailable')
      const payload = await response.json()
      renderGroups(Array.isArray(payload.groups) ? payload.groups : [])
    } catch (_) {
      if (status) {
        status.dataset.state = 'error'
        status.textContent = '倍率暂时不可用'
      }
    }
  }

  const logo = document.getElementById('site-logo')
  if (logo) logo.addEventListener('error', () => {
    if (!logo.src.endsWith(fallbackLogo)) logo.src = fallbackLogo
  })
  document.addEventListener('DOMContentLoaded', () => {
    loadBranding()
    loadGroups()
  })
})()

(() => {
  const fallbackName = 'Sub2API'
  const fallbackSubtitle = '一个密钥连接多种主流模型。统一管理访问、用量与调度，让 API 接入保持清晰、稳定、可控。'
  const fallbackLogo = '/logo.svg'

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

  function element(tag, className, text) {
    const node = document.createElement(tag)
    if (className) node.className = className
    if (text !== undefined) node.textContent = text
    return node
  }

  function platformKey(platform) {
    const value = safeText(platform, 'other').toLowerCase()
    if (value === 'anthropic' || value === 'openai' || value === 'gemini' || value === 'grok' || value === 'antigravity' || value === 'composite') {
      return value
    }
    return 'other'
  }

  function platformLabel(platform) {
    const labels = {
      anthropic: 'Anthropic',
      openai: 'OpenAI',
      gemini: 'Gemini',
      grok: 'Grok',
      antigravity: 'Antigravity',
      composite: 'Composite',
      other: '其他'
    }
    const key = platformKey(platform)
    if (key !== 'other') return labels[key]
    return safeText(platform, '其他')
  }

  function seriesTitle(platform) {
    const key = platformKey(platform)
    if (key === 'other') return '其他系列'
    return `${platformLabel(platform)} 系列`
  }

  function formatMultiplier(value) {
    const number = Number(value)
    if (!Number.isFinite(number)) return '--'
    return `${Number(number.toFixed(4))}x`
  }

  function rateTone(value) {
    const number = Number(value)
    if (!Number.isFinite(number)) return 'mid'
    if (number < 0.5) return 'low'
    if (number <= 1) return 'mid'
    return 'high'
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
      // Nav keeps the configured short subtitle; Hero lead is the fixed product value line
      // so it does not collapse into a second brand slogan or repeat the H1 site name.
      const navSubtitle = safeText(settings && settings.site_subtitle, 'AI API Gateway Platform')
      const logo = document.getElementById('site-logo')
      setText('site-name', name)
      setText('site-subtitle', navSubtitle)
      setText('hero-site-name', name)
      setText('hero-lead', fallbackSubtitle)
      setText('footer-site-name', name)
      document.title = name
      if (logo) logo.src = safeLogo(settings && settings.site_logo)
    } catch (_) {
      document.title = fallbackName
      setText('hero-lead', fallbackSubtitle)
    }
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
      const key = platformKey(group && group.platform)
      if (!grouped.has(key)) grouped.set(key, [])
      grouped.get(key).push(group)
    })

    const order = ['anthropic', 'openai', 'gemini', 'grok', 'antigravity', 'composite', 'other']
    const keys = [
      ...order.filter((key) => grouped.has(key)),
      ...Array.from(grouped.keys()).filter((key) => !order.includes(key))
    ]

    keys.forEach((key) => {
      const items = grouped.get(key) || []
      const card = element('article', 'hp-rc')
      const head = element('header', 'hp-rc-head')
      head.append(
        element('h3', 'hp-rc-title', seriesTitle(key === 'other' && items[0] ? items[0].platform : key)),
        element('span', `hp-rc-platform ${key}`, platformLabel(key === 'other' && items[0] ? items[0].platform : key))
      )
      card.append(head)

      const body = element('div', 'hp-rc-body')
      const formula = element('div', 'hp-rc-formula')
      formula.append(element('strong', '', '计算方式：'), document.createTextNode('实际扣费 = 官方价 × 分组倍率'))
      body.append(formula)

      items.forEach((group) => {
        const row = element('div', 'hp-rc-row')
        row.append(element('span', 'hp-rc-name', safeText(group && group.name, '未命名分组')))
        row.append(element(
          'span',
          `hp-rc-rate ${rateTone(group && group.rate_multiplier)}`,
          `${formatMultiplier(group && group.rate_multiplier)} 实际`
        ))
        if (group && group.peak_rate_enabled && group.peak_start && group.peak_end) {
          row.append(element(
            'span',
            'hp-rc-peak',
            `高峰 ${group.peak_start}-${group.peak_end} · ${formatMultiplier(group.peak_rate_multiplier)}`
          ))
        }
        body.append(row)
      })

      card.append(body)
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

  function resolveTheme() {
    // Follow Sub when user has an explicit choice in localStorage.theme.
    // Homepage default is light when no saved preference (product preference).
    try {
      const savedTheme = localStorage.getItem('theme')
      if (savedTheme === 'dark') return 'dark'
      if (savedTheme === 'light') return 'light'
    } catch (_) {
      // private mode / blocked storage
    }
    return 'light'
  }

  function applyTheme(theme) {
    const isDark = theme === 'dark'
    document.documentElement.classList.toggle('dark', isDark)
    document.documentElement.style.colorScheme = isDark ? 'dark' : 'light'
  }

  function syncThemeFromSub() {
    applyTheme(resolveTheme())
  }

  function initThemeFollowSub() {
    syncThemeFromSub()

    window.addEventListener('storage', (event) => {
      if (event.key === 'theme' || event.key === null) syncThemeFromSub()
    })

    try {
      const media = window.matchMedia('(prefers-color-scheme: dark)')
      const onSchemeChange = () => {
        try {
          if (!localStorage.getItem('theme')) syncThemeFromSub()
        } catch (_) {
          syncThemeFromSub()
        }
      }
      if (typeof media.addEventListener === 'function') media.addEventListener('change', onSchemeChange)
      else if (typeof media.addListener === 'function') media.addListener(onSchemeChange)
    } catch (_) {
      // ignore
    }

    // Parent SPA may toggle theme in the same page without a storage event for this frame
    // in some browsers; poll lightly while visible.
    let last = resolveTheme()
    window.setInterval(() => {
      const next = resolveTheme()
      if (next !== last) {
        last = next
        applyTheme(next)
      }
    }, 1000)
  }

  const logo = document.getElementById('site-logo')
  if (logo) {
    logo.addEventListener('error', () => {
      if (!logo.src.endsWith(fallbackLogo)) logo.src = fallbackLogo
    })
  }

  initThemeFollowSub()

  document.addEventListener('DOMContentLoaded', () => {
    syncThemeFromSub()
    loadBranding()
    loadGroups()
  })
})()

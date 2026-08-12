import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), '../..')
const baseline = JSON.parse(readFileSync(resolve(repoRoot, 'deploy/stable-release-baseline.json'), 'utf8'))
const stableCommit = baseline.commit_sha

function diff(relativePath) {
  return execFileSync('git', ['diff', '--unified=0', stableCommit, '--', relativePath], {
    cwd: repoRoot,
    encoding: 'utf8'
  })
}

function addedLines(relativePath) {
  return diff(relativePath)
    .split(/\r?\n/)
    .filter((line) => line.startsWith('+') && !line.startsWith('+++'))
    .map((line) => line.slice(1))
}

test(`upstream hotspots remain identical to Stable Release ${baseline.tag}`, () => {
  const forbidden = [
    'backend/cmd/server/wire_gen.go',
    'backend/internal/handler/wire.go',
    'backend/internal/handler/gateway_handler.go',
    'backend/internal/handler/openai_gateway_handler.go',
    'backend/internal/server/router.go',
    'backend/internal/server/routes/gateway.go',
    'backend/internal/handler/admin/user_handler.go',
    'backend/internal/server/middleware/security_headers.go',
    'frontend/src/router/index.ts',
    'deploy/docker-compose.local.yml'
  ]

  for (const relativePath of forbidden) {
    const patch = diff(relativePath)
    assert.equal(
      patch.trim(),
      '',
      `${relativePath} exceeds the zero-overlap budget:\n${addedLines(relativePath).join('\n')}`
    )
  }
})

test('legacy release safety routes stay within the reviewed three-line budget', () => {
  const relativePath = 'backend/internal/server/routes/admin.go'
  assert.deepEqual(addedLines(relativePath), [
    '\t\tsystem.GET("/rollback-versions", h.Admin.System.LegacyRollbackUnsupported)',
    '\t\tsystem.POST("/update", h.Admin.System.PrepareUpdate)',
    '\t\tsystem.POST("/rollback", h.Admin.System.LegacyRollbackUnsupported)'
  ], `${relativePath} changed outside the reviewed compatibility hooks`)
})

test('authentication and content-risk lifecycle hooks do not grow silently', () => {
  const budgets = new Map([
    ['backend/internal/handler/auth_handler.go', { added: 21, markers: 12 }],
    ['backend/internal/handler/auth_email_oauth.go', { added: 7, markers: 2 }],
    ['backend/internal/handler/auth_oauth_pending_flow.go', { added: 8, markers: 2 }],
    ['backend/internal/handler/auth_oidc_oauth.go', { added: 8, markers: 2 }],
    ['backend/internal/handler/auth_linuxdo_oauth.go', { added: 2, markers: 1 }],
    ['backend/internal/handler/auth_wechat_oauth.go', { added: 1, markers: 1 }],
    ['backend/internal/handler/auth_dingtalk_oauth.go', { added: 1, markers: 1 }],
    ['backend/internal/handler/passkey_handler.go', { added: 9, markers: 3 }],
    ['backend/internal/handler/content_moderation_helper.go', { added: 3, markers: 1 }]
  ])
  const markerPattern = /riskControl|RiskBan|RegistrationRisk|LoginRisk|preflightRegistrationRisk|preflightLoginRisk|SetRiskEventContext|GetTrustedClientIP/g

  for (const [relativePath, budget] of budgets) {
    const lines = addedLines(relativePath)
    const markerCount = (lines.join('\n').match(markerPattern) ?? []).length
    assert.ok(lines.length <= budget.added, `${relativePath} added-line budget ${budget.added} exceeded:\n${lines.join('\n')}`)
    assert.ok(markerCount <= budget.markers, `${relativePath} custom-marker budget ${budget.markers} exceeded:\n${lines.join('\n')}`)
  }
})

test('the legacy deploy helper adds only the no-secret-output patch', () => {
  const relativePath = 'deploy/docker-deploy.sh'
  assert.deepEqual(addedLines(relativePath), [
    '    echo "Generated credentials were saved to .env and were not printed."'
  ], `${relativePath} contains unbudgeted custom additions`)

  const source = readFileSync(resolve(repoRoot, relativePath), 'utf8')
  assert.doesNotMatch(
    source,
    /^\s*(?:echo|printf)\b[^\n]*(?:POSTGRES_PASSWORD|JWT_SECRET|TOTP_ENCRYPTION_KEY|RISK_CONTROL_[A-Z_]+)/m,
    `${relativePath} must never print generated secret variables`
  )
})

test('Custom Release CI fetches the Stable baseline before overlap contracts', () => {
  const workflow = readFileSync(resolve(repoRoot, '.github/workflows/custom-release.yml'), 'utf8')
  const fetchIndex = workflow.indexOf('Fetch recorded Stable baseline')
  const contractsIndex = workflow.indexOf('Deployment contracts')
  assert.ok(fetchIndex >= 0, 'deployment job is missing the Stable baseline fetch')
  assert.ok(fetchIndex < contractsIndex, 'Stable baseline must be fetched before deployment contracts')
})

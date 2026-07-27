import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { spawnSync } from 'node:child_process'
import test from 'node:test'

const stableBaseline = JSON.parse(
  readFileSync(new URL('../stable-release-baseline.json', import.meta.url), 'utf8')
)

const stableOwnedHotFiles = [
  'backend/internal/service/update_service.go',
  'backend/internal/handler/admin/system_handler.go',
  'frontend/src/components/common/VersionBadge.vue',
  'frontend/src/api/admin/system.ts',
  'frontend/src/stores/app.ts'
]

test('stable-owned release hot files match the pinned Stable commit', () => {
  const result = spawnSync(
    'git',
    ['diff', '--exit-code', stableBaseline.commit_sha, '--', ...stableOwnedHotFiles],
    { cwd: new URL('../..', import.meta.url), encoding: 'utf8', shell: process.platform === 'win32' }
  )

  assert.equal(result.status, 0, result.stdout || result.stderr)
})

test('pinned Stable identity matches the newest Stable Release merge', () => {
  const cwd = new URL('../..', import.meta.url)
  const latestMerge = spawnSync(
    'git',
    [
      'log',
      '--first-parent',
      '--merges',
      '-n',
      '1',
      '--format=%H%x00%s',
      '--grep=^merge: integrate stable Release v[0-9]',
      'HEAD'
    ],
    { cwd, encoding: 'utf8' }
  )
  assert.equal(latestMerge.status, 0, latestMerge.stdout || latestMerge.stderr)

  const [mergeCommit, mergeSubject] = latestMerge.stdout.trim().split('\0')
  const releaseTag = mergeSubject?.match(/^merge: integrate stable Release (v\d+\.\d+\.\d+)$/)?.[1]
  assert.equal(stableBaseline.tag, releaseTag)

  const releaseCommit = spawnSync(
    'git',
    ['rev-parse', `${mergeCommit}^2`],
    { cwd, encoding: 'utf8' }
  )
  assert.equal(releaseCommit.status, 0, releaseCommit.stdout || releaseCommit.stderr)
  assert.equal(stableBaseline.commit_sha, releaseCommit.stdout.trim())
})

test('official admin routes fail closed or delegate to custom prepare', () => {
  const source = readFileSync(new URL('../../backend/internal/server/routes/admin.go', import.meta.url), 'utf8')
  const registerSystemRoutes = source.match(/func registerSystemRoutes[\s\S]*?\n}/)?.[0] ?? ''

  assert.match(registerSystemRoutes, /POST\("\/update", h\.Admin\.System\.PrepareUpdate\)/)
  assert.match(registerSystemRoutes, /POST\("\/rollback", h\.Admin\.System\.LegacyRollbackUnsupported\)/)
  assert.match(registerSystemRoutes, /GET\("\/rollback-versions", h\.Admin\.System\.LegacyRollbackUnsupported\)/)
  assert.doesNotMatch(registerSystemRoutes, /(ledger|high.water|state machine|release-state)/i)
})

test('sidebar delegates release UI to the additive custom badge', () => {
  const source = readFileSync(new URL('../../frontend/src/components/layout/AppSidebar.vue', import.meta.url), 'utf8')

  assert.match(source, /import VersionBadge from '@\/features\/custom-release\/CustomReleaseBadge\.vue'/)
  assert.doesNotMatch(source, /(preparedJob|releaseLedger|customVersionHighWater|applyUpdate)/)
})

test('custom runtime image includes git for local rollback source eligibility', () => {
  const dockerfile = readFileSync(new URL('../../Dockerfile', import.meta.url), 'utf8')
  const runtimeStage = dockerfile.slice(dockerfile.lastIndexOf('FROM ${ALPINE_IMAGE}'))

  assert.match(runtimeStage, /RUN apk add --no-cache[\s\S]*?\bgit\s*\\/)
})

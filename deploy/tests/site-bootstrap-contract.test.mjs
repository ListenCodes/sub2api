import assert from 'node:assert/strict'
import { existsSync, readFileSync } from 'node:fs'
import test from 'node:test'

const root = new URL('../../', import.meta.url)
const load = (path) => {
  const url = new URL(path, root)
  assert.equal(existsSync(url), true, `${path} must exist`)
  return readFileSync(url, 'utf8')
}

const forbidden = [
  /docker compose down/,
  /docker system prune/,
  /docker volume prune/,
  /git reset --hard/,
  /\b(?:ssh|scp)\b/,
  /sub2api-custom:(?!custom-)/,
  /sub2api-extensions:(?!custom-)/,
]

test('site exporter creates a complete checksummed migration bundle', () => {
  const script = load('deploy/ops/export-custom-site.sh')

  for (const marker of [
    'EXPORT-SITE',
    'origin/custom-release',
    'release-ledger',
    'release-state.json',
    'release-backups',
    'sub2api_db.dump',
    'risk_control_db.dump',
    'pg_restore --list',
    'SHA256SUMS',
    'active_operation_id',
  ]) assert.match(script, new RegExp(marker.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')))

  for (const pattern of forbidden) assert.doesNotMatch(script, pattern)
})

test('site bootstrap supports only fail-closed fresh and migrate modes', () => {
  const script = load('deploy/ops/bootstrap-custom-site.sh')

  for (const marker of [
    'fresh',
    'migrate',
    'FRESH-EMPTY-SITE',
    'RESTORE-MIGRATION',
    'origin/custom-release',
    'verify-release-images.sh',
    'stable-release-baseline.json',
    'linux/amd64',
    'sha256:',
    'release-ledger',
    'release-state.json',
    'pg_restore --list',
    'sub2api-release.path',
    'systemctl enable --now',
    '--project-name deploy',
    '--pull never',
  ]) assert.match(script, new RegExp(marker.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')))

  const postgres = script.indexOf('start_service postgres')
  const redis = script.indexOf('start_service redis')
  const risk = script.indexOf('start_service risk-control-postgres')
  const extensions = script.indexOf('start_service extensions-self')
  const main = script.indexOf('start_service sub2api')
  assert.ok(postgres >= 0 && postgres < redis && redis < risk && risk < extensions && extensions < main)

  for (const pattern of forbidden) assert.doesNotMatch(script, pattern)
})

test('bootstrap cleanup only removes containers created by the current run', () => {
  const script = load('deploy/ops/bootstrap-custom-site.sh')

  assert.match(script, /TARGET_CONTAINERS=\([^)]*sub2api[^)]*extensions-self[^)]*\)/s)
  assert.match(script, /CREATED_CONTAINERS=\(\)/)
  assert.match(script, /CREATED_CONTAINERS\+=\("\$container"\)/)
  assert.doesNotMatch(script, /CREATED_CONTAINERS=\(sub2api/)
})

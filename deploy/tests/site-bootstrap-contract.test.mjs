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
    'protected-release-backups',
    'sub2api_db.dump',
    'risk_control_db.dump',
    'pg_restore --list',
    'SHA256SUMS',
    'active_operation_id',
  ]) assert.match(script, new RegExp(marker.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')))

  for (const pattern of forbidden) assert.doesNotMatch(script, pattern)

  assert.ok(script.indexOf('flock -n 8') < script.indexOf('ledger_validate_state'), 'export must lock before reading release state')
  assert.match(script, /ledger_validate_current_projection/)
  assert.match(script, /env_value POSTGRES_USER "\$ENV_FILE"/)
  assert.match(script, /env_value RISK_CONTROL_POSTGRES_USER "\$ENV_FILE"/)
  assert.doesNotMatch(script, /RISK_POSTGRES_USER/)
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
    'protected-release-backups',
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

  assert.match(script, /ledger_validate_current_projection/)
  assert.match(script, /--no-pull/)
  assert.match(script, /BOOTSTRAP_OWNER/)
  assert.match(script, /com\.listencodes\.sub2api\.bootstrap-owner/)
  assert.ok(script.indexOf('volume_owner=') < script.indexOf('CREATED_VOLUMES+=("$volume")'))
})

test('bootstrap cleanup only removes containers created by the current run', () => {
  const script = load('deploy/ops/bootstrap-custom-site.sh')

  assert.match(script, /TARGET_CONTAINERS=\([^)]*sub2api[^)]*extensions-self[^)]*\)/s)
  assert.match(script, /CREATED_CONTAINERS=\(\)/)
  assert.match(script, /CREATED_CONTAINERS\+=\("\$container"\)/)
  assert.match(script, /CREATED_CONTAINER_IDS\+=\("\$container_id"\)/)
  assert.match(script, /container_id="\$\(docker container inspect --format '\{\{\.Id\}\}'/)
  assert.doesNotMatch(script, /CREATED_CONTAINERS=\(sub2api/)
})

test('image verification exposes a registry-only mode for check-only bootstrap', () => {
  const script = load('deploy/ops/verify-release-images.sh')

  assert.match(script, /--no-pull/)
  assert.match(script, /imagetools inspect "\$tag" --format/)
  assert.match(script, /if \[\[ "\$NO_PULL" == true \]\]/)
})

test('the Linux bootstrap fixture is an enforced deployment workflow gate', () => {
  const workflow = load('.github/workflows/custom-release.yml')
  assert.match(workflow, /bash deploy\/tests\/site-bootstrap-test\.sh/)
})

test('the Linux bootstrap fixture does not require Windows path conversion', () => {
  const fixture = load('deploy/tests/site-bootstrap-test.sh')

  assert.doesNotMatch(fixture, /\bcygpath\b/)
  assert.match(fixture, /export SUB2API_DATA_DIR="\$TMP\/data"/)
  assert.match(
    fixture,
    /export MSYS2_ARG_CONV_EXCL="\$SUB2API_DATA_DIR\/release-backups\/;\$BUNDLE\/release-backups\/"/,
  )
  assert.doesNotMatch(
    fixture,
    /MSYS2_ARG_CONV_EXCL=['"]\*['"]/,
  )
})

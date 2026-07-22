import assert from 'node:assert/strict'
import { execFileSync, spawnSync } from 'node:child_process'
import { existsSync, readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

const deployRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const repoRoot = resolve(deployRoot, '..')
const basePath = resolve(deployRoot, 'docker-compose.yml')
const overlayPath = resolve(deployRoot, 'docker-compose.custom.yml')
const envPath = resolve(deployRoot, 'tests/fixtures/compose.env')

function read(relativePath) {
  return readFileSync(resolve(repoRoot, relativePath), 'utf8')
}

test('production base Compose remains byte-identical to Stable Release v0.1.163', () => {
  const upstream = execFileSync('git', [
    'show',
    'd0bdd7e771636a8d315f542cafd39484f39bd60c:deploy/docker-compose.yml'
  ], { cwd: repoRoot, encoding: 'utf8' })
  assert.equal(read('deploy/docker-compose.yml'), upstream)
})

test('custom production differences live only in the explicit overlay', () => {
  const base = read('deploy/docker-compose.yml')
  const overlay = read('deploy/docker-compose.custom.yml')

  assert.equal(existsSync(overlayPath), true, 'custom Compose overlay is missing')
  assert.doesNotMatch(base, /SUB2API_IMAGE|EXTENSIONS_SELF_IMAGE|risk-control-postgres|extensions-self/)
  assert.doesNotMatch(base, /\/root\/sub2api|docker\.sock|sync-trigger\.sh|\/usr\/bin\/docker/)
  assert.match(overlay, /services:\s*\n\s+sub2api:/)
  for (const marker of [
    'image: ${SUB2API_IMAGE:?SUB2API_IMAGE is required}',
    'image: ${EXTENSIONS_SELF_IMAGE:?EXTENSIONS_SELF_IMAGE is required}',
    '/root/sub2api:/repo:rw',
    '/var/run/docker.sock:/var/run/docker.sock',
    '/opt/sub2api-custom/sync-trigger.sh:/app/scripts/sync-upstream.sh:ro',
    '/usr/bin/docker:/usr/bin/docker:ro',
    'RISK_CONTROL_URL:',
    'RISK_CONTROL_INTERNAL_SECRET:',
    'RISK_CONTROL_DECISION_FAIL_MODE:',
    'extensions-self:',
    'risk-control-postgres:',
    'risk_control_postgres_data:'
  ]) {
    assert.match(overlay, new RegExp(marker.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')), `overlay is missing ${marker}`)
  }
})

test('rendered Compose keeps the production service and identity contract', (t) => {
  if (!existsSync(envPath)) t.skip('fixture is missing')
  const result = spawnSync('docker', [
    'compose',
    '--project-name',
    'deploy',
    '-f',
    basePath,
    '-f',
    overlayPath,
    '--env-file',
    envPath,
    'config',
    '--format',
    'json'
  ], { cwd: repoRoot, encoding: 'utf8' })
  if (result.error?.code === 'ENOENT') {
    t.skip('Docker CLI is unavailable')
    return
  }
  assert.equal(result.status, 0, result.stderr)
  const rendered = JSON.parse(result.stdout)
  assert.deepEqual(Object.keys(rendered.services).sort(), [
    'extensions-self',
    'postgres',
    'redis',
    'risk-control-postgres',
    'sub2api'
  ])
  assert.equal(rendered.services.sub2api.image, process.env.SUB2API_IMAGE ?? 'ghcr.io/listencodes/sub2api-custom@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa')
  assert.equal(rendered.services['extensions-self'].image, process.env.EXTENSIONS_SELF_IMAGE ?? 'ghcr.io/listencodes/sub2api-extensions@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb')
  assert.equal(rendered.services['risk-control-postgres'].container_name, 'risk-control-postgres')
  assert.ok(rendered.volumes.risk_control_postgres_data)
  assert.ok(rendered.networks['sub2api-network'])
  assert.ok(rendered.services.sub2api.volumes.some((volume) => volume.source === '/root/sub2api' && volume.target === '/repo'))
  assert.equal(rendered.services.sub2api.environment.RISK_CONTROL_URL, 'http://extensions-self:8090')
  assert.equal(rendered.services['extensions-self'].depends_on['risk-control-postgres'].condition, 'service_healthy')
})

test('publisher always uses and backs up the matching Compose pair', () => {
  const publisher = read('deploy/ops/publish-custom.sh')
  for (const marker of [
    'docker-compose.yml',
    'docker-compose.custom.yml',
    'main-docker-compose.yml',
    'custom-docker-compose.yml',
    '-f "$COMPOSE_BASE"',
    '-f "$COMPOSE_CUSTOM"'
  ]) {
    assert.match(publisher, new RegExp(marker.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')), `publisher is missing ${marker}`)
  }
})

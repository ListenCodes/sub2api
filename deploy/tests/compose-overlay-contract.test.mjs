import assert from 'node:assert/strict'
import { execFileSync, spawnSync } from 'node:child_process'
import { appendFileSync, existsSync, readFileSync, rmSync, statSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

const deployRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const repoRoot = resolve(deployRoot, '..')
const basePath = resolve(deployRoot, 'docker-compose.yml')
const overlayPath = resolve(deployRoot, 'docker-compose.custom.yml')
const envPath = resolve(deployRoot, 'tests/fixtures/compose.env')
const localOverlayPath = resolve(deployRoot, 'docker-compose.custom.local.yml')
const stableBaseline = JSON.parse(
  readFileSync(resolve(deployRoot, 'stable-release-baseline.json'), 'utf8')
)

function read(relativePath) {
  return readFileSync(resolve(repoRoot, relativePath), 'utf8')
}

function reportComposeFailure(error, result) {
  if (!process.env.GITHUB_ACTIONS) return
  const details = [error?.message, result?.stderr, result?.stdout]
    .filter(Boolean)
    .join('\n')
    .replaceAll('%', '%25')
    .replaceAll('\r', '%0D')
    .replaceAll('\n', '%0A')
  console.error(`::error file=deploy/docker-compose.custom.yml::${details}`)
  if (process.env.GITHUB_STEP_SUMMARY) {
    appendFileSync(process.env.GITHUB_STEP_SUMMARY, `### Compose contract failure\n\n\`\`\`text\n${[error?.stack, result?.stderr, result?.stdout].filter(Boolean).join('\n').slice(0, 12000)}\n\`\`\`\n`)
  }
}

test(`production base Compose remains byte-identical to Stable Release ${stableBaseline.tag}`, () => {
  const upstream = execFileSync('git', [
    'show',
    `${stableBaseline.commit_sha}:deploy/docker-compose.yml`
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

test('custom local differences live only in the explicit local overlay', () => {
  const base = read('deploy/docker-compose.local.yml')
  const overlay = read('deploy/docker-compose.custom.local.yml')

  assert.equal(existsSync(localOverlayPath), true, 'custom local Compose overlay is missing')
  assert.doesNotMatch(base, /RISK_CONTROL_|risk-control-postgres|extensions-self/)
  for (const marker of [
    'services:',
    'sub2api:',
    'RISK_CONTROL_URL:',
    'RISK_CONTROL_INTERNAL_SECRET:',
    'RISK_CONTROL_DECISION_FAIL_MODE:',
    'risk-control-postgres:',
    'extensions-self:',
    'image: deploy-extensions-self',
    'context: ../extensions-self',
    './risk_control_postgres_data:/var/lib/postgresql/data:Z'
  ]) {
    assert.match(overlay, new RegExp(marker.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')), `local overlay is missing ${marker}`)
  }
})

test('custom local bootstrap writes private secrets without printing them', (t) => {
  const scriptPath = resolve(deployRoot, 'ops/bootstrap-custom-local.sh')
  assert.equal(existsSync(scriptPath), true, 'custom local bootstrap is missing')
  const suffix = `${process.pid}-${Date.now()}`
  const relativeEnvPath = `deploy/tests/.tmp-custom-local-${suffix}.env`
  const absoluteEnvPath = resolve(repoRoot, relativeEnvPath)
  const secrets = [
    'fixture-postgres-secret',
    'fixture-jwt-secret',
    'fixture-totp-secret',
    'fixture-risk-internal-secret',
    'fixture-risk-postgres-secret'
  ]

  t.after(() => rmSync(absoluteEnvPath, { force: true }))
  const result = spawnSync(process.env.BASH_BIN || 'bash', ['deploy/ops/bootstrap-custom-local.sh'], {
    cwd: repoRoot,
    encoding: 'utf8',
    env: {
      ...process.env,
      CUSTOM_LOCAL_ENV_FILE: relativeEnvPath,
      CUSTOM_LOCAL_DOCKER_BIN: 'true',
      CUSTOM_LOCAL_POSTGRES_PASSWORD: secrets[0],
      CUSTOM_LOCAL_JWT_SECRET: secrets[1],
      CUSTOM_LOCAL_TOTP_ENCRYPTION_KEY: secrets[2],
      CUSTOM_LOCAL_RISK_CONTROL_INTERNAL_SECRET: secrets[3],
      CUSTOM_LOCAL_RISK_CONTROL_POSTGRES_PASSWORD: secrets[4]
    }
  })
  if (result.error?.code === 'ENOENT') {
    t.skip('bash is unavailable')
    return
  }

  assert.equal(result.status, 0, result.stderr)
  assert.deepEqual(result.stdout.trim().split(/\r?\n/), [
    `Custom local environment created at ${relativeEnvPath}`,
    'Generated 5 secret values; values were not printed'
  ])
  const combinedOutput = `${result.stdout}\n${result.stderr}`
  for (const secret of secrets) {
    assert.doesNotMatch(combinedOutput, new RegExp(secret))
    assert.match(readFileSync(absoluteEnvPath, 'utf8'), new RegExp(secret))
  }
  if (process.platform !== 'win32') {
    assert.equal(statSync(absoluteEnvPath).mode & 0o077, 0)
  }

  const script = read('deploy/ops/bootstrap-custom-local.sh')
  assert.match(script, /chmod 600/)
  assert.match(script, /docker-compose\.local\.yml/)
  assert.match(script, /docker-compose\.custom\.local\.yml/)
  assert.match(script, /--env-file/)
  assert.match(script, /up -d/)
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
  try {
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
  } catch (error) {
    reportComposeFailure(error, result)
    throw error
  }
})

test('prepare and apply always use the matching Compose pair', () => {
  const publisher = `${read('deploy/ops/prepare-release.sh')}\n${read('deploy/ops/apply-release.sh')}`
  for (const marker of [
    'docker-compose.yml',
    'docker-compose.custom.yml',
    'docker-compose.yml',
    'docker-compose.custom.yml',
    '-f "$COMPOSE_BASE"',
    '-f "$COMPOSE_CUSTOM"'
  ]) {
    assert.match(publisher, new RegExp(marker.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')), `publisher is missing ${marker}`)
  }
})

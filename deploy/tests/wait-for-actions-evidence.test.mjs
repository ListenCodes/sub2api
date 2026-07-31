import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { spawnSync } from 'node:child_process'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

const deployRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const repositoryRoot = resolve(deployRoot, '..')
const filter = resolve(deployRoot, 'ops', 'actions-check-result.jq')
const waiter = resolve(deployRoot, 'ops', 'wait-for-actions.sh')
const expectedChecks = JSON.stringify([
  'backend',
  'golangci',
  'frontend',
  'extensions',
  'deployment',
  'metadata',
  'images'
])

function evaluateFixture(name) {
  const result = spawnSync(
    process.env.JQ_BIN || 'jq',
    ['-c', '--argjson', 'expected', expectedChecks, '-f', filter, resolve(deployRoot, 'tests', 'fixtures', 'actions', `${name}.json`)],
    { cwd: repositoryRoot, encoding: 'utf8' }
  )
  return {
    ...result,
    evidence: result.status === 0 ? JSON.parse(result.stdout) : undefined
  }
}

function runWaiter(name) {
  const result = spawnSync(
    process.env.BASH_BIN || 'bash',
    [waiter, 'a'.repeat(40)],
    {
      cwd: repositoryRoot,
      encoding: 'utf8',
      env: {
        ...process.env,
        SUB2API_CHECKS_JSON_FILE: resolve(deployRoot, 'tests', 'fixtures', 'actions', `${name}.json`)
      }
    }
  )
  if (result.error?.code === 'ENOENT') return undefined
  const lines = result.stdout.trim().split(/\r?\n/).filter(Boolean)
  return {
    ...result,
    lines,
    evidence: lines.length === 1 ? JSON.parse(lines[0]) : undefined
  }
}

test('successful required checks return the images workflow URL', () => {
  const result = evaluateFixture('success')
  assert.equal(result.status, 0, result.stderr)
  assert.deepEqual(result.evidence, {
    ok: true,
    message: 'all required GitHub Actions checks succeeded',
    error_code: '',
    failed_check: '',
    check_url: '',
    conclusion: 'success',
    workflow_url: 'https://github.com/ListenCodes/sub2api/actions/runs/7/job/70',
    production_changed: false
  })
})

for (const [fixture, check, conclusion] of [
  ['failure', 'deployment', 'failure'],
  ['cancelled', 'frontend', 'cancelled'],
  ['skipped', 'extensions', 'skipped'],
  ['images-failed', 'images', 'failure']
]) {
  test(`${fixture} returns the concrete failed check evidence`, () => {
    const result = evaluateFixture(fixture)
    assert.equal(result.status, 0, result.stderr)
    assert.equal(result.evidence.ok, false)
    assert.equal(result.evidence.error_code, 'ACTIONS_REQUIRED_CHECK_FAILED')
    assert.equal(result.evidence.failed_check, check)
    assert.equal(result.evidence.conclusion, conclusion)
    assert.match(result.evidence.check_url, /^https:\/\/github\.com\/ListenCodes\/sub2api\/actions\//)
    assert.equal(result.evidence.workflow_url, result.evidence.check_url)
    assert.equal(result.evidence.production_changed, false)
  })
}

test('a missing required check stays pending with its exact identity', () => {
  const result = evaluateFixture('missing')
  assert.equal(result.status, 0, result.stderr)
  assert.equal(result.evidence.ok, null)
  assert.equal(result.evidence.error_code, 'ACTIONS_REQUIRED_CHECK_MISSING')
  assert.equal(result.evidence.failed_check, 'deployment')
  assert.equal(result.evidence.conclusion, 'missing')
  assert.equal(result.evidence.check_url, '')
  assert.equal(result.evidence.production_changed, false)
})

test('malformed GitHub check evidence fails closed', () => {
  const result = evaluateFixture('malformed')
  assert.notEqual(result.status, 0)
  assert.equal(result.evidence, undefined)
})

for (const [fixture, expectedStatus, expectedCode] of [
  ['success', 0, ''],
  ['failure', 1, 'ACTIONS_REQUIRED_CHECK_FAILED'],
  ['missing', 1, 'ACTIONS_REQUIRED_CHECK_MISSING'],
  ['malformed', 1, 'ACTIONS_EVIDENCE_INVALID']
]) {
  test(`waiter emits one structured ${fixture} result`, (t) => {
    const result = runWaiter(fixture)
    if (!result) {
      t.skip('bash is unavailable')
      return
    }
    assert.equal(result.status, expectedStatus, result.stderr)
    assert.equal(result.lines.length, 1, `unexpected stdout: ${result.stdout}`)
    assert.equal(result.evidence.error_code, expectedCode)
    assert.equal(result.evidence.production_changed, false)
    assert.equal(result.evidence.ok, expectedStatus === 0)
  })
}

test('prepare-release persists structured waiter evidence without changing production', () => {
  const source = readFileSync(resolve(deployRoot, 'ops', 'prepare-release.sh'), 'utf8')
  for (const field of ['failed_check', 'check_url', 'conclusion', 'workflow_url', 'error_code', 'production_changed']) {
    assert.match(source, new RegExp(`\\b${field}\\b`), `prepare-release.sh does not persist ${field}`)
  }
  assert.match(source, /\.ok == false/)
  assert.match(source, /\.ok == true/)
})

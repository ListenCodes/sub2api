import assert from 'node:assert/strict'
import { mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, resolve } from 'node:path'
import { spawnSync } from 'node:child_process'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

const deployRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const repositoryRoot = resolve(deployRoot, '..')
const promoteScript = resolve(deployRoot, 'ops', 'promote-release.sh')

function run(command, args, cwd, options = {}) {
  const result = spawnSync(command, args, { cwd, encoding: 'utf8', ...options })
  assert.equal(result.status, 0, result.stderr || result.stdout)
  return result.stdout.trim()
}

function git(cwd, ...args) {
  return run('git', args, cwd)
}

function promotionFixture(subject, baselineCommit = 'stable') {
  const root = mkdtempSync(resolve(tmpdir(), 'sub2api-stable-merge-'))
  const remote = resolve(root, 'origin.git')
  const seed = resolve(root, 'seed')
  const checkout = resolve(root, 'checkout')
  mkdirSync(seed)
  git(root, 'init', '--bare', remote)
  git(seed, 'init')
  git(seed, 'config', 'user.email', 'release-fixture@example.com')
  git(seed, 'config', 'user.name', 'Release Fixture')
  writeFileSync(resolve(seed, 'root.txt'), 'root\n')
  git(seed, 'add', 'root.txt')
  git(seed, 'commit', '-m', 'root')
  const rootCommit = git(seed, 'rev-parse', 'HEAD')

  git(seed, 'switch', '-c', 'stable')
  writeFileSync(resolve(seed, 'stable.txt'), 'stable\n')
  git(seed, 'add', 'stable.txt')
  git(seed, 'commit', '-m', 'stable release')
  const stableCommit = git(seed, 'rev-parse', 'HEAD')

  git(seed, 'switch', '-c', 'custom-release', rootCommit)
  writeFileSync(resolve(seed, 'custom.txt'), 'custom\n')
  git(seed, 'add', 'custom.txt')
  git(seed, 'commit', '-m', 'custom base')
  const baseCommit = git(seed, 'rev-parse', 'HEAD')
  git(seed, 'merge', '--no-ff', '-m', subject, stableCommit)

  mkdirSync(resolve(seed, 'deploy'))
  writeFileSync(resolve(seed, 'deploy', 'stable-release-baseline.json'), `${JSON.stringify({
    repository: 'Wei-Shaw/sub2api',
    tag: 'v0.1.169',
    tag_object_sha: 'b'.repeat(40),
    commit_sha: baselineCommit === 'stable' ? stableCommit : rootCommit,
    published_at: '2026-07-31T03:00:00Z'
  }, null, 2)}\n`)
  git(seed, 'add', 'deploy/stable-release-baseline.json')
  git(seed, 'commit', '-m', 'chore: record stable Release v0.1.169')
  const targetCommit = git(seed, 'rev-parse', 'HEAD')
  git(seed, 'remote', 'add', 'origin', remote)
  git(seed, 'push', 'origin', `${baseCommit}:refs/heads/custom-release`)
  git(seed, 'push', 'origin', `${targetCommit}:refs/heads/integration/release-v0.1.169-fixture`)

  git(root, 'clone', remote, checkout)
  git(checkout, 'switch', 'custom-release')
  return { root, checkout, baseCommit, targetCommit }
}

function promote(fixture) {
  return spawnSync(
    process.env.BASH_BIN || 'bash',
    [promoteScript, fixture.baseCommit, fixture.targetCommit, 'integration/release-v0.1.169-fixture'],
    {
      cwd: repositoryRoot,
      encoding: 'utf8',
      env: {
        ...process.env,
        SUB2API_REPO: fixture.checkout,
        SUB2API_PROMOTE_LOG: resolve(fixture.root, 'promote.log')
      }
    }
  )
}

test('sync-upstream creates the canonical Stable merge subject explicitly', () => {
  const source = readFileSync(resolve(deployRoot, 'ops', 'sync-upstream.sh'), 'utf8')
  assert.match(source, /MERGE_SUBJECT="merge: integrate stable Release \$RELEASE_TAG"/)
  assert.match(source, /merge --no-ff -m "\$MERGE_SUBJECT" "\$RELEASE_COMMIT"/)
})

test('promotion accepts a canonical merge whose second parent matches the baseline', (t) => {
  const fixture = promotionFixture('merge: integrate stable Release v0.1.169')
  t.after(() => rmSync(fixture.root, { recursive: true, force: true }))
  const result = promote(fixture)
  if (result.error?.code === 'ENOENT') {
    t.skip('bash is unavailable')
    return
  }
  assert.equal(result.status, 0, result.stderr || result.stdout)
})

test('promotion rejects a generic merge subject even when ancestry is valid', (t) => {
  const fixture = promotionFixture("Merge commit '26d894ef' into integration/release-v0.1.169")
  t.after(() => rmSync(fixture.root, { recursive: true, force: true }))
  const result = promote(fixture)
  if (result.error?.code === 'ENOENT') {
    t.skip('bash is unavailable')
    return
  }
  assert.notEqual(result.status, 0)
  assert.match(result.stderr, /canonical stable Release merge/i)
})

test('promotion rejects a baseline commit that is not the merge second parent', (t) => {
  const fixture = promotionFixture('merge: integrate stable Release v0.1.169', 'root')
  t.after(() => rmSync(fixture.root, { recursive: true, force: true }))
  const result = promote(fixture)
  if (result.error?.code === 'ENOENT') {
    t.skip('bash is unavailable')
    return
  }
  assert.notEqual(result.status, 0)
  assert.match(result.stderr, /second parent does not match the baseline commit/i)
})

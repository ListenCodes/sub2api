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
const releaseCommonScript = resolve(deployRoot, 'ops', 'release-common.sh')

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

function revertedIntegrationFixture() {
  const root = mkdtempSync(resolve(tmpdir(), 'sub2api-reverted-stable-'))
  const repository = resolve(root, 'repository')
  mkdirSync(repository)
  git(repository, 'init')
  git(repository, 'config', 'user.email', 'release-fixture@example.com')
  git(repository, 'config', 'user.name', 'Release Fixture')
  writeFileSync(resolve(repository, 'root.txt'), 'root\n')
  git(repository, 'add', 'root.txt')
  git(repository, 'commit', '-m', 'root')
  const rootCommit = git(repository, 'rev-parse', 'HEAD')

  git(repository, 'switch', '-c', 'stable')
  writeFileSync(resolve(repository, 'stable.txt'), 'stable\n')
  git(repository, 'add', 'stable.txt')
  git(repository, 'commit', '-m', 'stable release')
  const stableCommit = git(repository, 'rev-parse', 'HEAD')

  git(repository, 'switch', '-c', 'custom-release', rootCommit)
  writeFileSync(resolve(repository, 'custom.txt'), 'custom\n')
  git(repository, 'add', 'custom.txt')
  git(repository, 'commit', '-m', 'custom base')
  git(repository, 'merge', '--no-ff', '-m', 'merge: integrate stable Release v0.1.180', stableCommit)
  const originalMerge = git(repository, 'rev-parse', 'HEAD')
  git(repository, 'revert', '-m', '1', '--no-edit', originalMerge)
  const revertCommit = git(repository, 'rev-parse', 'HEAD')
  writeFileSync(resolve(repository, 'later.txt'), 'later custom change\n')
  git(repository, 'add', 'later.txt')
  git(repository, 'commit', '-m', 'later custom change')
  const baseCommit = git(repository, 'rev-parse', 'HEAD')
  return { root, repository, stableCommit, originalMerge, revertCommit, baseCommit }
}

function reactivateStable(fixture) {
  return spawnSync(
    process.env.BASH_BIN || 'bash',
    ['-c', 'source "$1"\nrelease_merge_stable_candidate "$2" "$3" v0.1.180', 'bash', releaseCommonScript, fixture.repository, fixture.stableCommit],
    { cwd: repositoryRoot, encoding: 'utf8', env: { ...process.env } }
  )
}

test('sync-upstream creates the canonical Stable merge subject explicitly', () => {
  const source = readFileSync(resolve(deployRoot, 'ops', 'sync-upstream.sh'), 'utf8')
  const preflight = readFileSync(resolve(deployRoot, 'ci', 'prepare-upstream-candidate.sh'), 'utf8')
  const common = readFileSync(resolve(deployRoot, 'ops', 'release-common.sh'), 'utf8')
  assert.match(common, /git -C "\$repo" merge --no-ff -m "\$\(release_stable_merge_subject "\$release_tag"\)" "\$release_commit"/)
  assert.match(source, /release_merge_stable_candidate "\$WORKTREE" "\$RELEASE_COMMIT" "\$RELEASE_TAG"/)
  assert.match(preflight, /release_merge_stable_candidate "\$REPO" "\$RELEASE_COMMIT" "\$RELEASE_TAG"/)
  assert.match(source, /release_validate_canonical_stable_merge/)
  assert.match(preflight, /release_validate_canonical_stable_merge/)
})

test('sync-upstream keeps deployed and locked-target Stable baselines separate', () => {
  const source = readFileSync(resolve(deployRoot, 'ops', 'sync-upstream.sh'), 'utf8')
  assert.match(source, /deployed_baseline_json="\$\(git show "\$DEPLOYED_COMMIT:\$BASELINE_RELATIVE"/)
  assert.match(source, /target_baseline_json="\$\(git show "\$BASE_COMMIT:\$BASELINE_RELATIVE"/)
  assert.match(source, /deployed_baseline_matches/)
  assert.match(source, /merge-base --is-ancestor "\$deployed_baseline_commit" "\$DEPLOYED_COMMIT"/)
  assert.match(source, /merge-base --is-ancestor "\$target_baseline_commit" "\$BASE_COMMIT"/)
  assert.doesNotMatch(
    source,
    /jq -er '\.tag' "\$BASELINE_FILE"/,
    'a stale host checkout must not supply baseline metadata for the locked remote base'
  )
})

test('a fully reverted Stable integration is rebuilt as a fresh canonical merge', (t) => {
  const fixture = revertedIntegrationFixture()
  t.after(() => rmSync(fixture.root, { recursive: true, force: true }))
  assert.equal(git(fixture.repository, 'diff', '--exit-code', `${fixture.originalMerge}^1`, fixture.revertCommit), '')

  const result = reactivateStable(fixture)
  if (result.error?.code === 'ENOENT') {
    t.skip('bash is unavailable')
    return
  }
  assert.equal(result.status, 0, result.stderr || result.stdout)
  const targetCommit = git(fixture.repository, 'rev-parse', 'HEAD')
  const [identity, firstParent, secondParent, extraParent] = git(fixture.repository, 'rev-list', '--parents', '-n', '1', targetCommit).split(' ')
  assert.equal(identity, targetCommit)
  assert.equal(firstParent, fixture.baseCommit)
  assert.equal(secondParent, fixture.stableCommit)
  assert.equal(extraParent, undefined)
  assert.equal(git(fixture.repository, 'show', '-s', '--format=%s', targetCommit), 'merge: integrate stable Release v0.1.180')
  assert.equal(readFileSync(resolve(fixture.repository, 'stable.txt'), 'utf8').trim(), 'stable')
  assert.equal(readFileSync(resolve(fixture.repository, 'later.txt'), 'utf8').trim(), 'later custom change')
  assert.equal(git(fixture.repository, 'status', '--porcelain'), '')
})

test('prepare-release validates installed scripts against the ledger production commit', () => {
  const source = readFileSync(resolve(deployRoot, 'ops', 'prepare-release.sh'), 'utf8')
  const productionCommit = source.indexOf('PRODUCTION_COMMIT="$(jq')
  const gate = source.indexOf('release_validate_installed_ops_at_commit "$REPO" "$PRODUCTION_COMMIT"')
  const sync = source.indexOf('"$SYNC_SCRIPT" --job-id')
  assert.ok(productionCommit >= 0 && productionCommit < gate && gate < sync)
  assert.match(source, /HOST_OPS_DRIFT/)
  assert.match(source, /SUB2API_PRODUCTION_COMMIT="\$PRODUCTION_COMMIT"/)
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

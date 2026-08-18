import assert from 'node:assert/strict'
import { chmodSync, existsSync, mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, resolve } from 'node:path'
import { spawnSync } from 'node:child_process'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

const deployRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const repoRoot = resolve(deployRoot, '..')
const workflowPath = resolve(repoRoot, '.github', 'workflows', 'upstream-stable-preflight.yml')
const preflightScript = resolve(deployRoot, 'ci', 'prepare-upstream-candidate.sh')

function read(path) {
  return readFileSync(path, 'utf8')
}

function run(command, args, cwd, options = {}) {
  return spawnSync(command, args, { cwd, encoding: 'utf8', ...options })
}

function git(cwd, ...args) {
  const result = run('git', args, cwd)
  assert.equal(result.status, 0, result.stderr || result.stdout)
  return result.stdout.trim()
}

function bashPath(path) {
  if (process.platform !== 'win32') return path
  return path.replace(/^([A-Za-z]):[\\/]/, (_, drive) => `/${drive.toLowerCase()}/`).replaceAll('\\', '/')
}

test('upstream Stable preflight is read-only and runs every release validation surface', () => {
  assert.equal(existsSync(workflowPath), true)
  assert.equal(existsSync(preflightScript), true)
  const workflow = read(workflowPath)
  const script = read(preflightScript)

  assert.match(workflow, /workflow_dispatch:/)
  assert.match(workflow, /schedule:[\s\S]*cron:/)
  assert.match(workflow, /permissions:\s*\n\s+contents:\s*read/)
  assert.doesNotMatch(workflow, /packages:\s*write|contents:\s*write/)
  assert.match(workflow, /ref:\s*custom-release/)
  assert.match(workflow, /fetch-depth:\s*0/)
  assert.match(workflow, /persist-credentials:\s*false/)
  assert.match(workflow, /refs\/heads\/main:refs\/remotes\/origin\/main/)
  assert.match(workflow, /cmp "\$RUNNER_TEMP\/upstream-stable-preflight\.yml"/)
  assert.match(workflow, /bash deploy\/ci\/prepare-upstream-candidate\.sh/)

  for (const marker of [
    'make test-unit',
    'make test-integration',
    'golangci/golangci-lint-action',
    'pnpm run typecheck',
    'pnpm run test:run',
    'pnpm run build',
    'extensions-self/account-monitor',
    'extensions-self/risk-control',
    'node --test deploy/tests/*.test.mjs',
    'bash deploy/ops/tests/test-release-pipeline.sh',
  ]) {
    assert.match(workflow, new RegExp(marker.replaceAll('*', '\\*').replaceAll('/', '\\/')))
  }

  const prohibited = /git\s+push|docker\/build-push-action|docker\s+compose\s+up|ghcr\.io|\bssh\b|\bscp\b/
  assert.doesNotMatch(workflow, prohibited)
  assert.doesNotMatch(script, prohibited)

  const sync = read(resolve(deployRoot, 'ops', 'sync-upstream.sh'))
  const common = read(resolve(deployRoot, 'ops', 'release-common.sh'))
  assert.match(common, /release_stable_merge_subject/)
  assert.match(common, /release_merge_stable_candidate/)
  assert.match(common, /release_validate_canonical_stable_merge/)
  assert.match(script, /release_merge_stable_candidate/)
  assert.match(script, /release_stable_baseline_valid/)
  assert.match(script, /merge-base --is-ancestor "\$baseline_commit" "\$BASE_COMMIT"/)
  assert.match(sync, /release_merge_stable_candidate/)
  assert.ok(
    script.indexOf('export SUB2API_REPO="$REPO"') < script.indexOf('source "$COMMON_HELPER"'),
    'release-common must not replace the preflight checkout root'
  )
})

test('shared Stable baseline validation rejects incomplete identity metadata', (t) => {
  const bundledGitBash = 'C:/Program Files/Git/bin/bash.exe'
  const bash = process.env.BASH_BIN
    || (process.platform === 'win32' && existsSync(bundledGitBash) ? bundledGitBash : 'bash')
  const jqCheck = run(bash, ['-lc', 'command -v jq'], repoRoot)
  if (jqCheck.error?.code === 'ENOENT' || jqCheck.status !== 0) {
    t.skip('bash or jq is unavailable')
    return
  }
  const result = run(
    bash,
    ['-lc', 'source "$COMMON_HELPER"; ! release_stable_baseline_valid \'{"repository":"Wei-Shaw/sub2api","tag":"v0.2.0","commit_sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","published_at":"2026-02-01T00:00:00Z"}\''],
    repoRoot,
    { env: { ...process.env, COMMON_HELPER: bashPath(resolve(deployRoot, 'ops', 'release-common.sh')) } }
  )
  assert.equal(result.status, 0, result.stderr || result.stdout)
})

test('candidate preparation creates the canonical merge and baseline commit without pushing', (t) => {
  const root = mkdtempSync(resolve(tmpdir(), 'sub2api-preflight-'))
  t.after(() => rmSync(root, { recursive: true, force: true }))
  const repository = resolve(root, 'repository')
  const upstream = resolve(root, 'upstream')
  mkdirSync(repository)
  mkdirSync(upstream)

  git(upstream, 'init')
  git(upstream, 'config', 'user.email', 'fixture@example.com')
  git(upstream, 'config', 'user.name', 'Fixture')
  writeFileSync(resolve(upstream, 'shared.txt'), 'base\n')
  git(upstream, 'add', 'shared.txt')
  git(upstream, 'commit', '-m', 'base')
  const base = git(upstream, 'rev-parse', 'HEAD')
  writeFileSync(resolve(upstream, 'stable.txt'), 'stable\n')
  git(upstream, 'add', 'stable.txt')
  git(upstream, 'commit', '-m', 'stable release')
  const stableCommit = git(upstream, 'rev-parse', 'HEAD')
  git(upstream, 'tag', '-a', 'v0.2.0', '-m', 'v0.2.0')
  const tagObject = git(upstream, 'rev-parse', 'v0.2.0^{tag}')

  git(root, 'clone', upstream, repository)
  git(repository, 'switch', '-c', 'custom-release', base)
  git(repository, 'config', 'user.email', 'fixture@example.com')
  git(repository, 'config', 'user.name', 'Fixture')
  mkdirSync(resolve(repository, 'deploy', 'ci'), { recursive: true })
  mkdirSync(resolve(repository, 'deploy', 'ops'), { recursive: true })
  const fixturePreflightScript = resolve(repository, 'deploy', 'ci', 'prepare-upstream-candidate.sh')
  writeFileSync(fixturePreflightScript, read(preflightScript))
  writeFileSync(
    resolve(repository, 'deploy', 'ops', 'release-common.sh'),
    read(resolve(deployRoot, 'ops', 'release-common.sh'))
  )
  chmodSync(fixturePreflightScript, 0o755)
  writeFileSync(
    resolve(repository, 'deploy', 'stable-release-baseline.json'),
    `${JSON.stringify({
      repository: 'Wei-Shaw/sub2api',
      tag: 'v0.1.0',
      tag_object_sha: base,
      commit_sha: base,
      published_at: '2026-01-01T00:00:00Z',
    }, null, 2)}\n`
  )
  writeFileSync(resolve(repository, 'custom.txt'), 'custom\n')
  git(repository, 'add', 'deploy', 'custom.txt')
  git(repository, 'commit', '-m', 'custom base')
  const customBase = git(repository, 'rev-parse', 'HEAD')

  const resolver = resolve(root, 'resolver.sh')
  writeFileSync(
    resolver,
    `#!/usr/bin/env bash\nprintf '%s\\n' \\
  'release_tag=v0.2.0' \\
  'release_published_at=2026-02-01T00:00:00Z' \\
  'release_tag_object_sha=${tagObject}' \\
  'release_tag_object_type=tag' \\
  'release_commit=${stableCommit}'\n`
  )
  chmodSync(resolver, 0o755)
  const output = resolve(root, 'github-output.txt')
  const bundledGitBash = 'C:/Program Files/Git/bin/bash.exe'
  const bash = process.env.BASH_BIN
    || (process.platform === 'win32' && existsSync(bundledGitBash) ? bundledGitBash : 'bash')
  const jqCheck = run(bash, ['-lc', 'command -v jq'], repoRoot)
  if (jqCheck.error?.code === 'ENOENT' || jqCheck.status !== 0) {
    t.skip('bash or jq is unavailable')
    return
  }
  const forgedRepository = resolve(root, 'forged-repository')
  git(root, 'clone', repository, forgedRepository)
  git(forgedRepository, 'config', 'user.email', 'fixture@example.com')
  git(forgedRepository, 'config', 'user.name', 'Fixture')
  writeFileSync(
    resolve(forgedRepository, 'deploy', 'stable-release-baseline.json'),
    `${JSON.stringify({
      repository: 'Wei-Shaw/sub2api',
      tag: 'v0.2.0',
      tag_object_sha: tagObject,
      commit_sha: stableCommit,
      published_at: '2026-02-01T00:00:00Z',
    }, null, 2)}\n`
  )
  git(forgedRepository, 'add', 'deploy/stable-release-baseline.json')
  git(forgedRepository, 'commit', '-m', 'forge latest baseline metadata')
  const forgedResult = run(
    bash,
    [bashPath(resolve(forgedRepository, 'deploy', 'ci', 'prepare-upstream-candidate.sh'))],
    repoRoot,
    {
      env: {
        ...process.env,
        SUB2API_RELEASE_RESOLVER: bashPath(resolver),
        SUB2API_UPSTREAM_URL: bashPath(upstream),
      },
    }
  )
  assert.notEqual(forgedResult.status, 0, forgedResult.stdout)
  assert.match(forgedResult.stderr, /approved base does not contain its recorded Stable baseline commit/)

  const result = run(bash, [bashPath(fixturePreflightScript)], repoRoot, {
    env: {
      ...process.env,
      GITHUB_OUTPUT: bashPath(output),
      SUB2API_RELEASE_RESOLVER: bashPath(resolver),
      SUB2API_UPSTREAM_URL: bashPath(upstream),
    },
  })
  if (result.error?.code === 'ENOENT') {
    t.skip('bash is unavailable')
    return
  }
  assert.equal(result.status, 0, result.stderr || result.stdout)

  const target = git(repository, 'rev-parse', 'HEAD')
  const merge = git(repository, 'rev-parse', 'HEAD^')
  assert.equal(git(repository, 'show', '-s', '--format=%s', merge), 'merge: integrate stable Release v0.2.0')
  assert.equal(git(repository, 'rev-parse', `${merge}^1`), customBase)
  assert.equal(git(repository, 'rev-parse', `${merge}^2`), stableCommit)
  assert.equal(git(repository, 'show', '-s', '--format=%s', target), 'chore: record stable Release v0.2.0')
  assert.deepEqual(
    JSON.parse(read(resolve(repository, 'deploy', 'stable-release-baseline.json'))),
    {
      repository: 'Wei-Shaw/sub2api',
      tag: 'v0.2.0',
      tag_object_sha: tagObject,
      commit_sha: stableCommit,
      published_at: '2026-02-01T00:00:00Z',
    }
  )
  assert.match(read(output), /candidate_prepared=true/)
  assert.match(read(output), new RegExp(`target_commit=${target}`))
})

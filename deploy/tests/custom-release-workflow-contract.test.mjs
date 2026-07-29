import assert from 'node:assert/strict'
import { existsSync, readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

const deployRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const repoRoot = resolve(deployRoot, '..')

function read(relativePath) {
  return readFileSync(resolve(repoRoot, relativePath), 'utf8')
}

function escapeRegExp(value) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

test('custom-release workflow gates paired images on every required validation job', () => {
  const workflowPath = resolve(repoRoot, '.github/workflows/custom-release.yml')
  assert.equal(existsSync(workflowPath), true, 'custom-release workflow is missing')

  const workflow = read('.github/workflows/custom-release.yml')
  assert.match(workflow, /push:[\s\S]*branches:[\s\S]*custom-release[\s\S]*integration\/release-/)
  assert.match(workflow, /permissions:[\s\S]*contents:\s*read[\s\S]*packages:\s*write/)
  assert.doesNotMatch(workflow, /contents:\s*write/)
  assert.match(
    workflow,
    /\n      - name: golangci-lint\n        uses: golangci\/golangci-lint-action@v9\n        with:\n          version: v2\.9\.0\n          install-mode: goinstall\n/,
    'golangci-lint must be built with the configured Go toolchain'
  )
  assert.match(
    workflow,
    /deployment:[\s\S]*?uses: actions\/checkout@v6\n        with:\n          fetch-depth: 0/,
    'deployment contracts must fetch the Stable baseline commit'
  )

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
    'bash -n',
    'docker/build-push-action'
  ]) {
    assert.match(workflow, new RegExp(escapeRegExp(marker)), `workflow is missing ${marker}`)
  }

  assert.match(workflow, /needs:\s*\[[^\]]*backend[^\]]*golangci[^\]]*frontend[^\]]*extensions[^\]]*deployment[^\]]*\]/)
  assert.match(
    workflow,
    /images:\s*\n\s+needs:[^\n]+\n\s+if:\s*github\.event_name\s*==\s*'push'/,
    'manual workflow runs must not publish release images'
  )
  assert.match(
    workflow,
    /ghcr\.io\/listencodes\/sub2api-custom:custom-\$\{\{\s*github\.sha\s*\}\}/
  )
  assert.match(
    workflow,
    /ghcr\.io\/listencodes\/sub2api-extensions:custom-\$\{\{\s*github\.sha\s*\}\}/
  )
  assert.match(workflow, /username:\s*\$\{\{\s*github\.actor\s*\}\}/)
  assert.match(workflow, /password:\s*\$\{\{\s*secrets\.GITHUB_TOKEN\s*\}\}/)
})

test('custom-release workflow excludes documentation-only pushes', () => {
  const workflow = read('.github/workflows/custom-release.yml')
  assert.match(workflow, /paths-ignore:/, 'documentation-only pushes must be excluded before validation and image jobs')
  assert.match(workflow, /\*\*\/\*\.md/, 'all Markdown-only pushes must be excluded')
  assert.match(workflow, /\.gitignore/, 'repository ignore-file-only pushes must be excluded')
  assert.doesNotMatch(workflow, /paths-ignore:[^]*\.github\/workflows/, 'workflow changes must remain runtime changes')
})

test('release scope classifier compares the production and target commits', () => {
  const classifierPath = resolve(repoRoot, 'deploy/ops/classify-release-scope.sh')
  assert.equal(existsSync(classifierPath), true, 'release scope classifier is missing')
  const classifier = read('deploy/ops/classify-release-scope.sh')
  assert.match(classifier, /PRODUCTION_COMMIT/)
  assert.match(classifier, /TARGET_COMMIT/)
  assert.match(classifier, /git\s+-C\s+.*diff\s+--name-only/)
  assert.match(classifier, /\.md|AGENTS\.md/)
  assert.match(classifier, /\.gitignore/)
  assert.match(classifier, /docs_only=(?:true|false)/)
})

test('both application images expose the same OCI release identity', () => {
  for (const dockerfilePath of ['Dockerfile', 'extensions-self/Dockerfile']) {
    const dockerfile = read(dockerfilePath)
    assert.match(dockerfile, /ARG IMAGE_REVISION/)
    assert.match(dockerfile, /ARG IMAGE_VERSION/)
    assert.match(
      dockerfile,
      /org\.opencontainers\.image\.revision="?\$\{IMAGE_REVISION\}"?/
    )
    assert.match(
      dockerfile,
      /org\.opencontainers\.image\.version="?\$\{IMAGE_VERSION\}"?/
    )
    assert.match(
      dockerfile,
      /org\.opencontainers\.image\.source="https:\/\/github\.com\/ListenCodes\/sub2api"/
    )
  }
})

test('production compose requires immutable application image references', () => {
  const base = read('deploy/docker-compose.yml')
  const compose = read('deploy/docker-compose.custom.yml')
  assert.match(base, /image:\s*weishaw\/sub2api:latest/)
  assert.doesNotMatch(base, /SUB2API_IMAGE|EXTENSIONS_SELF_IMAGE/)
  assert.match(compose, /image:\s*\$\{SUB2API_IMAGE:\?SUB2API_IMAGE is required\}/)
  assert.match(
    compose,
    /image:\s*\$\{EXTENSIONS_SELF_IMAGE:\?EXTENSIONS_SELF_IMAGE is required\}/
  )
  assert.doesNotMatch(compose, /image:\s*sub2api:custom/)
  assert.doesNotMatch(compose, /image:\s*deploy-extensions-self/)

  const envExample = read('deploy/.env.example')
  assert.match(envExample, /^SUB2API_IMAGE=ghcr\.io\/listencodes\/sub2api-custom@sha256:/m)
  assert.match(
    envExample,
    /^EXTENSIONS_SELF_IMAGE=ghcr\.io\/listencodes\/sub2api-extensions@sha256:/m
  )
})

test('production compose preserves upstream Redis ACL username support', () => {
  const compose = read('deploy/docker-compose.yml')
  assert.match(compose, /REDIS_USERNAME=\$\{REDIS_USERNAME:-\}/)
})

test('prepare validates evidence and apply deploys and rolls back an immutable image pair', () => {
  const publisher = `${read('deploy/ops/prepare-release.sh')}\n${read('deploy/ops/apply-release.sh')}`
  for (const marker of [
    'main_digest',
    'extensions_digest',
    'verify-release-images.sh',
    'release-ledger',
    'release_job_update',
    'backing_up',
    'switching_extensions',
    'switching_main',
    'health_checking',
    'rolling_back',
    'rollback_on_error'
  ]) {
    assert.match(publisher, new RegExp(escapeRegExp(marker)), `publisher is missing ${marker}`)
  }
  assert.doesNotMatch(publisher, /docker\s+build/)
  assert.doesNotMatch(publisher, /compose[^\n]*build/)
  assert.doesNotMatch(publisher, /up -d[^\n]*risk-control-postgres/)
  assert.doesNotMatch(publisher, /(?:rm|down)[^\n]*risk-control-postgres/)

  const verifyIndex = publisher.indexOf('VERIFY_IMAGES_SCRIPT')
  const backupIndex = publisher.indexOf('backing_up')
  const extensionsIndex = publisher.indexOf('switching_extensions')
  const mainIndex = publisher.indexOf('switching_main')
  const healthIndex = publisher.lastIndexOf('health_checking')
  assert.ok(verifyIndex >= 0 && verifyIndex < backupIndex)
  assert.ok(backupIndex < extensionsIndex)
  assert.ok(extensionsIndex < mainIndex)
  assert.ok(mainIndex < healthIndex)
})

test('dispatcher selects all two-stage update and rollback executors from ledger operations', () => {
  const dispatcher = read('deploy/ops/sync-and-publish.sh')
  for (const marker of ['prepare-release.sh', 'apply-release.sh', 'prepare-rollback.sh', 'apply-rollback.sh', 'operation_kind']) {
    assert.match(dispatcher, new RegExp(marker.replaceAll('.', '\\.')), `dispatcher is missing ${marker}`)
  }
  assert.doesNotMatch(dispatcher, /publish-custom\.sh/, 'dispatcher must not invoke the legacy publisher')
})

test('release documentation defines only the administrator-triggered digest path', () => {
  const documentation = [
    'AGENTS.md',
    'deploy/RELEASE-RUNBOOK.md',
    'deploy/README.md',
    'deploy/ops/README.md'
  ].map((path) => ({ path, text: read(path) }))

  for (const { path, text } of documentation) {
    for (const marker of [
      'custom-release',
      'ghcr.io/listencodes/sub2api-custom',
      'ghcr.io/listencodes/sub2api-extensions',
      'sub2api-release.path',
      'SUB2API_IMAGE',
      'EXTENSIONS_SELF_IMAGE'
    ]) {
      assert.match(text, new RegExp(escapeRegExp(marker)), `${path} is missing ${marker}`)
    }
    assert.match(text, /administrator|管理员/i, `${path} is missing administrator-only triggering`)
    assert.match(text, /automatic(?:ally)?[\s\S]{0,80}(?:rollback|rolls? back)|自动[^\n]*回退/i, `${path} is missing automatic paired rollback`)
    assert.doesNotMatch(text, /auto-update\.sh/, `${path} still documents auto-update.sh`)
    assert.doesNotMatch(text, /^\s*0\s+3\s+\*\s+\*\s+\*/m, `${path} still documents a daily release cron`)
    assert.doesNotMatch(text, /^\s*\*\s+\*\s+\*\s+\*\s+\*[^\n]*(?:sync|release-trigger)/m, `${path} still documents a polling release cron`)
  }

  const agents = read('AGENTS.md')
  assert.match(agents, /cherry-pick\s+-x[^\n]*custom/i)
  assert.match(agents, /entire[^\n]*custom[^\n]*never merged|never merge[^\n]*entire[^\n]*custom/i)
  assert.doesNotMatch(agents, /main Compose file must build|build and deploy the same tag/i)
  assert.match(agents, /publish-custom\.sh[^\n]*deprecated fail-closed/i)
  assert.doesNotMatch(agents, /publish-custom\.sh[^\n]*internal digest deployment entrypoint/i)

  const runbook = read('deploy/RELEASE-RUNBOOK.md')
  assert.match(runbook, /feature[^\n]*custom-release[^\n]*Actions/i)
  assert.match(runbook, /database restore[^\n]*not automatic|does not automatically restore[^\n]*database/i)
  assert.match(runbook, /implementation[\s\S]{0,160}push[\s\S]{0,160}deployment/i)
  assert.doesNotMatch(runbook, /publish-custom\.sh\s+internal backup\/digest deploy\/rollback component/i)

  const dataManagement = read('deploy/DATAMANAGEMENTD_CN.md')
  assert.doesNotMatch(dataManagement, /建议[^\n]*docker-compose\.override\.yml/)
  assert.match(dataManagement, /不得依赖[^\n]*docker-compose\.override\.yml/)
  assert.match(dataManagement, /docker-compose\.custom(?:\.local)?\.yml/)
})

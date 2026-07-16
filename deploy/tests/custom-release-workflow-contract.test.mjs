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
    'bash -n',
    'docker/build-push-action'
  ]) {
    assert.match(workflow, new RegExp(escapeRegExp(marker)), `workflow is missing ${marker}`)
  }

  assert.match(workflow, /needs:\s*\[[^\]]*backend[^\]]*golangci[^\]]*frontend[^\]]*extensions[^\]]*deployment[^\]]*\]/)
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
  const compose = read('deploy/docker-compose.yml')
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

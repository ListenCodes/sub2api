import assert from 'node:assert/strict'
import { readFileSync, readdirSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

const deployRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const repoRoot = resolve(deployRoot, '..')
const opsRoot = resolve(deployRoot, 'ops')

const legacyStatuses = [
  'checking_updates',
  'checking_release',
  'validating_tag',
  'merging_release',
  'waiting_actions',
  'waiting_images',
  'preparing_compose',
  'promoting_release',
  'deploying_extensions',
  'deploying_main'
]

function read(relativePath) {
  return readFileSync(resolve(repoRoot, relativePath), 'utf8')
}

function canonicalBackendStatuses() {
  const source = read('backend/internal/service/update_job.go')
  return new Set(
    [...source.matchAll(/^\s*ReleaseStatus\w+\s+=\s+"([a-z][a-z0-9_]*)"/gm)]
      .map((match) => match[1])
  )
}

function shellSources(directory = opsRoot) {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const absolutePath = resolve(directory, entry.name)
    if (entry.isDirectory()) return shellSources(absolutePath)
    if (!entry.name.endsWith('.sh')) return []

    return [{
      name: absolutePath.slice(opsRoot.length + 1).replaceAll('\\', '/'),
      source: readFileSync(absolutePath, 'utf8')
    }]
  })
}

test('shell status validator exactly matches the backend canonical vocabulary', () => {
  const source = read('deploy/ops/release-state.sh')
  const match = source.match(
    /release_valid_status\(\)\s*\{[\s\S]*?case "\$\{1:-\}" in\s*\n\s*([^\n]+)\)/
  )
  assert.ok(match, 'release_valid_status case list is missing')

  const shellStatuses = new Set(match[1].split('|').map((status) => status.trim()))
  assert.deepEqual(
    [...shellStatuses].sort(),
    [...canonicalBackendStatuses()].sort(),
    'release_valid_status must use the backend canonical states only'
  )
})

test('versioned host scripts contain no legacy release status literals', () => {
  for (const { name, source } of shellSources()) {
    for (const status of legacyStatuses) {
      assert.doesNotMatch(
        source,
        new RegExp(`\\b${status}\\b`),
        `${name} still contains legacy status ${status}`
      )
    }
  }
})

test('literal script status writes are accepted by the backend', () => {
  const canonical = canonicalBackendStatuses()

  for (const { name, source } of shellSources()) {
    for (const match of source.matchAll(/release_job_update\s+"?\$[A-Za-z_][A-Za-z0-9_]*"?\s+([a-z][a-z0-9_]*)/g)) {
      assert.equal(
        canonical.has(match[1]),
        true,
        `${name} writes backend-unknown status ${match[1]}`
      )
    }
  }

  const initialStatus = read('deploy/ops/release-state.sh').match(/status:"([a-z][a-z0-9_]*)"/)
  assert.ok(initialStatus, 'release job initial status is missing')
  assert.equal(
    canonical.has(initialStatus[1]),
    true,
    `release-state.sh initializes backend-unknown status ${initialStatus[1]}`
  )
})

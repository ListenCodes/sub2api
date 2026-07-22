import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const deploymentFiles = [
  'docker-compose.yml',
  'docker-compose.custom.yml',
  'docker-compose.local.yml',
  '.env.example',
  'ops/prepare-release.sh',
  'ops/apply-release.sh',
  'ops/README.md',
  'RELEASE-RUNBOOK.md',
]

function read(relativePath) {
  return readFileSync(resolve(root, relativePath), 'utf8')
}

test('the active risk-control API uses the extensions-self hostname', () => {
  for (const file of deploymentFiles) {
    assert.doesNotMatch(read(file), /risk-control-v2/, `${file} still references risk-control-v2`)
  }

  assert.match(read('docker-compose.custom.yml'), /RISK_CONTROL_URL:\s*\$\{RISK_CONTROL_URL:-http:\/\/extensions-self:8090\}/)
  const applyScript = read('ops/apply-release.sh')
  assert.match(applyScript, /extensions-self/)
  assert.match(applyScript, /health_checking/)
  assert.match(applyScript, /--pull never/)
})

test('release documentation no longer treats the retired standalone service as active', () => {
  const runbook = read('RELEASE-RUNBOOK.md')
  assert.doesNotMatch(runbook, /\/root\/sub2api-risk-control\/deploy\/docker-compose\.prod\.yml/)
  assert.match(runbook, /\/root\/sub2api\/extensions-self\/risk-control/)
  assert.match(runbook, /`risk-control-postgres` and `risk_control_postgres_data` remain independent/)
  assert.match(runbook, /starts and verifies\s+`extensions-self` before removing the old application container/)
})

import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const deploymentFiles = [
  'docker-compose.yml',
  'docker-compose.local.yml',
  '.env.example',
  'ops/publish-custom.sh',
  'ops/README.md',
  'RELEASE-RUNBOOK.md',
]

function read(relativePath) {
  return readFileSync(resolve(root, relativePath), 'utf8')
}

test('the active risk-control service uses the canonical risk-control hostname', () => {
  for (const file of deploymentFiles) {
    assert.doesNotMatch(read(file), /risk-control-v2/, `${file} still references risk-control-v2`)
  }

  assert.match(read('docker-compose.yml'), /RISK_CONTROL_URL=\$\{RISK_CONTROL_URL:-http:\/\/risk-control:8090\}/)
  const publishScript = read('ops/publish-custom.sh')
  assert.match(publishScript, /http:\/\/risk-control:8090\/healthz/)
  assert.match(publishScript, /rendered_risk_url/)
  assert.match(publishScript, /retired legacy container sub2api-risk-control still exists/)
  assert.match(publishScript, /deploy-risk-control:rollback-\$STAMP/)
  assert.doesNotMatch(publishScript, /"risk-control:rollback-\$STAMP"/)
})

test('release documentation no longer treats the retired standalone service as active', () => {
  const runbook = read('RELEASE-RUNBOOK.md')
  assert.doesNotMatch(runbook, /\/root\/sub2api-risk-control\/deploy\/docker-compose\.prod\.yml/)
  assert.match(runbook, /\/root\/sub2api\/risk-control/)
  assert.match(runbook, /archive `\/root\/sub2api-risk-control`/)
  assert.match(runbook, /stop its\s+`sub2api-risk-control` container without removing volumes/)
})
